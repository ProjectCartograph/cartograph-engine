package vault_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/conformance"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/sqlite"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/vault"
)

func TestManifestStoreConformance(t *testing.T) {
	conformance.RunManifestStore(t, func(t *testing.T) store.ManifestStore {
		dir := t.TempDir()
		v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { v.Close() })
		return v
	})
}

// TestWatchReloads tests that files written externally are reloaded into the cache.
func TestWatchReloads(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: true, Debounce: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// Create the Team directory first and wait for watcher to pick it up
	teamDir := filepath.Join(dir, "Team")
	if err := os.MkdirAll(teamDir, 0755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	// Test case 1: Write file through PutVersion (adds to vault.yaml, then watcher reloads)
	filePath := filepath.Join(teamDir, "t1.yaml")
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")

	// Write via PutVersion so it gets added to vault.yaml
	err = v.PutVersion(ctx, store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 1,
		YAML:   yaml1,
		Actor:  "test",
		Reason: "test",
		On:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for watcher to pick it up
	time.Sleep(50 * time.Millisecond)

	// Verify it was loaded into the cache
	cur, found, err := v.GetCurrent(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected file to be loaded by watcher")
	}
	if string(cur.YAML) != string(yaml1) {
		t.Fatalf("expected YAML to match, got %q", cur.YAML)
	}

	// Test case 2: Simulate editor temp-file rename (sed behavior)
	// Write to temp file, rename over the original
	tempPath := filepath.Join(teamDir, "t1.yaml.tmp")
	yaml2 := []byte("metadata:\n  id: t1\n  name: Team One Renamed\n")
	if err := os.WriteFile(tempPath, yaml2, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tempPath, filePath); err != nil {
		t.Fatal(err)
	}

	// Wait for watcher to pick up the rename
	time.Sleep(50 * time.Millisecond)

	// Verify the renamed file was reloaded
	cur2, found2, err := v.GetCurrent(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !found2 {
		t.Fatal("expected renamed file to be reloaded")
	}
	if string(cur2.YAML) != string(yaml2) {
		t.Fatalf("expected YAML to match after rename, got %q", cur2.YAML)
	}
	// File content is loaded but not versioned; it should have Number=0 (withdrawn I3.3a auto-versioning)
	if cur2.Number != 0 {
		t.Fatalf("expected file content (version 0) after rename, got %d", cur2.Number)
	}

	// Test case 3: File removal (delete from cache)
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}

	// Wait for watcher to pick up the removal
	time.Sleep(50 * time.Millisecond)

	// Verify it was removed from the cache
	_, found3, err := v.GetCurrent(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if found3 {
		t.Fatal("expected file to be removed from cache after deletion")
	}
}

// TestConflictError tests that PutVersion returns ConflictError when a file changes on disk.
func TestConflictError(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// Write a version
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	v1 := store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 1,
		YAML:   yaml1,
		Actor:  "a1",
		Reason: "seed",
		On:     time.Now().UTC(),
	}
	if err := v.PutVersion(ctx, v1); err != nil {
		t.Fatal(err)
	}

	// Modify the file on disk directly
	filePath := filepath.Join(dir, "Team", "t1.yaml")
	yaml2 := []byte("metadata:\n  id: t1\n  name: Team One Modified\n")
	if err := os.WriteFile(filePath, yaml2, 0644); err != nil {
		t.Fatal(err)
	}

	// Try to put a new version; should get ConflictError
	v2 := store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 2,
		YAML:   yaml1,
		Actor:  "a1",
		Reason: "update",
		On:     time.Now().UTC(),
	}
	err = v.PutVersion(ctx, v2)
	if err == nil {
		t.Fatal("expected ConflictError")
	}

	conflict, ok := err.(*store.ConflictError)
	if !ok {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
	if conflict.Kind != "Team" || conflict.ID != "t1" {
		t.Fatalf("wrong conflict details: %v", conflict)
	}
	if string(conflict.Ours) != string(yaml1) {
		t.Fatalf("Ours should be yaml1")
	}
	if string(conflict.Theirs) != string(yaml2) {
		t.Fatalf("Theirs should be yaml2")
	}
}

// TestIndexRebuild tests that deleting .cartograph/ and reopening rebuilds the index.
func TestIndexRebuild(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Write two versions
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	v.PutVersion(ctx, store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 1,
		YAML:   yaml1,
		Actor:  "a1",
		Reason: "seed",
		On:     time.Now().UTC(),
	})

	yaml2 := []byte("metadata:\n  id: t1\n  name: Team One Updated\n")
	v.PutVersion(ctx, store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 2,
		YAML:   yaml2,
		Actor:  "a1",
		Reason: "update",
		On:     time.Now().UTC(),
	})
	v.Close()

	// Delete .cartograph directory
	if err := os.RemoveAll(filepath.Join(dir, ".cartograph")); err != nil {
		t.Fatal(err)
	}

	// Reopen the vault
	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v2.Close() })

	// Verify the file content is still there (loaded from disk, not versioned - withdrawn I3.3a)
	cur, found, err := v2.GetCurrent(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected file content after index rebuild")
	}
	// File should be loaded with Number=0 (not auto-versioned)
	if cur.Number != 0 {
		t.Fatalf("expected file content (version 0) after rebuild, got %d", cur.Number)
	}
	if string(cur.YAML) != string(yaml2) {
		t.Fatalf("expected yaml2, got %q", cur.YAML)
	}

	// Verify version history is empty (no auto-versioning from disk read)
	vs, err := v2.ListVersions(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("expected 0 versions after rebuild (no auto-versioning), got %d", len(vs))
	}
}

