package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/manifestmeta"
)

// The vault is the one adapter with an apply gate: a file can sit in
// <Kind>/<id>.yaml before vault.yaml includes it, and a person applies it
// from the Snapshots screen or the command line. These methods are the
// store.StateStore port over vault.yaml; the engine calls them and never
// reads the file itself.

var (
	_ store.StateStore  = (*ManifestStore)(nil)
	_ store.BundleStore = (*ManifestStore)(nil)
)

// Name is the state manifest's id: vault.yaml's metadata.id, or the
// directory's name until one is written.
func (m *ManifestStore) Name() string {
	if v, err := m.readVaultManifest(context.Background()); err == nil && v.Metadata.ID != "" {
		return v.Metadata.ID
	}
	return filepath.Base(m.vaultDir)
}

// Included returns vault.yaml's include list.
func (m *ManifestStore) Included(ctx context.Context) ([]string, error) {
	v, err := m.readVaultManifest(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(v.Spec.Include))
	copy(out, v.Spec.Include)
	return out, nil
}

// StateYAML returns vault.yaml's bytes as written on disk.
func (m *ManifestStore) StateYAML(context.Context) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(m.vaultDir, "vault.yaml"))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read vault.yaml: %w", err)
	}
	return data, true, nil
}

// ListUnapplied scans every <Kind>/<id>.yaml and returns the ones that
// vault.yaml does not include and no exclusion records.
func (m *ManifestStore) ListUnapplied(ctx context.Context) ([]store.UnappliedRef, error) {
	included, err := m.Included(ctx)
	if err != nil {
		return nil, err
	}
	skip := make(map[string]bool, len(included))
	for _, ref := range included {
		skip[ref] = true
	}
	exclusions, err := m.ListExcluded(ctx)
	if err != nil {
		return nil, err
	}
	for _, x := range exclusions {
		skip[x.Kind+"/"+x.ID] = true
	}

	entries, err := os.ReadDir(m.vaultDir)
	if err != nil {
		return nil, fmt.Errorf("read vault dir: %w", err)
	}
	out := []store.UnappliedRef{}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		kind := entry.Name()
		kindEntries, err := os.ReadDir(filepath.Join(m.vaultDir, kind))
		if err != nil {
			continue
		}
		for _, fe := range kindEntries {
			if fe.IsDir() || !strings.HasSuffix(fe.Name(), m.ext()) {
				continue
			}
			id := strings.TrimSuffix(fe.Name(), m.ext())
			if skip[kind+"/"+id] {
				continue
			}
			var name string
			if data, err := os.ReadFile(filepath.Join(m.vaultDir, kind, fe.Name())); err == nil {
				name = manifestmeta.Name(data)
			}
			out = append(out, store.UnappliedRef{Kind: kind, ID: id, Name: name})
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

// Apply adds refs to vault.yaml in one write. Every ref must have a file
// on disk, or nothing is written and the first missing one is returned as
// a store.NotFoundError. Refs already included are quiet.
func (m *ManifestStore) Apply(ctx context.Context, refs []string) (int, error) {
	for _, ref := range refs {
		kind, id, ok := strings.Cut(ref, "/")
		if !ok || kind == "" || id == "" {
			return 0, fmt.Errorf("invalid ref format: %s", ref)
		}
		if _, err := os.Stat(filepath.Join(m.vaultDir, kind, id+m.ext())); err != nil {
			return 0, &store.NotFoundError{Kind: kind, ID: id}
		}
	}

	v, err := m.readVaultManifest(ctx)
	if err != nil {
		return 0, err
	}
	already := make(map[string]bool, len(v.Spec.Include))
	for _, ref := range v.Spec.Include {
		already[ref] = true
	}
	added := 0
	for _, ref := range refs {
		if already[ref] {
			continue
		}
		already[ref] = true
		v.Spec.Include = append(v.Spec.Include, ref)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	if err := m.writeVaultManifest(v); err != nil {
		return 0, err
	}
	hash, err := m.vaultHash(ctx)
	if err != nil {
		return added, err
	}
	return added, m.recordVaultHash(ctx, hash)
}

// PutBundle writes a handoff bundle under .cartograph/handoff/<project>/v<n>,
// into a temporary directory first and renamed into place, so a crash
// leaves either the whole bundle or none of it.
func (m *ManifestStore) PutBundle(_ context.Context, projectID string, version int, files map[string][]byte) (store.Bundle, error) {
	tempDir, err := os.MkdirTemp(m.vaultDir, ".cartograph-handoff-tmp-")
	if err != nil {
		return store.Bundle{}, fmt.Errorf("create temp handoff directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tempDir, name), content, 0o644); err != nil {
			return store.Bundle{}, fmt.Errorf("write %s: %w", name, err)
		}
	}
	rel := filepath.Join(".cartograph", "handoff", projectID, fmt.Sprintf("v%d", version))
	dir := filepath.Join(m.vaultDir, rel)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return store.Bundle{}, fmt.Errorf("create handoff directory: %w", err)
	}
	if err := os.Rename(tempDir, dir); err != nil {
		return store.Bundle{}, fmt.Errorf("move handoff bundle into place: %w", err)
	}
	paths := make(map[string]string, len(files))
	for name := range files {
		paths[name] = filepath.Join(dir, name)
	}
	return store.Bundle{Location: rel, Paths: paths}, nil
}
