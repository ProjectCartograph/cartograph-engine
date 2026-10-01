// Package vault implements store.ManifestStore over a directory tree: files
// are the source of truth. On open, it reads every <Kind>/*.yaml file,
// compares each file's SHA-256 against the hash recorded in the index
// (a store.VaultIndex the caller opens under .cartograph/), and appends new versions for any file that is new
// or changed, so ListVersions and diff keep working. An fsnotify watcher
// (when enabled) debounces file changes 200 ms and reloads them into memory
// and the index. Deleting .cartograph/ and reopening rebuilds an equivalent index
// with the same current versions (version numbers may restart at 1).
package vault

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/manifestmeta"
	"github.com/fsnotify/fsnotify"
)

const timeLayout = time.RFC3339Nano

// ManifestStore is a file-based vault adapter. Files in <vault>/<Kind>/<id>.yaml
// are the source of truth. The index beside them (a store.VaultIndex under
// .cartograph/) keeps versions, references and summaries.
type ManifestStore struct {
	vaultDir           string
	extension          string // the manifest files' extension, from the codec: ".yaml"
	index              store.VaultIndex
	mu                 sync.RWMutex
	cache              map[string]*store.Version // key: "kind/id"
	hashes             map[string]string         // key: "kind/id", SHA-256 hash of current file
	watcher            *fsnotify.Watcher
	watchDone          chan struct{}
	applyJournalMaxAge int          // Max age (count) of units to keep; 0 = unlimited
	failurePoint       FailurePoint // TEST ONLY: inject failure at this point
}

// FailurePoint is used for testing: injects a failure at a specific point in applyUnit.
type FailurePoint int

const (
	FailurePointNone           FailurePoint = iota // No failure
	FailurePointAfterJournal                = iota // After journal.PutUnit, before file writes
	FailurePointAfterFirstFile              = iota // After first file write, before vault.yaml
	FailurePointAfterLastFile               = iota // After vault.yaml write, before mark applied
)

// Options control how a vault is opened.
type Options struct {
	Watch bool
	// Extension is the manifest files' extension with its dot, the
	// codec's; ".yaml" when empty. vault.yaml itself is always YAML: it
	// is the adapter's own file, not a manifest.
	Extension    string
	Debounce     time.Duration // Watcher debounce interval; 0 means use default 200ms
	FailurePoint FailurePoint  // TEST ONLY: inject a failure at this point in applyUnit
	// OpenIndex opens the index the vault keeps beside its files, given
	// the directory set aside for it (.cartograph). Required: which index
	// adapter a vault runs on is the composition root's choice, never
	// the vault's.
	OpenIndex func(ctx context.Context, dir string) (store.VaultIndex, error)
}

// SetFailurePoint sets the failure point for testing purposes. TEST ONLY.
func (m *ManifestStore) SetFailurePoint(fp FailurePoint) {
	m.failurePoint = fp
}

// New opens a vault directory and the index opts.OpenIndex opens. On open,
// it scans every <Kind>/*.yaml file and records new or changed versions
// in the index. If opts.Watch is true, a debounced watcher tracks
// further file changes and reloads them into memory and the index (debounce
// interval defaults to 200 ms, or opts.Debounce if set). Call
// Close() to stop the watcher and close the index.
func New(ctx context.Context, vaultDir string, opts Options) (*ManifestStore, error) {
	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		return nil, fmt.Errorf("create vault directory: %w", err)
	}

	// Create .cartograph directory for index
	cartographDir := filepath.Join(vaultDir, ".cartograph")
	if err := os.MkdirAll(cartographDir, 0755); err != nil {
		return nil, fmt.Errorf("create .cartograph directory: %w", err)
	}

	// The index is a port (store.VaultIndex); the caller says which
	// adapter opens it.
	if opts.OpenIndex == nil {
		return nil, errors.New("vault: an index is required (Options.OpenIndex)")
	}
	index, err := opts.OpenIndex(ctx, cartographDir)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}

	ext := opts.Extension
	if ext == "" {
		ext = ".yaml"
	}
	m := &ManifestStore{
		vaultDir:           vaultDir,
		extension:          ext,
		index:              index,
		cache:              make(map[string]*store.Version),
		hashes:             make(map[string]string),
		watchDone:          make(chan struct{}),
		applyJournalMaxAge: 1000, // Default: keep last 1000 units
		failurePoint:       opts.FailurePoint,
	}

	// Replay unapplied units from the journal before rehydration
	if err := m.replayUnapplied(ctx); err != nil {
		index.Close()
		return nil, fmt.Errorf("replay unapplied units: %w", err)
	}

	// Prune old units from the journal
	if err := m.pruneJournal(ctx); err != nil {
		index.Close()
		return nil, fmt.Errorf("prune journal: %w", err)
	}

	// Check if vault.yaml exists; generate if missing
	vaultYAMLPath := filepath.Join(vaultDir, "vault.yaml")
	if _, err := os.Stat(vaultYAMLPath); os.IsNotExist(err) {
		if _, err := m.generateVaultManifest(ctx); err != nil {
			index.Close()
			return nil, fmt.Errorf("generate vault.yaml: %w", err)
		}
	}

	// ALWAYS load from vault.yaml's include list into cache (regardless of hash)
	// The hash is used only to decide whether the index needs rebuilding, not what to load
	if err := m.rehydrate(ctx); err != nil {
		index.Close()
		return nil, fmt.Errorf("rehydrate from vault.yaml: %w", err)
	}

	// Note: auto-apply is disabled to maintain the invariant that only included files are live.
	// Unapplied files must be explicitly applied via the apply endpoint.

	// Drafts come back after the files they are drafts of, so unfinished
	// work survives a restart and still wins over what is saved.
	if err := m.loadStagedIntoCache(ctx); err != nil {
		index.Close()
		return nil, fmt.Errorf("load staged drafts: %w", err)
	}

	// Record the vault.yaml hash for next open (used only for index rebuild decision)
	hash, err := m.vaultHash(ctx)
	if err != nil {
		index.Close()
		return nil, err
	}
	if err := m.recordVaultHash(ctx, hash); err != nil {
		index.Close()
		return nil, err
	}

	// Start watcher if requested
	if opts.Watch {
		debounce := opts.Debounce
		if debounce == 0 {
			debounce = 200 * time.Millisecond
		}
		if err := m.startWatcher(ctx, debounce); err != nil {
			index.Close()
			return nil, fmt.Errorf("start watcher: %w", err)
		}
	}

	return m, nil
}