// TestTransactionalConflictWithoutWatch tests that a transactional PUT refuses a stale write
// when a file is edited externally (Watch: false, external edit detection via hash).
func TestTransactionalConflictWithoutWatch(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// Commit a manifest through the engine (transactional path)
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	err = v.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		return tx.PutVersion(ctx, store.Version{
			Kind:   "Team",
			ID:     "t1",
			Number: 1,
			YAML:   yaml1,
			Actor:  "a1",
			Reason: "seed",
			On:     time.Now().UTC(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify file was written to disk
	filePath := filepath.Join(dir, "Team", "t1.yaml")
	diskData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(diskData) != string(yaml1) {
		t.Fatalf("expected file to contain yaml1, got %q", diskData)
	}

	// Overwrite the file on disk with different bytes
	yaml2 := []byte("metadata:\n  id: t1\n  name: Team One Modified\n")
	if err := os.WriteFile(filePath, yaml2, 0644); err != nil {
		t.Fatal(err)
	}

	// Try another transactional commit with the old content; should get ConflictError
	err = v.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		return tx.PutVersion(ctx, store.Version{
			Kind:   "Team",
			ID:     "t1",
			Number: 2,
			YAML:   yaml1, // Trying to commit old content again
			Actor:  "a1",
			Reason: "update",
			On:     time.Now().UTC(),
		})
	})
	if err == nil {
		t.Fatal("expected ConflictError on stale transactional write")
	}

	conflict, ok := err.(*store.ConflictError)
	if !ok {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
	if string(conflict.Theirs) != string(yaml2) {
		t.Fatalf("expected Theirs to be the disk content (yaml2), got %q", conflict.Theirs)
	}

	// Verify file on disk was not overwritten
	diskDataAfter, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(diskDataAfter) != string(yaml2) {
		t.Fatalf("expected file to still have yaml2, got %q", diskDataAfter)
	}
}

// TestTransactionalSuccessWithWatch tests that after an external edit is detected by the watcher,
// a new transactional commit succeeds with the next version number.
func TestTransactionalSuccessWithWatch(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: true, Debounce: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// Create the Team directory for watcher
	teamDir := filepath.Join(dir, "Team")
	if err := os.MkdirAll(teamDir, 0755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	// Commit a manifest through the engine (transactional path)
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	err = v.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		return tx.PutVersion(ctx, store.Version{
			Kind:   "Team",
			ID:     "t1",
			Number: 1,
			YAML:   yaml1,
			Actor:  "a1",
			Reason: "seed",
			On:     time.Now().UTC(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Edit the file on disk (watcher will reload it as version 2)
	filePath := filepath.Join(dir, "Team", "t1.yaml")
	yaml2 := []byte("metadata:\n  id: t1\n  name: Team One Modified\n")
	if err := os.WriteFile(filePath, yaml2, 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for watcher to detect and reload
	time.Sleep(50 * time.Millisecond)

	// Verify watcher detected the change and loaded the file (not versioned - withdrawn I3.3a)
	cur, found, err := v.GetCurrent(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected file to be in cache")
	}
	// File should be loaded with Number=0 (not auto-versioned)
	if cur.Number != 0 {
		t.Fatalf("expected watcher to load file content (version 0), got %d", cur.Number)
	}

	// Now commit a new version through the transactional path; should succeed with version 2 (since version 1 exists in index)
	yaml3 := []byte("metadata:\n  id: t1\n  name: Team One Final\n")
	err = v.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		return tx.PutVersion(ctx, store.Version{
			Kind:   "Team",
			ID:     "t1",
			Number: 2,
			YAML:   yaml3,
			Actor:  "a1",
			Reason: "final",
			On:     time.Now().UTC(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify the new version is in cache and on disk
	cur3, found3, err := v.GetCurrent(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !found3 {
		t.Fatal("expected version 2 in cache")
	}
	if cur3.Number != 2 {
		t.Fatalf("expected version 2, got %d", cur3.Number)
	}
	if string(cur3.YAML) != string(yaml3) {
		t.Fatalf("expected yaml3, got %q", cur3.YAML)
	}

	diskData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(diskData) != string(yaml3) {
		t.Fatalf("expected file to contain yaml3, got %q", diskData)
	}
}

// TestStrayFilesExcludedFromLiveState tests that files on disk but not in vault.yaml
// are excluded from the live state (not in GetCurrent, ListSummaries, or the tree),
// but are listed as unapplied.
func TestStrayFilesExcludedFromLiveState(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// Create a stray file on disk (not in vault.yaml)
	goalDir := filepath.Join(dir, "Goal")
	if err := os.MkdirAll(goalDir, 0755); err != nil {
		t.Fatal(err)
	}
	strayYAML := []byte("apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: stray-goal\n  name: Stray Goal\n")
	if err := os.WriteFile(filepath.Join(goalDir, "stray-goal.yaml"), strayYAML, 0644); err != nil {
		t.Fatal(err)
	}

	// Reopen the vault (vault.yaml will be generated without the stray)
	v.Close()
	v, err = vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	// The stray file should NOT be in GetCurrent
	cur, found, err := v.GetCurrent(ctx, "Goal", "stray-goal")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected stray file not to be found in GetCurrent, but got %+v", cur)
	}

	// The stray file should NOT be in ListSummaries
	summaries, err := v.ListSummaries(ctx, "Goal", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range summaries {
		if s.ID == "stray-goal" {
			t.Fatalf("expected stray file not to be in ListSummaries, but found %+v", s)
		}
	}

	// Check vault.yaml content to verify stray is not included
	vaultPath := filepath.Join(dir, "vault.yaml")
	vaultData, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(vaultData), "stray-goal") {
		t.Fatalf("expected stray-goal not to be in vault.yaml, but found it")
	}
}

// TestDraftStaysOutOfTheVault is the inverse of what this asserted until
// 2026-09-28, when autosave moved to a staging directory: a draft must not
// reach the vault's own tree or its include list. Opening a wizard is not a
// decision to include something.
func TestDraftStaysOutOfTheVault(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// Write a working copy
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	err = v.PutWorking(ctx, "Team", "t1", yaml1)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the file exists
	cur, found, err := v.GetCurrent(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected working copy to be in cache")
	}
	if string(cur.YAML) != string(yaml1) {
		t.Fatalf("expected yaml1, got %q", cur.YAML)
	}
	if cur.Number != 0 {
		t.Fatalf("expected working copy number 0, got %d", cur.Number)
	}

	// The vault's own tree is untouched: no file, and no include entry.
	if _, err := os.Stat(filepath.Join(dir, "Team", "t1.yaml")); !os.IsNotExist(err) {
		t.Fatalf("a draft wrote into the vault tree: %v", err)
	}
	vaultData, err := os.ReadFile(filepath.Join(dir, "vault.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(vaultData), "Team/t1") {
		t.Fatalf("a draft was included in vault.yaml:\n%s", vaultData)
	}
	// It is in staging, where GetWorking finds it.
	staged, found, err := v.GetWorking(ctx, "Team", "t1")
	if err != nil || !found {
		t.Fatalf("expected the draft to be readable: %v found=%v", err, found)
	}
	if string(staged) != string(yaml1) {
		t.Fatalf("expected the draft back, got %q", staged)
	}

	// Saving is what promotes it, and only then does it join the vault.
	if err := v.PutVersion(ctx, store.Version{
		Kind: "Team", ID: "t1", Number: 1, YAML: yaml1, Actor: "local", Reason: "save",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Team", "t1.yaml")); err != nil {
		t.Fatalf("a save did not write the vault file: %v", err)
	}
	vaultData, err = os.ReadFile(filepath.Join(dir, "vault.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(vaultData), "Team/t1") {
		t.Fatalf("expected Team/t1 in vault.yaml after a save:\n%s", vaultData)
	}
	// And the draft is gone: promotion leaves nothing behind to re-apply.
	if _, found, _ := v.GetWorking(ctx, "Team", "t1"); found {
		// GetWorking falls through to the file, so read staging directly.
		if _, err := os.Stat(filepath.Join(dir, ".cartograph", "staging", "Team", "t1.yaml")); !os.IsNotExist(err) {
			t.Fatalf("the draft outlived its promotion: %v", err)
		}
	}
}

// TestWorkingSaveRestartStillLive: unfinished work survives a restart.
// This is why a draft is a file rather than memory — closing the browser or
// restarting the server must not silently throw away a half-answered
// definition.
func TestWorkingSaveRestartStillLive(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Write a working copy
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	err = v.PutWorking(ctx, "Team", "t1", yaml1)
	if err != nil {
		t.Fatal(err)
	}

	// Still not in vault.yaml: a restart does not decide for anybody.
	vaultData, err := os.ReadFile(filepath.Join(dir, "vault.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(vaultData), "Team/t1") {
		t.Fatalf("a draft was included in vault.yaml:\n%s", vaultData)
	}

	// Close the vault
	v.Close()

	// Reopen the vault
	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v2.Close() })

	// Verify the working copy is still there
	cur, found, err := v2.GetCurrent(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected working copy to be in cache after restart")
	}
	if string(cur.YAML) != string(yaml1) {
		t.Fatalf("expected yaml1 after restart, got %q", cur.YAML)
	}
	if cur.Number != 0 {
		t.Fatalf("expected working copy number 0 after restart, got %d", cur.Number)
	}
}

// TestWorkingSaveSnapshot tests that snapshots work correctly after a working save.
func TestWorkingSaveSnapshot(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// Write a working copy
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	err = v.PutWorking(ctx, "Team", "t1", yaml1)
	if err != nil {
		t.Fatal(err)
	}

	// Take a snapshot; since working copy has Number=0, next version is 1
	v1 := store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 1,
		YAML:   yaml1,
		Actor:  "test",
		Reason: "snapshot after working save",
		On:     time.Now().UTC(),
	}
	err = v.PutVersion(ctx, v1)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the version was created
	vs, err := v.ListVersions(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("expected 1 version, got %d", len(vs))
	}
	if vs[0].Number != 1 {
		t.Fatalf("expected version 1, got %d", vs[0].Number)
	}

	// Verify current state is now version 1
	cur, found, err := v.GetCurrent(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected Team/t1 to exist")
	}
	if cur.Number != 1 {
		t.Fatalf("expected current version 1, got %d", cur.Number)
	}

	// Take another snapshot directly from current state (no working copy), should be version 2
	yaml2 := []byte("metadata:\n  id: t1\n  name: Team One Updated\n")
	v2 := store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 2,
		YAML:   yaml2,
		Actor:  "test",
		Reason: "second snapshot",
		On:     time.Now().UTC(),
	}
	err = v.PutVersion(ctx, v2)
	if err != nil {
		t.Fatal(err)
	}

	// Verify both versions exist
	vs, err = v.ListVersions(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(vs))
	}
	if vs[1].Number != 2 {
		t.Fatalf("expected version 2, got %d", vs[1].Number)
	}
}

// TestWorkingSaveOnExcluded tests that PutWorking rejects an excluded id with ConflictError (409).
func TestWorkingSaveOnExcluded(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// Create and exclude a manifest
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	err = v.PutVersion(ctx, store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 1,
		YAML:   yaml1,
		Actor:  "test",
		Reason: "seed",
		On:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Exclude the manifest
	err = v.Exclude(ctx, "Team", "t1", "Team One", "testing", "tester")
	if err != nil {
		t.Fatal(err)
	}

	// Try to write a working copy; should get ConflictError
	yaml2 := []byte("metadata:\n  id: t1\n  name: Team One Updated\n")
	err = v.PutWorking(ctx, "Team", "t1", yaml2)
	if err == nil {
		t.Fatal("expected ConflictError when writing to excluded manifest")
	}

	conflict, ok := err.(*store.ConflictError)
	if !ok {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
	if conflict.Kind != "Team" || conflict.ID != "t1" {
		t.Fatalf("wrong conflict details: %v", conflict)
	}
}

// TestVersionNumberFromLog tests that version numbers come from the version log, not the cache.
func TestVersionNumberFromLog(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Create version 1
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	err = v.PutVersion(ctx, store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 1,
		YAML:   yaml1,
		Actor:  "test",
		Reason: "v1",
		On:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create version 2
	yaml2 := []byte("metadata:\n  id: t1\n  name: Team One v2\n")
	err = v.PutVersion(ctx, store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 2,
		YAML:   yaml2,
		Actor:  "test",
		Reason: "v2",
		On:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Close and reopen to clear the cache
	v.Close()
	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v2.Close() })

	// Try to create version 3; it should be based on the log (which has v1 and v2),
	// not the cache (which may have been cleared)
	yaml3 := []byte("metadata:\n  id: t1\n  name: Team One v3\n")
	err = v2.PutVersion(ctx, store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 3,
		YAML:   yaml3,
		Actor:  "test",
		Reason: "v3",
		On:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify v3 was created
	vs, err := v2.ListVersions(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(vs))
	}
	if vs[2].Number != 3 {
		t.Fatalf("expected version 3, got %d", vs[2].Number)
	}
}

// TestStaleIndexVersionMismatch tests that a version-number mismatch is logged and returned.
func TestStaleIndexVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Create version 1
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	err = v.PutVersion(ctx, store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 1,
		YAML:   yaml1,
		Actor:  "test",
		Reason: "v1",
		On:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Close and reopen to clear the cache
	v.Close()
	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v2.Close() })

	// Try to create version 2 with the wrong number (say 1 instead of 2)
	yaml2 := []byte("metadata:\n  id: t1\n  name: Team One v2\n")
	err = v2.PutVersion(ctx, store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 1, // Wrong number!
		YAML:   yaml2,
		Actor:  "test",
		Reason: "v2 with wrong number",
		On:     time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected error for version number mismatch")
	}

	// Verify the error message contains the expected details
	if !strings.Contains(err.Error(), "expected number 2") {
		t.Fatalf("expected error about expected number 2, got: %v", err)
	}
	if !strings.Contains(err.Error(), "got 1") {
		t.Fatalf("expected error about got 1, got: %v", err)
	}
}

// TestRepeatedSnapshots tests that three snapshots in a row produce numbers 1, 2, 3.
func TestRepeatedSnapshots(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// First snapshot: version 1
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	v1 := store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 1,
		YAML:   yaml1,
		Actor:  "test",
		Reason: "first snapshot",
		On:     time.Now().UTC(),
	}
	err = v.PutVersion(ctx, v1)
	if err != nil {
		t.Fatalf("first snapshot failed: %v", err)
	}

	// Verify version 1 exists
	vs, err := v.ListVersions(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].Number != 1 {
		t.Fatalf("expected [v1], got %v", vs)
	}

	// Second snapshot: version 2
	yaml2 := []byte("metadata:\n  id: t1\n  name: Team One v2\n")
	v2 := store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 2,
		YAML:   yaml2,
		Actor:  "test",
		Reason: "second snapshot",
		On:     time.Now().UTC(),
	}
	err = v.PutVersion(ctx, v2)
	if err != nil {
		t.Fatalf("second snapshot failed: %v", err)
	}

	// Verify versions 1 and 2 exist
	vs, err = v.ListVersions(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 || vs[0].Number != 1 || vs[1].Number != 2 {
		t.Fatalf("expected [v1, v2], got %v", vs)
	}

	// Third snapshot: version 3
	yaml3 := []byte("metadata:\n  id: t1\n  name: Team One v3\n")
	v3 := store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 3,
		YAML:   yaml3,
		Actor:  "test",
		Reason: "third snapshot",
		On:     time.Now().UTC(),
	}
	err = v.PutVersion(ctx, v3)
	if err != nil {
		t.Fatalf("third snapshot failed: %v", err)
	}

	// Verify all three versions exist
	vs, err = v.ListVersions(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(vs))
	}
	if vs[0].Number != 1 || vs[1].Number != 2 || vs[2].Number != 3 {
		t.Fatalf("expected [v1, v2, v3], got %v", []int{vs[0].Number, vs[1].Number, vs[2].Number})
	}
}

// TestWorkingSaveBetweenSnapshots tests that a working save between snapshots does not change numbering.
func TestWorkingSaveBetweenSnapshots(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// First snapshot: version 1
	yaml1 := []byte("metadata:\n  id: t1\n  name: Team One\n")
	v1 := store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 1,
		YAML:   yaml1,
		Actor:  "test",
		Reason: "first snapshot",
		On:     time.Now().UTC(),
	}
	err = v.PutVersion(ctx, v1)
	if err != nil {
		t.Fatal(err)
	}

	// Working save (draft)
	yaml2 := []byte("metadata:\n  id: t1\n  name: Team One (draft)\n")
	err = v.PutWorking(ctx, "Team", "t1", yaml2)
	if err != nil {
		t.Fatal(err)
	}

	// Verify current content is the working copy
	cur, found, err := v.GetCurrent(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected working copy to be current")
	}
	if string(cur.YAML) != string(yaml2) {
		t.Fatalf("expected working copy, got %q", cur.YAML)
	}
	if cur.Number != 0 {
		t.Fatalf("expected working copy number 0, got %d", cur.Number)
	}

	// Second snapshot: version 2 (from the working copy YAML)
	v2 := store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 2,
		YAML:   yaml2,
		Actor:  "test",
		Reason: "second snapshot",
		On:     time.Now().UTC(),
	}
	err = v.PutVersion(ctx, v2)
	if err != nil {
		t.Fatalf("second snapshot failed: %v", err)
	}

	// Verify both versions exist
	vs, err := v.ListVersions(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(vs))
	}
	if vs[0].Number != 1 || vs[1].Number != 2 {
		t.Fatalf("expected [v1, v2], got %v", []int{vs[0].Number, vs[1].Number})
	}

	// Working save again
	yaml3 := []byte("metadata:\n  id: t1\n  name: Team One (draft v2)\n")
	err = v.PutWorking(ctx, "Team", "t1", yaml3)
	if err != nil {
		t.Fatal(err)
	}

	// Third snapshot: version 3
	v3 := store.Version{
		Kind:   "Team",
		ID:     "t1",
		Number: 3,
		YAML:   yaml3,
		Actor:  "test",
		Reason: "third snapshot",
		On:     time.Now().UTC(),
	}
	err = v.PutVersion(ctx, v3)
	if err != nil {
		t.Fatalf("third snapshot failed: %v", err)
	}

	// Verify all three versions exist
	vs, err = v.ListVersions(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(vs))
	}
	if vs[0].Number != 1 || vs[1].Number != 2 || vs[2].Number != 3 {
		t.Fatalf("expected [v1, v2, v3], got %v", []int{vs[0].Number, vs[1].Number, vs[2].Number})
	}
}

// TestTransactionalCommitAppliedToVault verifies that a manifest created via
// transactional WithinTransaction is added to vault.yaml (the defect fix).
func TestTransactionalCommitAppliedToVault(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// Create a manifest through WithinTransaction (simulates engine.Commit)
	yaml1 := []byte("metadata:\n  id: goal-one\n  name: Goal One\n")
	err = v.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		return tx.PutVersion(ctx, store.Version{
			Kind:   "Goal",
			ID:     "goal-one",
			Number: 1,
			YAML:   yaml1,
			Actor:  "test",
			Reason: "create",
			On:     time.Now().UTC(),
		})
	})
	if err != nil {
		t.Fatalf("transactional create failed: %v", err)
	}

	// Verify it's in the cache immediately
	cur, found, err := v.GetCurrent(ctx, "Goal", "goal-one")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected goal to be in cache after transactional commit")
	}
	if string(cur.YAML) != string(yaml1) {
		t.Fatalf("expected YAML to match, got %q", cur.YAML)
	}

	// Verify it's in vault.yaml
	vaultPath := filepath.Join(dir, "vault.yaml")
	vaultBytes, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatalf("read vault.yaml: %v", err)
	}
	if !strings.Contains(string(vaultBytes), "Goal/goal-one") {
		t.Fatalf("expected Goal/goal-one in vault.yaml, got:\n%s", vaultBytes)
	}

	// Now do a working save on another manifest (simulates a later operation that rewrites vault.yaml)
	yaml2 := []byte("metadata:\n  id: goal-two\n  name: Goal Two\n")
	err = v.PutWorking(ctx, "Goal", "goal-two", yaml2)
	if err != nil {
		t.Fatal(err)
	}

	// Verify goal-one is still in the tree (not lost by rehydration)
	cur, found, err = v.GetCurrent(ctx, "Goal", "goal-one")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected goal-one to still be in cache after working save elsewhere (goal-two)")
	}

	// The committed one is still included; the draft never was. What this
	// test is really about is that writing goal-two did not drop goal-one.
	vaultBytes, err = os.ReadFile(vaultPath)
	if err != nil {
		t.Fatalf("read vault.yaml: %v", err)
	}
	if !strings.Contains(string(vaultBytes), "Goal/goal-one") {
		t.Fatalf("a draft elsewhere dropped Goal/goal-one from vault.yaml:\n%s", vaultBytes)
	}
	if strings.Contains(string(vaultBytes), "Goal/goal-two") {
		t.Fatalf("a draft was included in vault.yaml:\n%s", vaultBytes)
	}
}

