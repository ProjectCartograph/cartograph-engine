// Package memory is the in-memory ManifestStore and OperationalStore
// adapter used by tests and by the conformance suite. It has no
// persistence: state lives only as long as the process.
package memory

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/manifestmeta"
)

type manifestKey struct{ kind, id string }

// ManifestStore is the in-memory adapter for store.ManifestStore.
type ManifestStore struct {
	mu         sync.Mutex
	versions   map[manifestKey][]store.Version // append-only, index 0 is version 1
	refs       map[manifestKey][]store.Ref     // current outgoing references, replaced whole on each IndexReferences
	working    map[manifestKey][]byte          // working copies (autosave), keyed by kind/id
	exclusions map[manifestKey]store.Exclusion // excluded manifests, keyed by kind/id
}

func NewManifestStore() *ManifestStore {
	return &ManifestStore{
		versions:   map[manifestKey][]store.Version{},
		refs:       map[manifestKey][]store.Ref{},
		working:    map[manifestKey][]byte{},
		exclusions: map[manifestKey]store.Exclusion{},
	}
}

func (m *ManifestStore) PutVersion(_ context.Context, v store.Version) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.putLocked(v)
}

// PutWorking stores a manifest's working copy (autosave without versioning).
func (m *ManifestStore) PutWorking(_ context.Context, kind, id string, yamlBytes []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.working[manifestKey{kind, id}] = yamlBytes
	return nil
}

func (m *ManifestStore) putLocked(v store.Version) error {
	k := manifestKey{v.Kind, v.ID}
	existing := m.versions[k]
	wantNumber := len(existing) + 1
	if v.Number != wantNumber {
		return &store.NotFoundError{Kind: v.Kind, ID: v.ID}
	}
	m.versions[k] = append(existing, v)
	return nil
}

func (m *ManifestStore) GetCurrent(_ context.Context, kind, id string) (store.Version, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := manifestKey{kind, id}

	// Working copy is the current content when one exists
	if working, found := m.working[key]; found {
		return store.Version{
			Kind:   kind,
			ID:     id,
			Number: 0, // Working copies don't have version numbers
			YAML:   working,
		}, true, nil
	}

	// Fall back to latest snapshot
	vs := m.versions[key]
	if len(vs) == 0 {
		return store.Version{}, false, nil
	}
	return vs[len(vs)-1], true, nil
}

func (m *ManifestStore) GetVersion(_ context.Context, kind, id string, number int) (store.Version, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	vs := m.versions[manifestKey{kind, id}]
	if number < 1 || number > len(vs) {
		return store.Version{}, false, nil
	}
	return vs[number-1], true, nil
}

func (m *ManifestStore) ListVersions(_ context.Context, kind, id string) ([]store.Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	vs := m.versions[manifestKey{kind, id}]
	out := make([]store.Version, len(vs))
	copy(out, vs)
	return out, nil
}