// recordFileHash upserts a file's hash and mtime in the index.
func (m *ManifestStore) recordFileHash(ctx context.Context, kind, id, hash, mtime string) error {
	return m.index.PutFileHash(ctx, kind, id, hash, mtime)
}

// startWatcher begins watching the vault directory for file changes.
// It debounces changes by the specified interval before reloading files.
// Watches all existing kind directories and the vault root for new directories.
func (m *ManifestStore) startWatcher(ctx context.Context, debounce time.Duration) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	m.watcher = watcher

	// Watch the vault root directory for new kind directories
	if err := watcher.Add(m.vaultDir); err != nil {
		watcher.Close()
		return fmt.Errorf("watch %s: %w", m.vaultDir, err)
	}

	// Watch all existing kind directories
	entries, err := os.ReadDir(m.vaultDir)
	if err != nil {
		watcher.Close()
		return fmt.Errorf("read vault dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != ".cartograph" {
			kindDir := filepath.Join(m.vaultDir, entry.Name())
			if err := watcher.Add(kindDir); err != nil {
				watcher.Close()
				return fmt.Errorf("watch %s: %w", kindDir, err)
			}
		}
	}

	// Run watcher in background
	go m.watchLoop(ctx, debounce)
	return nil
}

// watchLoop runs in a goroutine and handles file system events with debouncing.
// Handles Write, Create, Rename, and Remove events on .yaml files.
func (m *ManifestStore) watchLoop(ctx context.Context, debounceInterval time.Duration) {
	debounce := time.NewTimer(0)
	<-debounce.C // Drain the initial fire
	debounce.Stop()

	pending := make(map[string]bool) // key: "kind/id"
	removes := make(map[string]bool) // key: "kind/id" to remove
	vaultYAMLChanged := false
	pendingMu := sync.Mutex{}

	defer func() { close(m.watchDone) }()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}

			// Watch new subdirectories (Create event on a directory)
			if event.Op&fsnotify.Create != 0 {
				info, err := os.Stat(event.Name)
				if err == nil && info.IsDir() && filepath.Base(event.Name) != ".cartograph" {
					m.watcher.Add(event.Name)
				}
			}

			if !strings.HasSuffix(event.Name, m.ext()) {
				continue
			}

			// Check for vault.yaml changes
			if filepath.Base(event.Name) == "vault.yaml" {
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					pendingMu.Lock()
					vaultYAMLChanged = true
					pendingMu.Unlock()
					debounce.Reset(debounceInterval)
				}
				continue
			}

			// Extract kind and id from path
			dir := filepath.Dir(event.Name)
			kind := filepath.Base(dir)
			id := strings.TrimSuffix(filepath.Base(event.Name), m.ext())
			key := kind + "/" + id

			// Mark as pending for reload or remove
			pendingMu.Lock()
			if event.Op&fsnotify.Remove != 0 {
				// File was deleted; mark for removal from cache
				removes[key] = true
				delete(pending, key) // Don't reload if also marked for removal
			} else if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
				// File was written, created, or renamed; mark for reload
				pending[key] = true
				delete(removes, key) // Don't remove if also marked for reload
			}
			pendingMu.Unlock()

			// Reset debounce timer
			debounce.Reset(debounceInterval)

		case <-debounce.C:
			// Process pending changes and removes
			pendingMu.Lock()
			if vaultYAMLChanged {
				// Rehydrate from vault.yaml
				vaultYAMLChanged = false
				pendingMu.Unlock()
				if err := m.rehydrate(ctx); err != nil {
					fmt.Fprintf(os.Stderr, "vault watcher: rehydrate error: %v\n", err)
				}
				pendingMu.Lock()
			}
			if len(pending) > 0 || len(removes) > 0 {
				pendingKeys := make([]string, 0, len(pending))
				for key := range pending {
					pendingKeys = append(pendingKeys, key)
				}
				removeKeys := make([]string, 0, len(removes))
				for key := range removes {
					removeKeys = append(removeKeys, key)
				}
				pending = make(map[string]bool)
				removes = make(map[string]bool)
				pendingMu.Unlock()

				for _, key := range pendingKeys {
					parts := strings.Split(key, "/")
					if len(parts) != 2 {
						continue
					}
					kind, id := parts[0], parts[1]
					m.reloadFile(ctx, kind, id)
				}

				for _, key := range removeKeys {
					parts := strings.Split(key, "/")
					if len(parts) != 2 {
						continue
					}
					kind, id := parts[0], parts[1]
					m.removeFile(kind, id)
				}
			} else {
				pendingMu.Unlock()
			}

		case err, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "vault watcher error: %v\n", err)
		}
	}
}

// reloadFile reads a file from disk and updates the cache and index.
func (m *ManifestStore) reloadFile(ctx context.Context, kind, id string) {
	ref := kind + "/" + id

	// Check if file is in vault.yaml or already in cache
	vault, err := m.readVaultManifest(ctx)
	if err != nil {
		// Can't read vault, check cache only
		vault.Spec.Include = []string{}
	}

	// Check if this file is included in vault.yaml
	included := false
	for _, includedRef := range vault.Spec.Include {
		if includedRef == ref {
			included = true
			break
		}
	}

	// Check if already in cache (from a prior PutVersion)
	m.mu.RLock()
	_, inCache := m.cache[ref]
	m.mu.RUnlock()

	// Skip loading only if NOT included and NOT in cache (truly unapplied/new)
	if !included && !inCache {
		return
	}

	path := filepath.Join(m.vaultDir, kind, id+m.ext())
	data, err := os.ReadFile(path)
	if err != nil {
		// File deleted or unreadable; skip
		if os.IsNotExist(err) {
			m.removeFile(kind, id)
		}
		return
	}

	hash := sha256String(data)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if hash changed (ignore if it matches the already-recorded hash from disk)
	key := kind + "/" + id
	if m.hashes[key] == hash {
		// File hasn't changed since we last recorded it, skip
		return
	}

	// Check if hash changed from current cache
	current, ok := m.cache[key]
	if ok && sha256String(current.YAML) == hash {
		// No change, skip
		return
	}

	// Hash changed, update cache with file content (no versioning)
	v := store.Version{
		Kind:   kind,
		ID:     id,
		Number: 0, // File content, not a version
		YAML:   data,
		Actor:  "local",
		Reason: "read from disk",
		On:     time.Now().UTC(),
	}

	// Update cache and hash
	m.cache[kind+"/"+id] = &v
	m.hashes[key] = hash

	// Record hash in database
	fi, err := os.Stat(path)
	if err == nil {
		_ = m.recordFileHash(ctx, kind, id, hash, fi.ModTime().Format(timeLayout))
	}
}