// TestTransactionalCommitSurvivesRestart verifies that a transactionally-created
// manifest persists across restart (the full defect fix).
func TestTransactionalCommitSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	// First instance: create via transactional commit
	{
		v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
		if err != nil {
			t.Fatal(err)
		}

		ctx := context.Background()
		yaml1 := []byte("metadata:\n  id: goal-one\n  name: Goal One\n")
		err = v.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
			return tx.PutVersion(ctx, store.Version{
				Kind:   "Goal",
				ID:     "goal-one",
				Number: 1,
				YAML:   yaml1,
				Actor:  "test",
				Reason: "create",
				On:     time.Now().UTC(),
			})
		})
		if err != nil {
			t.Fatalf("transactional create failed: %v", err)
		}

		// Verify in vault.yaml before close
		vaultPath := filepath.Join(dir, "vault.yaml")
		vaultBytes, err := os.ReadFile(vaultPath)
		if err != nil {
			t.Fatalf("read vault.yaml: %v", err)
		}
		if !strings.Contains(string(vaultBytes), "Goal/goal-one") {
			t.Fatalf("expected Goal/goal-one in vault.yaml before close, got:\n%s", vaultBytes)
		}

		v.Close()
	}

	// Second instance: reopen and verify goal-one is still there
	{
		v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { v.Close() })

		ctx := context.Background()
		cur, found, err := v.GetCurrent(ctx, "Goal", "goal-one")
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatal("expected goal-one to be present after restart (loaded from vault.yaml)")
		}
		if cur.ID != "goal-one" {
			t.Fatalf("expected goal-one, got %s", cur.ID)
		}
	}
}