func (m *ManifestStore) ListAllVersions(_ context.Context, limit int, cursor string) ([]store.Version, string, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Collect all versions from all manifests
	all := []store.Version{}
	for _, vs := range m.versions {
		all = append(all, vs...)
	}

	// Sort by on_ts descending (newest first), with tie-breaker
	sort.Slice(all, func(i, j int) bool {
		if !all[i].On.Equal(all[j].On) {
			return all[i].On.After(all[j].On)
		}
		if all[i].Kind != all[j].Kind {
			return all[i].Kind < all[j].Kind
		}
		if all[i].ID != all[j].ID {
			return all[i].ID < all[j].ID
		}
		return all[i].Number > all[j].Number
	})

	// Apply cursor if provided
	startIdx := 0
	if cursor != "" {
		// Cursor is encoded as "on_ts,kind,id,number"
		parts := strings.Split(cursor, ",")
		if len(parts) == 4 {
			// Find the position to start from (first item after the cursor)
			for i, v := range all {
				if v.On.Format(time.RFC3339Nano) == parts[0] &&
					v.Kind == parts[1] &&
					v.ID == parts[2] {
					// Parse number from cursor
					if curNum := parseInt(parts[3]); v.Number == curNum {
						startIdx = i + 1
						break
					}
				}
			}
		}
	}

	// Extract page
	end := startIdx + limit
	if end > len(all) {
		end = len(all)
	}
	page := all[startIdx:end]

	nextCursor := ""
	if end < len(all) {
		// We have more results, encode the last item as the next cursor
		last := page[len(page)-1]
		nextCursor = fmt.Sprintf("%s,%s,%s,%d", last.On.Format(time.RFC3339Nano), last.Kind, last.ID, last.Number)
	}

	return page, nextCursor, nil
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func (m *ManifestStore) ListSummaries(_ context.Context, kind, query string, refs []store.RefFilter) ([]store.Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []store.Summary{}
	q := strings.ToLower(strings.TrimSpace(query))
	seen := make(map[string]bool)

	// First, add versions
	for k, vs := range m.versions {
		if k.kind != kind || len(vs) == 0 {
			continue
		}
		if !m.matchesRefFiltersLocked(k, refs) {
			continue
		}
		cur := vs[len(vs)-1]
		name := manifestmeta.Name(cur.YAML)
		if q != "" && !strings.Contains(strings.ToLower(k.id), q) && !strings.Contains(strings.ToLower(name), q) {
			continue
		}
		out = append(out, store.Summary{
			Kind: k.kind, ID: k.id, Name: name, Labels: manifestmeta.Labels(cur.YAML),
			Version: cur.Number, UpdatedOn: cur.On,
		})
		seen[k.id] = true
	}

	// Then add working copies (unless already in versions)
	for k, working := range m.working {
		if k.kind != kind {
			continue
		}
		if seen[k.id] {
			continue // Already have a version for this ID
		}
		if !m.matchesRefFiltersLocked(k, refs) {
			continue
		}
		name := manifestmeta.Name(working)
		if q != "" && !strings.Contains(strings.ToLower(k.id), q) && !strings.Contains(strings.ToLower(name), q) {
			continue
		}
		out = append(out, store.Summary{
			Kind: k.kind, ID: k.id, Name: name, Labels: manifestmeta.Labels(working),
			Version: 0, UpdatedOn: time.Now(),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// matchesRefFiltersLocked reports whether the manifest at k carries an
// outgoing reference matching every filter (logical AND). Called with mu
// already held.
func (m *ManifestStore) matchesRefFiltersLocked(k manifestKey, filters []store.RefFilter) bool {
	if len(filters) == 0 {
		return true
	}
	have := m.refs[k]
	for _, f := range filters {
		found := false
		for _, r := range have {
			if r.ToKind == f.Kind && r.ToID == f.ID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (m *ManifestStore) IndexReferences(_ context.Context, fromKind, fromID string, refs []store.Ref) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]store.Ref, len(refs))
	copy(cp, refs)
	m.refs[manifestKey{fromKind, fromID}] = cp
	return nil
}

func (m *ManifestStore) ListReferencedBy(_ context.Context, fromKind, fromID string) ([]store.Ref, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	have := m.refs[manifestKey{fromKind, fromID}]
	out := make([]store.Ref, len(have))
	copy(out, have)
	return out, nil
}

func (m *ManifestStore) ListReferencing(_ context.Context, toKind, toID string) ([]store.Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []store.Summary{}
	seen := make(map[string]bool)

	for k, have := range m.refs {
		for _, r := range have {
			if r.ToKind == toKind && r.ToID == toID {
				key := k.kind + "/" + k.id
				if seen[key] {
					break // Already added
				}

				// Try to get from versions first
				vs := m.versions[k]
				if len(vs) > 0 {
					cur := vs[len(vs)-1]
					out = append(out, store.Summary{
						Kind: k.kind, ID: k.id, Name: manifestmeta.Name(cur.YAML),
						Version: cur.Number, UpdatedOn: cur.On,
					})
					seen[key] = true
					break
				}

				// Fall back to working copy
				if working, found := m.working[k]; found {
					out = append(out, store.Summary{
						Kind: k.kind, ID: k.id, Name: manifestmeta.Name(working),
						Version: 0, UpdatedOn: time.Now(),
					})
					seen[key] = true
					break
				}
			}
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

func (m *ManifestStore) ListIDs(_ context.Context, kind string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	seen := make(map[string]bool)

	// Add IDs from versions
	for k, vs := range m.versions {
		if k.kind == kind && len(vs) > 0 {
			out = append(out, k.id)
			seen[k.id] = true
		}
	}

	// Add IDs from working copies (if not already in versions)
	for k := range m.working {
		if k.kind == kind && !seen[k.id] {
			out = append(out, k.id)
		}
	}

	sort.Strings(out)
	return out, nil
}

func (m *ManifestStore) Counts(_ context.Context) (map[string]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]int{}
	seen := make(map[string]bool) // Track kind/id pairs

	// Count from versions
	for k, vs := range m.versions {
		if len(vs) > 0 {
			out[k.kind]++
			seen[k.kind+"/"+k.id] = true
		}
	}

	// Add working copies not in versions
	for k := range m.working {
		key := k.kind + "/" + k.id
		if !seen[key] {
			out[k.kind]++
		}
	}

	return out, nil
}

// WithinTransaction is a best-effort transaction: the in-memory store holds
// one global lock for the duration of fn, and rolls back by snapshotting
// and restoring state if fn returns an error. This gives the same
// all-or-nothing guarantee the SQLite adapter gives, without needing a real
// transaction manager for a map.
func (m *ManifestStore) WithinTransaction(ctx context.Context, fn func(ctx context.Context, tx store.ManifestStore) error) error {
	m.mu.Lock()
	versionsSnapshot := make(map[manifestKey][]store.Version, len(m.versions))
	for k, vs := range m.versions {
		cp := make([]store.Version, len(vs))
		copy(cp, vs)
		versionsSnapshot[k] = cp
	}
	refsSnapshot := make(map[manifestKey][]store.Ref, len(m.refs))
	for k, rs := range m.refs {
		cp := make([]store.Ref, len(rs))
		copy(cp, rs)
		refsSnapshot[k] = cp
	}
	m.mu.Unlock()

	if err := fn(ctx, m); err != nil {
		m.mu.Lock()
		m.versions = versionsSnapshot
		m.refs = refsSnapshot
		m.mu.Unlock()
		return err
	}
	return nil
}

// OperationalStore is the in-memory adapter for store.OperationalStore.
type OperationalStore struct {
	mu            sync.Mutex
	projectStates map[string][]store.ProjectStateEntry // append-only, index 0 is the first transition
}

func NewOperationalStore() *OperationalStore {
	return &OperationalStore{
		projectStates: map[string][]store.ProjectStateEntry{},
	}
}

func (o *OperationalStore) PutProjectStateTransition(_ context.Context, e store.ProjectStateEntry) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.projectStates[e.ProjectID] = append(o.projectStates[e.ProjectID], e)
	return nil
}

func (o *OperationalStore) ListProjectStateHistory(_ context.Context, projectID string) ([]store.ProjectStateEntry, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	have := o.projectStates[projectID]
	out := make([]store.ProjectStateEntry, len(have))
	copy(out, have)
	return out, nil
}

// GetWorking returns a manifest's working copy (autosave without versioning).
// Returns found=false when none exists.
func (m *ManifestStore) GetWorking(_ context.Context, kind, id string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	yaml, found := m.working[manifestKey{kind, id}]
	return yaml, found, nil
}

// Exclude records an exclusion for a manifest.
func (m *ManifestStore) Exclude(_ context.Context, kind, id, name, reason, operator string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exclusions[manifestKey{kind, id}] = store.Exclusion{
		Kind:     kind,
		ID:       id,
		Name:     name,
		On:       time.Now(),
		Reason:   reason,
		Operator: operator,
	}
	return nil
}

// ListExcluded returns every excluded manifest, newest first.
func (m *ManifestStore) ListExcluded(_ context.Context) ([]store.Exclusion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var exclusions []store.Exclusion
	for _, excl := range m.exclusions {
		exclusions = append(exclusions, excl)
	}

	// Sort by On descending (newest first)
	sort.Slice(exclusions, func(i, j int) bool {
		return exclusions[i].On.After(exclusions[j].On)
	})

	return exclusions, nil
}

// Recover deletes an exclusion record.
func (m *ManifestStore) Recover(_ context.Context, kind, id, reason, operator string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.exclusions, manifestKey{kind, id})
	return nil
}