// removeFile removes a file from cache and hashes when it's deleted on disk.
func (m *ManifestStore) removeFile(kind, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := kind + "/" + id
	delete(m.cache, key)
	delete(m.hashes, key)
}

// PutVersion writes the manifest to disk at <vault>/<Kind>/<id>.yaml and
// appends a version to the index. If the file on disk has changed since
// it was last read (SHA-256 mismatch), returns *store.ConflictError.
func (m *ManifestStore) PutVersion(ctx context.Context, v store.Version) error {
	filePath := filepath.Join(m.vaultDir, v.Kind, v.ID+m.ext())

	// Check for concurrent edits
	diskData, diskErr := os.ReadFile(filePath)
	if diskErr != nil && !os.IsNotExist(diskErr) {
		return fmt.Errorf("read file: %w", diskErr)
	}

	// Get the last recorded hash
	recordedHash, foundHash, err := m.index.GetFileHash(ctx, v.Kind, v.ID)
	if err != nil {
		return fmt.Errorf("query file hash: %w", err)
	}

	// If file exists on disk, check for conflict
	if diskErr == nil {
		diskHash := sha256String(diskData)
		if foundHash && diskHash != recordedHash {
			return &store.ConflictError{
				Kind:   v.Kind,
				ID:     v.ID,
				Ours:   v.YAML,
				Theirs: diskData,
			}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Note: version number validation is done by the engine (Commit) before calling this method.
	// We skip validation here to avoid conflicts between cache-based (sqlite) and log-based (vault) checks.

	// Record in SQLite index
	if err := m.index.PutVersion(ctx, v); err != nil {
		return fmt.Errorf("put version in index: %w", err)
	}

	// The save is the promotion, so it is the operation that has to be
	// atomic: the manifest file and the include list that admits it go in
	// one journalled apply unit, written to a temp file, fsynced and
	// renamed. Until 2026-09-28 a save was a bare os.WriteFile plus a
	// separate vault.yaml write, either of which could land without the
	// other; the journalled path existed but only autosave used it, and
	// autosave no longer writes here at all.
	m.mu.Unlock()
	unit, err := m.promotionUnit(ctx, v)
	if err != nil {
		m.mu.Lock()
		return err
	}
	if err := m.applyUnit(ctx, unit); err != nil {
		m.mu.Lock()
		return err
	}
	// vault.yaml changed, so the hash recorded for the next open has to
	// follow it.
	if hash, hashErr := m.vaultHash(ctx); hashErr == nil {
		_ = m.recordVaultHash(ctx, hash)
	}
	// The draft has been promoted, so there is nothing left to promote. A
	// crash before this leaves a draft identical to the file, which the
	// next save writes again and skips as unchanged.
	if err := m.clearStaged(v.Kind, v.ID); err != nil {
		m.mu.Lock()
		return err
	}
	m.mu.Lock()

	// Cache and hash now describe the file that was just written.
	key := v.Kind + "/" + v.ID
	m.cache[key] = &v
	m.hashes[key] = sha256String(v.YAML)
	if fi, statErr := os.Stat(filepath.Join(m.vaultDir, v.Kind, v.ID+m.ext())); statErr == nil {
		_ = m.recordFileHash(ctx, v.Kind, v.ID, m.hashes[key], fi.ModTime().Format(timeLayout))
	}

	// Write snapshot marker file
	return m.writeSnapshotMarker(ctx, &v)
}

// promotionUnit is the manifest file and the vault.yaml that includes it,
// as one unit. Both or neither: a file nothing includes is invisible, and
// an include naming no file is a dangling reference.
func (m *ManifestStore) promotionUnit(ctx context.Context, v store.Version) (store.ApplyUnit, error) {
	vault, err := m.readVaultManifest(ctx)
	if err != nil {
		return store.ApplyUnit{}, fmt.Errorf("read vault.yaml: %w", err)
	}
	ref := v.Kind + "/" + v.ID
	included := false
	for _, existing := range vault.Spec.Include {
		if existing == ref {
			included = true
			break
		}
	}
	if !included {
		vault.Spec.Include = append(vault.Spec.Include, ref)
	}
	vaultYAMLBytes, err := m.getVaultYAMLBytes(vault)
	if err != nil {
		return store.ApplyUnit{}, err
	}
	actor := v.Actor
	if actor == "" {
		actor = "local"
	}
	return store.ApplyUnit{
		Operator: actor,
		Reason:   v.Reason,
		Files: []store.ApplyFile{
			{Path: ref + m.ext(), SHA256: sha256String(v.YAML), Content: v.YAML},
			{Path: "vault.yaml", SHA256: sha256String(vaultYAMLBytes), Content: vaultYAMLBytes},
		},
	}, nil
}

// PutWorking stages a draft. It does not touch the vault's own tree and
// does not add the ref to vault.yaml: an unfinished definition is not part
// of the vault, and autosave is not a decision to include something. See
// vault_staging.go for why.
//
// If the id is excluded, returns a ConflictError with the exclusion reason.
func (m *ManifestStore) PutWorking(ctx context.Context, kind, id string, yamlBytes []byte) error {
	key := kind + "/" + id

	// Check if the id is excluded; if so, reject with an error that includes the reason.
	// We use a custom error struct that wraps ConflictError with the exclusion reason.
	exclusions, err := m.ListExcluded(ctx)
	if err != nil {
		return fmt.Errorf("check exclusions: %w", err)
	}
	for _, excl := range exclusions {
		if excl.Kind == kind && excl.ID == id {
			// Return a ConflictError with the reason in the Theirs field (used for error message)
			// The API handler will extract this and format it as a problem message
			msg := fmt.Sprintf("excluded on %s: %s; recover to restore", excl.On.Format("2006-01-02"), excl.Reason)
			return &store.ConflictError{
				Kind:   kind,
				ID:     id,
				Ours:   yamlBytes,
				Theirs: []byte(msg), // Store the reason message here for the API handler
			}
		}
	}

	if err := m.writeStaged(kind, id, yamlBytes); err != nil {
		return err
	}

	m.mu.Lock()
	// Note: m.hashes tracks the *vault* file, which this did not write, so
	// it is deliberately left alone. Recording a draft's hash there would
	// make the next save think somebody else had edited the file.
	m.cache[key] = &store.Version{
		Kind:   kind,
		ID:     id,
		Number: 0, // A draft, not a version.
		YAML:   yamlBytes,
		Actor:  "local",
		Reason: "working copy",
		On:     time.Now().UTC(),
	}
	m.mu.Unlock()

	// The index's working copy is what ListReferencing reads, so a draft's
	// references are resolvable while it is still a draft.
	return m.index.PutWorking(ctx, kind, id, yamlBytes)
}

// GetWorking returns a manifest's working copy (the file on disk).
// Returns found=false when no file exists.
func (m *ManifestStore) GetWorking(ctx context.Context, kind, id string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// A draft is what somebody is working on, so it answers first. Falling
	// through to the file is what makes opening a saved definition work:
	// there is no draft until the first edit.
	if data, found, err := m.readStaged(kind, id); err != nil {
		return nil, false, err
	} else if found {
		return data, true, nil
	}

	filePath := filepath.Join(m.vaultDir, kind, id+m.ext())
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read file: %w", err)
	}
	return data, true, nil
}

// GetCurrent returns the current version from the in-memory cache.
func (m *ManifestStore) GetCurrent(ctx context.Context, kind, id string) (store.Version, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := kind + "/" + id
	v, ok := m.cache[key]
	if !ok {
		return store.Version{}, false, nil
	}
	return *v, true, nil
}

// GetVersion delegates to the index.
func (m *ManifestStore) GetVersion(ctx context.Context, kind, id string, number int) (store.Version, bool, error) {
	return m.index.GetVersion(ctx, kind, id, number)
}

// ListVersions delegates to the index.
func (m *ManifestStore) ListVersions(ctx context.Context, kind, id string) ([]store.Version, error) {
	return m.index.ListVersions(ctx, kind, id)
}

// ListAllVersions returns all versions of all manifests across kinds, newest first.
func (m *ManifestStore) ListAllVersions(ctx context.Context, limit int, cursor string) ([]store.Version, string, error) {
	return m.index.ListAllVersions(ctx, limit, cursor)
}

// ListSummaries returns summaries of all current versions of a kind.
func (m *ManifestStore) ListSummaries(ctx context.Context, kind, query string, refs []store.RefFilter) ([]store.Summary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summaries := make([]store.Summary, 0)
	query = strings.ToLower(strings.TrimSpace(query))

	for key, v := range m.cache {
		parts := strings.Split(key, "/")
		if len(parts) != 2 || parts[0] != kind {
			continue
		}

		// Query filter
		if query != "" {
			name := manifestmeta.Name(v.YAML)
			if !strings.Contains(strings.ToLower(v.ID), query) && !strings.Contains(strings.ToLower(name), query) {
				continue
			}
		}

		s := store.Summary{
			Kind:      v.Kind,
			ID:        v.ID,
			Name:      manifestmeta.Name(v.YAML),
			Labels:    manifestmeta.Labels(v.YAML),
			Version:   v.Number,
			UpdatedOn: v.On,
		}
		summaries = append(summaries, s)
	}

	// Filter by references if specified
	if len(refs) > 0 {
		filtered := summaries[:0]
		for _, s := range summaries {
			matches := true
			for _, rf := range refs {
				refSummaries, err := m.index.ListReferencing(ctx, rf.Kind, rf.ID)
				if err != nil {
					return nil, err
				}
				found := false
				for _, rs := range refSummaries {
					if rs.Kind == s.Kind && rs.ID == s.ID {
						found = true
						break
					}
				}
				if !found {
					matches = false
					break
				}
			}
			if matches {
				filtered = append(filtered, s)
			}
		}
		summaries = filtered
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].ID < summaries[j].ID
	})

	return summaries, nil
}