// TestTransactionalRenameNotLost verifies that a goal created inline, renamed,
// and then a working save elsewhere doesn't cause the goal to disappear (the exact defect scenario).
func TestTransactionalRenameNotLost(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	ctx := context.Background()

	// Step 1: Create a goal via transactional commit (inline add)
	yaml1 := []byte("metadata:\n  id: goal-new\n  name: New Goal\n")
	err = v.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		return tx.PutVersion(ctx, store.Version{
			Kind:   "Goal",
			ID:     "goal-new",
			Number: 1,
			YAML:   yaml1,
			Actor:  "test",
			Reason: "inline create",
			On:     time.Now().UTC(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Step 2: Verify goal is in vault.yaml and cache
	vaultBytes, err := os.ReadFile(filepath.Join(dir, "vault.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(vaultBytes), "Goal/goal-new") {
		t.Fatalf("step 2: expected Goal/goal-new in vault.yaml, got:\n%s", vaultBytes)
	}

	// Step 3: Rename the goal (update via transactional commit)
	yaml2 := []byte("metadata:\n  id: goal-new\n  name: Renamed Goal\n")
	err = v.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		return tx.PutVersion(ctx, store.Version{
			Kind:   "Goal",
			ID:     "goal-new",
			Number: 2,
			YAML:   yaml2,
			Actor:  "test",
			Reason: "renamed",
			On:     time.Now().UTC(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Step 4: Do a working save on another goal (triggers rehydration via watcher if enabled)
	yaml3 := []byte("metadata:\n  id: other-goal\n  name: Other Goal\n")
	err = v.PutWorking(ctx, "Goal", "other-goal", yaml3)
	if err != nil {
		t.Fatal(err)
	}

	// Step 5: Verify goal-new is STILL in cache (the defect was that it would disappear here)
	cur, found, err := v.GetCurrent(ctx, "Goal", "goal-new")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("step 5: expected goal-new to still be present after working save elsewhere (DEFECT: it disappeared)")
	}
	if !strings.Contains(string(cur.YAML), "Renamed Goal") {
		t.Fatalf("step 5: expected renamed goal content, got:\n%s", cur.YAML)
	}

	// Step 6: Verify goal-new is still in vault.yaml
	vaultBytes, err = os.ReadFile(filepath.Join(dir, "vault.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(vaultBytes), "Goal/goal-new") {
		t.Fatalf("step 6: expected Goal/goal-new in vault.yaml after rename+working-save, got:\n%s", vaultBytes)
	}
}

// TestApplyJournalReplayBasic tests that unapplied units are replayed on open.
func TestApplyJournalReplayBasic(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// A save is what goes through applyUnit and so through the journal.
	yaml1 := []byte("metadata:\n  id: goal-one\n  name: Goal One\n")
	err = v.PutWorking(ctx, "Goal", "goal-one", yaml1)
	if err != nil {
		t.Fatal(err)
	}

	// Verify it's in the vault
	cur, found, err := v.GetCurrent(ctx, "Goal", "goal-one")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected goal-one after PutWorking")
	}

	// Close the vault
	v.Close()

	// Reopen the vault - it should replay any unapplied units before rehydration
	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	defer v2.Close()

	// Verify the manifest is still there after replay
	cur2, found, err := v2.GetCurrent(ctx, "Goal", "goal-one")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected goal-one after reopen (replay failed)")
	}
	if string(cur2.YAML) != string(cur.YAML) {
		t.Fatalf("content mismatch after replay: expected %s, got %s", cur.YAML, cur2.YAML)
	}
}

// TestApplyJournalReplayIdempotent tests that replay skips files with matching sha256.
func TestApplyJournalReplayIdempotent(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Write a manifest
	yaml1 := []byte("metadata:\n  id: goal-one\n  name: Goal One\n")
	// The journal is the save path: autosave stages a draft and never
	// reaches applyUnit, so this drives a save.
	err = v.PutVersion(ctx, store.Version{Kind: "Goal", ID: "goal-one", Number: 1, YAML: yaml1, Actor: "local", Reason: "save"})
	if err != nil {
		t.Fatal(err)
	}

	// Record the mtime of the file
	filePath := filepath.Join(dir, "Goal", "goal-one.yaml")
	fi1, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	mtime1 := fi1.ModTime()

	// Close and reopen the vault
	v.Close()
	time.Sleep(10 * time.Millisecond) // Ensure time passes

	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	defer v2.Close()

	// Verify the manifest is still there
	_, found, err := v2.GetCurrent(ctx, "Goal", "goal-one")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected goal-one after reopen")
	}

	// Verify the file mtime is unchanged (replay skipped it because sha256 matched)
	fi2, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if fi2.ModTime() != mtime1 {
		t.Fatalf("file mtime changed after replay: expected %v, got %v (file should not have been rewritten)", mtime1, fi2.ModTime())
	}
}

// TestApplyJournalReplayWithMultipleUnits tests replay of multiple units.
func TestApplyJournalReplayWithMultipleUnits(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Write multiple manifests
	yaml1 := []byte("metadata:\n  id: goal-one\n  name: Goal One\n")
	yaml2 := []byte("metadata:\n  id: goal-two\n  name: Goal Two\n")
	yaml3 := []byte("metadata:\n  id: team-one\n  name: Team One\n")

	err = v.PutWorking(ctx, "Goal", "goal-one", yaml1)
	if err != nil {
		t.Fatal(err)
	}
	err = v.PutWorking(ctx, "Goal", "goal-two", yaml2)
	if err != nil {
		t.Fatal(err)
	}
	err = v.PutWorking(ctx, "Team", "team-one", yaml3)
	if err != nil {
		t.Fatal(err)
	}

	// Close and reopen
	v.Close()

	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	defer v2.Close()

	// Verify all manifests are present
	testCases := []struct {
		kind string
		id   string
		yaml []byte
	}{
		{"Goal", "goal-one", yaml1},
		{"Goal", "goal-two", yaml2},
		{"Team", "team-one", yaml3},
	}
	for _, tc := range testCases {
		cur, found, err := v2.GetCurrent(ctx, tc.kind, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatalf("expected %s/%s after reopen", tc.kind, tc.id)
		}
		if string(cur.YAML) != string(tc.yaml) {
			t.Fatalf("%s/%s content mismatch: expected %s, got %s", tc.kind, tc.id, tc.yaml, cur.YAML)
		}
	}
}

// TestApplyJournalReplayConvergence tests that replay converges files and index after a crash.
func TestApplyJournalReplayConvergence(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Create a version via transactional commit
	yaml1 := []byte("metadata:\n  id: goal-one\n  name: Goal One\nspec:\n  level: goal\n")
	err = v.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		return tx.PutVersion(ctx, store.Version{
			Kind:   "Goal",
			ID:     "goal-one",
			Number: 1,
			YAML:   yaml1,
			Actor:  "test",
			Reason: "created",
			On:     time.Now().UTC(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Close and reopen
	v.Close()

	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	defer v2.Close()

	// Verify the version is present and correct
	cur, found, err := v2.GetCurrent(ctx, "Goal", "goal-one")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected goal-one after replay and rehydration")
	}
	if string(cur.YAML) != string(yaml1) {
		t.Fatalf("content mismatch: expected %s, got %s", yaml1, cur.YAML)
	}

	// Verify it's in vault.yaml
	vaultBytes, err := os.ReadFile(filepath.Join(dir, "vault.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(vaultBytes), "Goal/goal-one") {
		t.Fatalf("expected Goal/goal-one in vault.yaml, got:\n%s", vaultBytes)
	}

	// Verify the version log has the version
	versions, err := v2.ListVersions(ctx, "Goal", "goal-one")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) == 0 {
		t.Fatal("expected version log entry after replay")
	}
	if versions[0].Number != 1 {
		t.Fatalf("expected version number 1, got %d", versions[0].Number)
	}
}

// TestApplyJournalExcludeAndRecover tests that exclude and recover use applyUnit.
func TestApplyJournalExcludeAndRecover(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Create a manifest
	yaml1 := []byte("metadata:\n  id: goal-one\n  name: Goal One\nspec:\n  level: goal\n")
	err = v.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		return tx.PutVersion(ctx, store.Version{
			Kind:   "Goal",
			ID:     "goal-one",
			Number: 1,
			YAML:   yaml1,
			Actor:  "test",
			Reason: "created",
			On:     time.Now().UTC(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Exclude it
	err = v.Exclude(ctx, "Goal", "goal-one", "Goal One", "testing exclusion", "test")
	if err != nil {
		t.Fatal(err)
	}

	// Verify it's excluded
	exclusions, err := v.ListExcluded(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(exclusions) != 1 || exclusions[0].ID != "goal-one" {
		t.Fatal("expected exclusion record after Exclude")
	}

	// Verify file still exists (Exclude is not a file delete)
	_, err = os.Stat(filepath.Join(dir, "Goal", "goal-one.yaml"))
	if err != nil {
		t.Fatal("expected file to still exist after Exclude")
	}

	// Close and reopen
	v.Close()

	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	defer v2.Close()

	// Verify exclusion persists
	exclusions2, err := v2.ListExcluded(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(exclusions2) != 1 || exclusions2[0].ID != "goal-one" {
		t.Fatal("expected exclusion to persist after reopen")
	}

	// Verify file is not in current state
	_, found, err := v2.GetCurrent(ctx, "Goal", "goal-one")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("excluded goal should not be in current state")
	}

	// Recover it
	err = v2.Recover(ctx, "Goal", "goal-one", "testing recovery", "test")
	if err != nil {
		t.Fatal(err)
	}

	// Verify it's back in the current state
	cur, found, err := v2.GetCurrent(ctx, "Goal", "goal-one")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected goal-one after recovery")
	}
	if string(cur.YAML) != string(yaml1) {
		t.Fatalf("content mismatch after recovery: expected %s, got %s", yaml1, cur.YAML)
	}

	// Verify exclusion is removed
	exclusions3, err := v2.ListExcluded(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(exclusions3) != 0 {
		t.Fatal("expected exclusion to be removed after recovery")
	}
}

// TestApplyJournalFailureAfterJournal tests convergence when failure occurs after journal commit.
func TestApplyJournalFailureAfterJournal(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false, FailurePoint: vault.FailurePointNone})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// A save - should fail after journal commit
	v.SetFailurePoint(vault.FailurePointAfterJournal)
	yaml1 := []byte("metadata:\n  id: goal-fail-one\n  name: Goal Fail One\n")
	// The journal is the save path: autosave stages a draft and never
	// reaches applyUnit, so this drives a save.
	err = v.PutVersion(ctx, store.Version{Kind: "Goal", ID: "goal-fail-one", Number: 1, YAML: yaml1, Actor: "local", Reason: "save"})
	if err == nil {
		t.Fatal("expected failure after journal commit")
	}

	// File should not exist yet (write failed before files written)
	_, err = os.Stat(filepath.Join(dir, "Goal", "goal-fail-one.yaml"))
	if err == nil {
		t.Fatal("expected file to NOT exist after failure")
	}

	// Close and reopen without failure injection
	v.Close()

	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false, FailurePoint: vault.FailurePointNone})
	if err != nil {
		t.Fatal(err)
	}
	defer v2.Close()

	// Verify file exists after replay (unit was in journal, replay wrote files)
	_, err = os.Stat(filepath.Join(dir, "Goal", "goal-fail-one.yaml"))
	if err != nil {
		t.Fatalf("expected file to exist after replay: %v", err)
	}

	// Verify manifest is in current state
	cur, found, err := v2.GetCurrent(ctx, "Goal", "goal-fail-one")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected goal-fail-one after replay")
	}
	if string(cur.YAML) != string(yaml1) {
		t.Fatalf("content mismatch after replay: expected %s, got %s", yaml1, cur.YAML)
	}
}

// TestApplyJournalFailureAfterFirstFile tests convergence when failure occurs after first file.
func TestApplyJournalFailureAfterFirstFile(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false, FailurePoint: vault.FailurePointNone})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Write manifest via transactional commit to have multiple files
	v.SetFailurePoint(vault.FailurePointAfterFirstFile)
	yaml1 := []byte("metadata:\n  id: goal-fail-two\n  name: Goal Fail Two\nspec:\n  level: goal\n")
	err = v.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		return tx.PutVersion(ctx, store.Version{
			Kind:   "Goal",
			ID:     "goal-fail-two",
			Number: 1,
			YAML:   yaml1,
			Actor:  "test",
			Reason: "created",
			On:     time.Now().UTC(),
		})
	})
	if err == nil {
		t.Fatal("expected failure after first file")
	}

	// Manifest file may exist, but vault.yaml should not have the entry yet
	vaultPath := filepath.Join(dir, "vault.yaml")
	vaultBytes, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(vaultBytes), "Goal/goal-fail-two") {
		t.Fatal("expected Goal/goal-fail-two to NOT be in vault.yaml after failure")
	}

	// Close and reopen
	v.Close()

	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false, FailurePoint: vault.FailurePointNone})
	if err != nil {
		t.Fatal(err)
	}
	defer v2.Close()

	// Verify manifest is in current state after replay
	_, found, err := v2.GetCurrent(ctx, "Goal", "goal-fail-two")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected goal-fail-two after replay")
	}

	// Verify it's in vault.yaml
	vaultBytes, err = os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(vaultBytes), "Goal/goal-fail-two") {
		t.Fatalf("expected Goal/goal-fail-two in vault.yaml after replay, got:\n%s", vaultBytes)
	}
}

