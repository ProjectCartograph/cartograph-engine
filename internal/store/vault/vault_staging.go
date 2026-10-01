package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Unfinished work does not belong in the vault.
//
// Autosave used to write straight to <vault>/<Kind>/<id>.yaml and add the
// ref to vault.yaml, which meant three things nobody asked for: a
// half-answered definition became part of the vault the moment somebody
// opened the wizard, a hand-maintained file was reformatted under its
// author, and "discard draft" had nothing to go back to because the
// working copy *was* the file.
//
// So a draft is written to <vault>/.cartograph/staging/<Kind>/<id>.yaml
// instead, and the vault's own tree is not touched until somebody saves.
// Staging lives inside .cartograph/ because every directory scan in this
// package already skips it, so a draft is invisible to the vault scan, to
// the unapplied listing and to export — which is exactly what it should
// be.
//
// Promotion is the existing save path: PutVersion writes the vault file
// through the journalled apply unit (write to a temp file, fsync, rename)
// and then clears the draft. So the move is atomic and survives a crash
// either side of it: the draft is still there if the rename did not
// happen, and the vault file is correct if it did.

const stagingRoot = ".cartograph/staging"

func (m *ManifestStore) stagingDir() string {
	return filepath.Join(m.vaultDir, filepath.FromSlash(stagingRoot))
}

func (m *ManifestStore) stagingPath(kind, id string) string {
	return filepath.Join(m.stagingDir(), kind, id+m.ext())
}

// writeStaged puts a draft in staging, atomically, so a crash mid-write
// cannot leave a half-written draft where a whole one was.
func (m *ManifestStore) writeStaged(kind, id string, yamlBytes []byte) error {
	path := m.stagingPath(kind, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cartograph-staged-")
	if err != nil {
		return fmt.Errorf("create staging temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(yamlBytes); err != nil {
		tmp.Close()
		return fmt.Errorf("write staging temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("fsync staging temp file: %w", err)
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename staging temp file: %w", err)
	}
	return nil
}

// readStaged returns the draft for one ref, if there is one.
func (m *ManifestStore) readStaged(kind, id string) ([]byte, bool, error) {
	data, err := os.ReadFile(m.stagingPath(kind, id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read staged draft: %w", err)
	}
	return data, true, nil
}

// clearStaged removes a draft, and the kind directory with it when that
// was the last draft of the kind, so staging does not silt up with empty
// directories. A draft that is not there is not an error: promotion runs
// after a save whether or not a draft existed.
func (m *ManifestStore) clearStaged(kind, id string) error {
	if err := os.Remove(m.stagingPath(kind, id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove staged draft: %w", err)
	}
	// Remove only if empty; Remove on a non-empty directory fails, which
	// is the test.
	_ = os.Remove(filepath.Join(m.stagingDir(), kind))
	return nil
}

// StagedRef is one draft waiting to be saved.
type StagedRef struct {
	Kind string
	ID   string
}

// ListStaged returns every draft in the vault, so a caller can say what is
// unfinished without walking the directory itself. Sorted, because two
// runs listing the same drafts in a different order is a bug nobody wants
// to chase.
func (m *ManifestStore) ListStaged(ctx context.Context) ([]StagedRef, error) {
	kinds, err := os.ReadDir(m.stagingDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read staging directory: %w", err)
	}
	var out []StagedRef
	for _, kindEntry := range kinds {
		if !kindEntry.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(m.stagingDir(), kindEntry.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), m.ext()) {
				continue
			}
			out = append(out, StagedRef{
				Kind: kindEntry.Name(),
				ID:   strings.TrimSuffix(f.Name(), m.ext()),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// loadStagedIntoCache puts every draft back in front of the vault file it
// is a draft of, so closing the browser or restarting the server does not
// silently lose unfinished work. Called on open, after the vault scan, so
// a draft wins over the file it will replace.
func (m *ManifestStore) loadStagedIntoCache(ctx context.Context) error {
	staged, err := m.ListStaged(ctx)
	if err != nil {
		return err
	}
	for _, ref := range staged {
		data, found, err := m.readStaged(ref.Kind, ref.ID)
		if err != nil || !found {
			continue
		}
		m.mu.Lock()
		m.cache[ref.Kind+"/"+ref.ID] = &store.Version{
			Kind: ref.Kind, ID: ref.ID, Number: 0, YAML: data,
			Actor: "local", Reason: "working copy", On: time.Now().UTC(),
		}
		m.mu.Unlock()
		// The index's own working copy is what ListReferencing reads, so a
		// draft's references survive a restart too.
		if err := m.index.PutWorking(ctx, ref.Kind, ref.ID, data); err != nil {
			return fmt.Errorf("index staged draft %s/%s: %w", ref.Kind, ref.ID, err)
		}
	}
	return nil
}

// IndexWorking records a manifest in the index's working-copy table without
// staging anything.
//
// Reindex needs this: it walks every current manifest on open so
// ListReferencing can resolve references, and it used to do that by calling
// PutWorking. That was harmless while PutWorking wrote the vault file the
// manifest had just been read from. Now PutWorking stages a draft, so the
// same call made opening a vault create a draft of every manifest in it —
// sixty-five of them in the example instance, none of them anybody's work.
//
// So the two jobs PutWorking was doing are separated: staging what somebody
// is editing, and telling the index what exists.
func (m *ManifestStore) IndexWorking(ctx context.Context, kind, id string, yamlBytes []byte) error {
	return m.index.PutWorking(ctx, kind, id, yamlBytes)
}

// DiscardWorking throws a draft away and puts back what the vault holds.
//
// This is the operation that could not exist before staging: the working
// copy was the file, so there was nothing to go back to. Now there is —
// the vault's own file, untouched since the last save. A ref with no vault
// file was never saved at all, so discarding it leaves nothing, which is
// the honest answer rather than an empty manifest.
func (m *ManifestStore) DiscardWorking(ctx context.Context, kind, id string) error {
	if err := m.clearStaged(kind, id); err != nil {
		return err
	}
	key := kind + "/" + id
	saved, err := os.ReadFile(filepath.Join(m.vaultDir, kind, id+m.ext()))
	m.mu.Lock()
	if err != nil {
		delete(m.cache, key)
		delete(m.hashes, key)
	} else {
		m.cache[key] = &store.Version{
			Kind: kind, ID: id, Number: 0, YAML: saved,
			Actor: "local", Reason: "file", On: time.Now().UTC(),
		}
		m.hashes[key] = sha256String(saved)
	}
	m.mu.Unlock()
	return nil
}