// ListIDs returns IDs from cache (file content) first, then from index (versions).
func (m *ManifestStore) ListIDs(ctx context.Context, kind string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := []string{}
	seen := make(map[string]bool)

	// Add IDs from cache (file content)
	for key := range m.cache {
		parts := strings.Split(key, "/")
		if len(parts) == 2 && parts[0] == kind {
			out = append(out, parts[1])
			seen[parts[1]] = true
		}
	}

	// Query index for versions not in cache
	m.mu.RUnlock()
	indexIDs, err := m.index.ListIDs(ctx, kind)
	m.mu.RLock()

	if err != nil {
		return nil, err
	}
	for _, id := range indexIDs {
		if !seen[id] {
			out = append(out, id)
		}
	}

	sort.Strings(out)
	return out, nil
}

// Counts returns manifest counts from cache (file content) and index (versions).
func (m *ManifestStore) Counts(ctx context.Context) (map[string]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := map[string]int{}
	seen := make(map[string]bool)

	// Count from cache (file content)
	for key := range m.cache {
		parts := strings.Split(key, "/")
		if len(parts) == 2 {
			out[parts[0]]++
			seen[key] = true
		}
	}

	// Query index for versions not in cache
	m.mu.RUnlock()
	indexCounts, err := m.index.Counts(ctx)
	m.mu.RLock()

	if err != nil {
		return nil, err
	}
	for kind := range indexCounts {
		// Count IDs in index not in cache
		indexIDs, errList := m.index.ListIDs(ctx, kind)
		if errList != nil {
			continue
		}
		for _, id := range indexIDs {
			key := kind + "/" + id
			if !seen[key] {
				out[kind]++
			}
		}
	}

	return out, nil
}