// TestApplyJournalFailureAfterLastFile tests convergence when failure occurs after vault.yaml write.
func TestApplyJournalFailureAfterLastFile(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false, FailurePoint: vault.FailurePointNone})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Write manifest - will fail after vault.yaml write but before mark applied
	v.SetFailurePoint(vault.FailurePointAfterLastFile)
	yaml1 := []byte("metadata:\n  id: goal-fail-three\n  name: Goal Fail Three\n")
	// The journal is the save path: autosave stages a draft and never
	// reaches applyUnit, so this drives a save.
	err = v.PutVersion(ctx, store.Version{Kind: "Goal", ID: "goal-fail-three", Number: 1, YAML: yaml1, Actor: "local", Reason: "save"})
	if err == nil {
		t.Fatal("expected failure after last file")
	}

	// File should exist now (written before mark applied failure)
	_, err = os.Stat(filepath.Join(dir, "Goal", "goal-fail-three.yaml"))
	if err != nil {
		t.Fatalf("expected file to exist after failure: %v", err)
	}

	// Unit is in journal but not marked applied
	// Close and reopen
	v.Close()

	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false, FailurePoint: vault.FailurePointNone})
	if err != nil {
		t.Fatal(err)
	}
	defer v2.Close()

	// Verify manifest is in current state after replay
	cur, found, err := v2.GetCurrent(ctx, "Goal", "goal-fail-three")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected goal-fail-three after replay")
	}
	if string(cur.YAML) != string(yaml1) {
		t.Fatalf("content mismatch: expected %s, got %s", yaml1, cur.YAML)
	}
}