// IndexReferences delegates to the index.
func (m *ManifestStore) IndexReferences(ctx context.Context, fromKind, fromID string, refs []store.Ref) error {
	return m.index.IndexReferences(ctx, fromKind, fromID, refs)
}

// ListReferencedBy delegates to the index.
func (m *ManifestStore) ListReferencedBy(ctx context.Context, fromKind, fromID string) ([]store.Ref, error) {
	return m.index.ListReferencedBy(ctx, fromKind, fromID)
}

// ListReferencing delegates to the index.
func (m *ManifestStore) ListReferencing(ctx context.Context, toKind, toID string) ([]store.Summary, error) {
	return m.index.ListReferencing(ctx, toKind, toID)
}

// txStore is a transactional ManifestStore that stages writes to be persisted after commit.
type txStore struct {
	parent *ManifestStore
	index  store.ManifestStore      // the sqlite transaction
	staged map[string]store.Version // kind/id -> version
}

// WithinTransaction runs fn against a transactional vault store that stages writes.
// When fn succeeds and the index transaction commits, staged files are written to disk
// and added to vault.yaml in one ApplyUnit (vault.yaml written last).
// If fn or the index commit fails, no files are written.
func (m *ManifestStore) WithinTransaction(ctx context.Context, fn func(ctx context.Context, tx store.ManifestStore) error) error {
	tx := &txStore{parent: m, staged: map[string]store.Version{}}

	// Run the transaction against the index
	err := m.index.WithinTransaction(ctx, func(ctx context.Context, itx store.ManifestStore) error {
		tx.index = itx
		return fn(ctx, tx)
	})
	if err != nil {
		return err // index rolled back; nothing was written to disk
	}

	// Index committed: now prepare an ApplyUnit with all staged files and vault.yaml
	// Read current vault.yaml and prepare updated version
	vault, err := m.readVaultManifest(ctx)
	if err != nil {
		return fmt.Errorf("read vault.yaml: %w", err)
	}

	// Build set of refs to apply and prepare ApplyUnit files
	toApply := make(map[string]bool)
	var unitFiles []store.ApplyFile

	for _, v := range tx.staged {
		ref := v.Kind + "/" + v.ID
		toApply[ref] = true

		// Add manifest file to unit
		unitFiles = append(unitFiles, store.ApplyFile{
			Path:    v.Kind + "/" + v.ID + m.ext(),
			SHA256:  sha256String(v.YAML),
			Content: v.YAML,
		})
	}

	// Add refs to vault.yaml
	existing := make(map[string]bool)
	for _, ref := range vault.Spec.Include {
		existing[ref] = true
	}
	for ref := range toApply {
		if !existing[ref] {
			vault.Spec.Include = append(vault.Spec.Include, ref)
		}
	}

	// Serialize updated vault.yaml
	vaultYAMLBytes, err := m.getVaultYAMLBytes(vault)
	if err != nil {
		return err
	}

	// Add vault.yaml to unit
	unitFiles = append(unitFiles, store.ApplyFile{
		Path:    "vault.yaml",
		SHA256:  sha256String(vaultYAMLBytes),
		Content: vaultYAMLBytes,
	})

	// Create and apply the unit
	unit := store.ApplyUnit{
		Operator: "local",
		Reason:   "transactional commit",
		Files:    unitFiles,
	}

	if err := m.applyUnit(ctx, unit); err != nil {
		return err
	}

	// Each promoted ref's draft is done with. This is the save path the
	// engine actually takes — Commit runs inside a transaction — so the
	// promotion has to be cleared here and not only in PutVersion. Only
	// the refs in this unit: there may be many other drafts, and saving
	// one is not a decision about the rest.
	for _, v := range tx.staged {
		if err := m.clearStaged(v.Kind, v.ID); err != nil {
			return err
		}
	}

	// Write snapshot markers for all staged versions
	for _, v := range tx.staged {
		if err := m.writeSnapshotMarker(ctx, &v); err != nil {
			return err
		}
	}

	// Update cache and hashes for all staged versions
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range tx.staged {
		key := v.Kind + "/" + v.ID
		hash := sha256String(v.YAML)
		m.hashes[key] = hash
		m.cache[key] = &v
	}

	return nil
}

// PutVersion for txStore: performs conflict check and version-number validation,
// delegates to index transaction, and records the staged version.
func (tx *txStore) PutVersion(ctx context.Context, v store.Version) error {
	key := v.Kind + "/" + v.ID

	// Get recorded hash for conflict check
	tx.parent.mu.RLock()
	recordedHash := tx.parent.hashes[key]
	tx.parent.mu.RUnlock()

	// Conflict check: compare staged hash against recorded hash from disk
	filePath := filepath.Join(tx.parent.vaultDir, v.Kind, v.ID+tx.parent.ext())
	diskData, diskErr := os.ReadFile(filePath)
	if diskErr != nil && !os.IsNotExist(diskErr) {
		return fmt.Errorf("read file: %w", diskErr)
	}
	if diskErr == nil {
		diskHash := sha256String(diskData)
		if recordedHash != "" && diskHash != recordedHash {
			return &store.ConflictError{
				Kind:   v.Kind,
				ID:     v.ID,
				Ours:   v.YAML,
				Theirs: diskData,
			}
		}
	}

	// Verify correct version number using the version log (ListVersions), not cache or GetCurrent.
	// The cache contains file content with Number=0 for working copies.
	// GetCurrent prioritizes working copies and returns Number=0, which is wrong for version numbering.
	// We compute want the same way the engine does: from the latest version in the version log.
	want := 1
	if staged, hasStagedVersion := tx.staged[key]; hasStagedVersion {
		want = staged.Number + 1
	} else {
		// Check the index transaction's version log
		versions, err := tx.index.ListVersions(ctx, v.Kind, v.ID)
		if err != nil {
			return fmt.Errorf("list versions: %w", err)
		}
		if len(versions) > 0 {
			want = versions[len(versions)-1].Number + 1
		}
	}
	if v.Number != want {
		fmt.Fprintf(os.Stderr, "put version: version number mismatch for %s/%s: expected %d, got %d\n", v.Kind, v.ID, want, v.Number)
		return fmt.Errorf("put version: expected number %d for %s/%s, got %d",
			want, v.Kind, v.ID, v.Number)
	}

	// Delegate to index transaction
	if err := tx.index.PutVersion(ctx, v); err != nil {
		return err
	}

	// Stage the write
	tx.staged[key] = v
	return nil
}

// PutWorking is not used within transactions; it's a no-op here.
func (tx *txStore) PutWorking(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

// GetWorking reads the working copy through the parent store; a transaction
// never stages working copies.
func (tx *txStore) GetWorking(ctx context.Context, kind, id string) ([]byte, bool, error) {
	return tx.parent.GetWorking(ctx, kind, id)
}

// GetCurrent returns from staged writes, then from parent cache.
func (tx *txStore) GetCurrent(ctx context.Context, kind, id string) (store.Version, bool, error) {
	key := kind + "/" + id
	if staged, ok := tx.staged[key]; ok {
		return staged, true, nil
	}
	return tx.parent.GetCurrent(ctx, kind, id)
}

// GetVersion delegates to index transaction.
func (tx *txStore) GetVersion(ctx context.Context, kind, id string, number int) (store.Version, bool, error) {
	return tx.index.GetVersion(ctx, kind, id, number)
}

// ListVersions delegates to index transaction.
func (tx *txStore) ListVersions(ctx context.Context, kind, id string) ([]store.Version, error) {
	return tx.index.ListVersions(ctx, kind, id)
}

// ListAllVersions delegates to index.
func (tx *txStore) ListAllVersions(ctx context.Context, limit int, cursor string) ([]store.Version, string, error) {
	return tx.index.ListAllVersions(ctx, limit, cursor)
}

// ListSummaries delegates to index.
func (tx *txStore) ListSummaries(ctx context.Context, kind, query string, refs []store.RefFilter) ([]store.Summary, error) {
	return tx.index.ListSummaries(ctx, kind, query, refs)
}

// ListIDs delegates to index transaction.
func (tx *txStore) ListIDs(ctx context.Context, kind string) ([]string, error) {
	return tx.index.ListIDs(ctx, kind)
}

// Counts delegates to index transaction.
func (tx *txStore) Counts(ctx context.Context) (map[string]int, error) {
	return tx.index.Counts(ctx)
}

// IndexReferences delegates to index transaction.
func (tx *txStore) IndexReferences(ctx context.Context, fromKind, fromID string, refs []store.Ref) error {
	return tx.index.IndexReferences(ctx, fromKind, fromID, refs)
}

// ListReferencedBy delegates to index transaction.
func (tx *txStore) ListReferencedBy(ctx context.Context, fromKind, fromID string) ([]store.Ref, error) {
	return tx.index.ListReferencedBy(ctx, fromKind, fromID)
}

// ListReferencing delegates to index transaction.
func (tx *txStore) ListReferencing(ctx context.Context, toKind, toID string) ([]store.Summary, error) {
	return tx.index.ListReferencing(ctx, toKind, toID)
}

// WithinTransaction on a txStore calls fn with itself (no nesting).
func (tx *txStore) WithinTransaction(ctx context.Context, fn func(ctx context.Context, txInner store.ManifestStore) error) error {
	return fn(ctx, tx)
}

// Exclude for txStore delegates to the parent manifest store.
func (tx *txStore) Exclude(ctx context.Context, kind, id, name, reason, operator string) error {
	return tx.parent.Exclude(ctx, kind, id, name, reason, operator)
}

// ListExcluded for txStore delegates to the parent manifest store.
func (tx *txStore) ListExcluded(ctx context.Context) ([]store.Exclusion, error) {
	return tx.parent.ListExcluded(ctx)
}

// Recover for txStore delegates to the parent manifest store.
func (tx *txStore) Recover(ctx context.Context, kind, id, reason, operator string) error {
	return tx.parent.Recover(ctx, kind, id, reason, operator)
}

// writeSnapshotMarker writes .cartograph/last-snapshot.txt with the version marker.
func (m *ManifestStore) writeSnapshotMarker(ctx context.Context, v *store.Version) error {
	if v.Number <= 0 {
		// Don't write marker for non-versioned content
		return nil
	}

	cartographDir := filepath.Join(m.vaultDir, ".cartograph")
	if err := os.MkdirAll(cartographDir, 0755); err != nil {
		return fmt.Errorf("create .cartograph dir: %w", err)
	}

	markerPath := filepath.Join(cartographDir, "last-snapshot.txt")
	content := fmt.Sprintf("%s %s v%d: %s\n", v.Kind, v.ID, v.Number, v.Reason)
	return os.WriteFile(markerPath, []byte(content), 0644)
}

// ext is the manifest files' extension, with its dot.
func (m *ManifestStore) ext() string { return m.extension }

// VaultDir returns the vault root directory.
func (m *ManifestStore) VaultDir() string {
	return m.vaultDir
}

// IndexStatus returns information about the vault index: when it was last rebuilt
// and how many files are stale (have a different SHA-256 than recorded).
func (m *ManifestStore) IndexStatus(ctx context.Context) (rebuiltOn time.Time, staleCount int, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get rebuild time from the index's meta facts
	rebuiltOnStr, found, err := m.index.GetMeta(ctx, "rebuilt_on")
	if err != nil {
		return time.Time{}, 0, err
	}
	if found {
		rebuiltOn, _ = time.Parse(timeLayout, rebuiltOnStr)
	}

	// Count stale files: files on disk with different SHA-256 than recorded
	staleCount = 0
	entries, err := os.ReadDir(m.vaultDir)
	if err != nil {
		return rebuiltOn, 0, fmt.Errorf("read vault dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == ".cartograph" {
			continue
		}
		kind := entry.Name()
		kindDir := filepath.Join(m.vaultDir, kind)
		kindEntries, err := os.ReadDir(kindDir)
		if err != nil {
			return rebuiltOn, 0, fmt.Errorf("read kind dir %s: %w", kind, err)
		}

		for _, fe := range kindEntries {
			if !fe.IsDir() && strings.HasSuffix(fe.Name(), m.ext()) && fe.Name() != "vault.yaml" {
				id := strings.TrimSuffix(fe.Name(), m.ext())
				path := filepath.Join(kindDir, fe.Name())
				data, err := os.ReadFile(path)
				if err != nil {
					continue // Skip files that can't be read
				}

				hash := sha256String(data)
				// Query recorded hash
				recorded, found, err := m.index.GetFileHash(ctx, kind, id)
				if err == nil && !found {
					// File not in index, counts as stale
					staleCount++
				} else if err == nil && hash != recorded {
					// Hash differs from recorded, file is stale
					staleCount++
				}
			}
		}
	}

	return rebuiltOn, staleCount, nil
}

// Rehydrate reloads the in-memory state from vault.yaml and included manifests.
// Used after applying a new manifest to load it into the cache immediately.
func (m *ManifestStore) Rehydrate(ctx context.Context) error {
	return m.rehydrate(ctx)
}

// Close stops the watcher (if running) and closes the index.
func (m *ManifestStore) Close() error {
	if m.watcher != nil {
		m.watcher.Close()
		<-m.watchDone
	}
	return m.index.Close()
}

// Index returns the vault's index: versions, summaries, references, the
// apply journal, project state history and the rest of what a database
// holds beside the vault's files. Callers that need the operational store
// or the apply journal go through this rather than opening the index
// themselves.
func (m *ManifestStore) Index() store.VaultIndex {
	return m.index
}

// Exclude removes a manifest from vault.yaml and records the exclusion.
func (m *ManifestStore) Exclude(ctx context.Context, kind, id, name, reason, operator string) error {
	// Read current vault.yaml
	vault, err := m.readVaultManifest(ctx)
	if err != nil {
		return err
	}

	ref := kind + "/" + id
	var found bool
	var newInclude []string

	// Remove from include list
	for _, existing := range vault.Spec.Include {
		if existing != ref {
			newInclude = append(newInclude, existing)
		} else {
			found = true
		}
	}

	if !found {
		// Already excluded or not present; still allow it
		newInclude = vault.Spec.Include
	}

	vault.Spec.Include = newInclude

	// Serialize updated vault.yaml
	vaultYAMLBytes, err := m.getVaultYAMLBytes(vault)
	if err != nil {
		return err
	}

	// Create ApplyUnit with vault.yaml only (manifest file is not modified, only excluded)
	unit := store.ApplyUnit{
		Operator: operator,
		Reason:   reason,
		Files: []store.ApplyFile{
			{
				Path:    "vault.yaml",
				SHA256:  sha256String(vaultYAMLBytes),
				Content: vaultYAMLBytes,
			},
		},
	}

	// Execute the apply unit
	if err := m.applyUnit(ctx, unit); err != nil {
		return err
	}

	// Record exclusion in the index
	if err := m.index.Exclude(ctx, kind, id, name, reason, operator); err != nil {
		return err
	}

	// Clear outgoing references for the excluded manifest
	if err := m.index.IndexReferences(ctx, kind, id, []store.Ref{}); err != nil {
		return err
	}

	// Remove from cache if present
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cache, kind+"/"+id)
	delete(m.hashes, kind+"/"+id)

	return nil
}