// TestApplyJournalReplayIdempotentMtime tests that replayed files with matching sha256 keep mtime.
func TestApplyJournalReplayIdempotentMtime(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false, FailurePoint: vault.FailurePointNone})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Write manifest normally
	yaml1 := []byte("metadata:\n  id: goal-mtime\n  name: Goal Mtime\n")
	// The journal is the save path: autosave stages a draft and never
	// reaches applyUnit, so this drives a save.
	err = v.PutVersion(ctx, store.Version{Kind: "Goal", ID: "goal-mtime", Number: 1, YAML: yaml1, Actor: "local", Reason: "save"})
	if err != nil {
		t.Fatal(err)
	}

	// Get mtime of the file
	filePath := filepath.Join(dir, "Goal", "goal-mtime.yaml")
	fi1, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	mtime1 := fi1.ModTime()

	// Close and reopen - file sha256 matches, so replay should skip it
	v.Close()
	time.Sleep(20 * time.Millisecond) // Ensure time passes

	v2, err := vault.New(context.Background(), dir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false, FailurePoint: vault.FailurePointNone})
	if err != nil {
		t.Fatal(err)
	}
	defer v2.Close()

	// Verify manifest is present
	_, found, err := v2.GetCurrent(ctx, "Goal", "goal-mtime")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected goal-mtime after replay")
	}

	// Verify file mtime is unchanged
	fi2, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if fi2.ModTime() != mtime1 {
		t.Fatalf("file mtime changed after replay: expected %v, got %v", mtime1, fi2.ModTime())
	}
}