// ListExcluded returns every excluded manifest, newest first.
func (m *ManifestStore) ListExcluded(ctx context.Context) ([]store.Exclusion, error) {
	return m.index.ListExcluded(ctx)
}

// Recover re-adds an excluded manifest to vault.yaml.
func (m *ManifestStore) Recover(ctx context.Context, kind, id, reason, operator string) error {
	// Check that the file still exists
	path := filepath.Join(m.vaultDir, kind, id+m.ext())
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("cannot recover %s/%s: file not found on disk", kind, id)
	}

	// Read manifest from disk and current vault.yaml
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read manifest file: %w", err)
	}

	vault, err := m.readVaultManifest(ctx)
	if err != nil {
		return err
	}

	ref := kind + "/" + id
	var found bool

	// Check if already included
	for _, existing := range vault.Spec.Include {
		if existing == ref {
			found = true
			break
		}
	}

	if !found {
		// Add to include list
		vault.Spec.Include = append(vault.Spec.Include, ref)
	}

	// Serialize updated vault.yaml
	vaultYAMLBytes, err := m.getVaultYAMLBytes(vault)
	if err != nil {
		return err
	}

	// Create ApplyUnit with vault.yaml only (manifest file stays unchanged, only re-included)
	unit := store.ApplyUnit{
		Operator: operator,
		Reason:   reason,
		Files: []store.ApplyFile{
			{
				Path:    "vault.yaml",
				SHA256:  sha256String(vaultYAMLBytes),
				Content: vaultYAMLBytes,
			},
		},
	}

	// Execute the apply unit
	if err := m.applyUnit(ctx, unit); err != nil {
		return err
	}

	// Remove exclusion record from the index
	if err := m.index.Recover(ctx, kind, id, reason, operator); err != nil {
		return err
	}

	// Load the manifest into cache
	hash := sha256String(data)
	key := kind + "/" + id

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache[key] = &store.Version{
		Kind:   kind,
		ID:     id,
		Number: 0,
		YAML:   data,
	}
	m.hashes[key] = hash

	return nil
}

// applyUnit executes a complete apply-unit sequence: write the unit to the journal
// with applied=false, write all files (temp+fsync+rename, vault.yaml last), then mark
// applied and update the index in one transaction. This is the only path through which
// manifest files are written.
//
// applyUnit is idempotent by content hash: if a file already exists with the correct
// sha256, it is skipped. This makes replay safe after a crash at any point.
func (m *ManifestStore) applyUnit(ctx context.Context, unit store.ApplyUnit) error {
	// Step 1: Write the unit to the journal with applied=false
	unit.ID = newUnitID()
	unit.On = time.Now().UTC()
	unit.Applied = false

	if err := m.index.Journal().PutUnit(ctx, unit); err != nil {
		return fmt.Errorf("put unit: %w", err)
	}

	// TEST: Inject failure after journal commit, before file writes
	if m.failurePoint == FailurePointAfterJournal {
		return fmt.Errorf("injected failure after journal commit")
	}

	// Step 2: Write all files (temp file, fsync, rename; vault.yaml last)
	if err := m.writeApplyUnitFilesWithFailure(ctx, unit); err != nil {
		return err
	}

	// Step 3: Mark applied and update index in one transaction
	if err := m.index.Journal().WithinTransaction(ctx, func(ctx context.Context, txJournal store.ApplyJournal) error {
		return txJournal.MarkApplied(ctx, unit.ID)
	}); err != nil {
		return fmt.Errorf("mark applied: %w", err)
	}

	return nil
}

// writeApplyUnitFilesWithFailure writes all files in a unit, injecting failures for testing.
// Wraps writeApplyUnitFiles and checks failurePoint for test injection.
func (m *ManifestStore) writeApplyUnitFilesWithFailure(ctx context.Context, unit store.ApplyUnit) error {
	// Separate vault.yaml from other files
	var vaultFile *store.ApplyFile
	var otherFiles []store.ApplyFile

	for i := range unit.Files {
		if unit.Files[i].Path == "vault.yaml" {
			vaultFile = &unit.Files[i]
		} else {
			otherFiles = append(otherFiles, unit.Files[i])
		}
	}

	// Write non-vault files first
	for _, af := range otherFiles {
		if err := m.writeFileFromApplyFile(ctx, af); err != nil {
			return err
		}
	}

	// TEST: Inject failure after first file write, before vault.yaml
	if m.failurePoint == FailurePointAfterFirstFile && len(otherFiles) > 0 {
		return fmt.Errorf("injected failure after first file write")
	}

	// Write vault.yaml last
	if vaultFile != nil {
		if err := m.writeFileFromApplyFile(ctx, *vaultFile); err != nil {
			return err
		}
	}

	// TEST: Inject failure after vault.yaml write, before mark applied
	if m.failurePoint == FailurePointAfterLastFile {
		return fmt.Errorf("injected failure after last file write")
	}

	return nil
}

// writeApplyUnitFiles writes all files in a unit, skipping any whose sha256 already matches.
// Vault.yaml is written last. Each file is written to a temp file, fsynced, then renamed.
func (m *ManifestStore) writeApplyUnitFiles(ctx context.Context, unit store.ApplyUnit) error {
	// Separate vault.yaml from other files
	var vaultFile *store.ApplyFile
	var otherFiles []store.ApplyFile

	for i := range unit.Files {
		if unit.Files[i].Path == "vault.yaml" {
			vaultFile = &unit.Files[i]
		} else {
			otherFiles = append(otherFiles, unit.Files[i])
		}
	}

	// Write non-vault files first
	for _, af := range otherFiles {
		if err := m.writeFileFromApplyFile(ctx, af); err != nil {
			return err
		}
	}

	// Write vault.yaml last
	if vaultFile != nil {
		if err := m.writeFileFromApplyFile(ctx, *vaultFile); err != nil {
			return err
		}
	}

	return nil
}

// writeFileFromApplyFile writes one file from an ApplyFile, skipping if its sha256 already matches.
// Uses temp file + fsync + rename.
func (m *ManifestStore) writeFileFromApplyFile(ctx context.Context, af store.ApplyFile) error {
	filePath := filepath.Join(m.vaultDir, af.Path)

	// Check if file already exists with matching sha256
	if data, err := os.ReadFile(filePath); err == nil {
		if sha256String(data) == af.SHA256 {
			// File already has correct content; skip
			return nil
		}
	}

	// Create directory if needed
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	// Write to temp file
	tempFile, err := os.CreateTemp(dir, ".cartograph-tmp-")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name()) // Clean up if rename fails

	if _, err := tempFile.Write(af.Content); err != nil {
		tempFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}

	// Fsync to ensure durability
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return fmt.Errorf("fsync temp file: %w", err)
	}

	tempFile.Close()

	// Atomic rename
	if err := os.Rename(tempFile.Name(), filePath); err != nil {
		return fmt.Errorf("rename temp file to %s: %w", filePath, err)
	}

	// Update in-memory state
	m.mu.Lock()
	defer m.mu.Unlock()

	// For vault.yaml, don't update cache
	if af.Path != "vault.yaml" {
		// Extract kind and id from path (e.g., "Goal/some-id.yaml" -> "Goal", "some-id")
		parts := strings.Split(af.Path, "/")
		if len(parts) == 2 && strings.HasSuffix(parts[1], m.ext()) {
			kind := parts[0]
			id := strings.TrimSuffix(parts[1], m.ext())
			key := kind + "/" + id
			hash := sha256String(af.Content)
			m.hashes[key] = hash
		}
	}

	return nil
}

// replayUnapplied replays every unit in the journal that has applied=false.
// For each file in each unit, if the file's sha256 already matches, skip;
// otherwise write it. Then mark applied. Replay is idempotent.
func (m *ManifestStore) replayUnapplied(ctx context.Context) error {
	units, err := m.index.Journal().ListUnapplied(ctx)
	if err != nil {
		return fmt.Errorf("list unapplied units: %w", err)
	}

	for _, unit := range units {
		// Replay this unit's files
		if err := m.writeApplyUnitFiles(ctx, unit); err != nil {
			return fmt.Errorf("replay unit %s: %w", unit.ID, err)
		}

		// Mark as applied
		if err := m.index.Journal().MarkApplied(ctx, unit.ID); err != nil {
			return fmt.Errorf("mark replayed unit %s as applied: %w", unit.ID, err)
		}
	}

	return nil
}

// pruneJournal removes old applied units, keeping only the most recent applyJournalMaxAge.
func (m *ManifestStore) pruneJournal(ctx context.Context) error {
	if m.applyJournalMaxAge <= 0 {
		return nil // No pruning
	}
	return m.index.Journal().PruneOld(ctx, m.applyJournalMaxAge)
}

func sha256String(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// newUnitID returns a random version 4 UUID (RFC 9562) for a journal unit,
// from crypto/rand, in the usual 8-4-4-4-12 hex form.
func newUnitID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // the system's randomness failing is not recoverable
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // the RFC 9562 variant
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
