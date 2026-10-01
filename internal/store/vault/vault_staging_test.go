package vault_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/vault"
)

func stagedPath(dir, kind, id string) string {
	return filepath.Join(dir, ".cartograph", "staging", kind, id+".yaml")
}

func openVault(t *testing.T, dir string) *vault.ManifestStore {
	t.Helper()
	v, err := vault.New(context.Background(), dir, vault.Options{Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	return v
}

// A draft goes to staging and nowhere else. This is the whole point: a
// half-answered definition is not part of the vault, and a hand-maintained
// file is not reformatted under its author by somebody opening a wizard.
func TestStagingKeepsTheVaultTreeUntouched(t *testing.T) {
	dir := t.TempDir()
	v := openVault(t, dir)
	ctx := context.Background()

	saved := []byte("metadata:\n  id: g1\n  name: Written by hand\n")
	if err := v.PutVersion(ctx, store.Version{
		Kind: "Goal", ID: "g1", Number: 1, YAML: saved, Actor: "local", Reason: "save",
	}); err != nil {
		t.Fatal(err)
	}

	draft := []byte("metadata:\n  id: g1\n  name: Half an edit\n")
	if err := v.PutWorking(ctx, "Goal", "g1", draft); err != nil {
		t.Fatal(err)
	}

	// The file on disk still says what its author wrote.
	onDisk, err := os.ReadFile(filepath.Join(dir, "Goal", "g1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(saved) {
		t.Fatalf("a draft overwrote the saved file: %q", onDisk)
	}
	// And the draft is in staging.
	stagedBytes, err := os.ReadFile(stagedPath(dir, "Goal", "g1"))
	if err != nil {
		t.Fatalf("no draft in staging: %v", err)
	}
	if string(stagedBytes) != string(draft) {
		t.Fatalf("expected the draft in staging, got %q", stagedBytes)
	}
	// GetWorking answers with the draft, because that is what somebody is
	// working on.
	got, found, err := v.GetWorking(ctx, "Goal", "g1")
	if err != nil || !found {
		t.Fatalf("expected a working copy: %v found=%v", err, found)
	}
	if string(got) != string(draft) {
		t.Fatalf("expected the draft back, got %q", got)
	}
}

// Saving promotes the draft and leaves nothing behind.
func TestSavePromotesAndClearsTheDraft(t *testing.T) {
	dir := t.TempDir()
	v := openVault(t, dir)
	ctx := context.Background()

	draft := []byte("metadata:\n  id: g2\n  name: New thing\n")
	if err := v.PutWorking(ctx, "Goal", "g2", draft); err != nil {
		t.Fatal(err)
	}
	// Nothing in the vault tree yet: this definition has never been saved.
	if _, err := os.Stat(filepath.Join(dir, "Goal", "g2.yaml")); !os.IsNotExist(err) {
		t.Fatalf("a draft created a vault file: %v", err)
	}

	if err := v.PutVersion(ctx, store.Version{
		Kind: "Goal", ID: "g2", Number: 1, YAML: draft, Actor: "local", Reason: "save",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Goal", "g2.yaml")); err != nil {
		t.Fatalf("a save did not write the vault file: %v", err)
	}
	if _, err := os.Stat(stagedPath(dir, "Goal", "g2")); !os.IsNotExist(err) {
		t.Fatalf("the draft outlived its promotion: %v", err)
	}
	// The file and the include list landed together.
	vaultYAML, err := os.ReadFile(filepath.Join(dir, "vault.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(vaultYAML), "Goal/g2") {
		t.Fatalf("a saved manifest is not included:\n%s", vaultYAML)
	}
	// Staging is left tidy rather than full of empty directories.
	if _, err := os.Stat(filepath.Join(dir, ".cartograph", "staging", "Goal")); !os.IsNotExist(err) {
		t.Fatalf("an empty staging kind directory was left behind: %v", err)
	}
}

// Unfinished work must survive a restart: a draft is a file, not memory.
func TestDraftSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	draft := []byte("metadata:\n  id: g3\n  name: Unfinished\n")
	{
		v, err := vault.New(ctx, dir, vault.Options{Watch: false})
		if err != nil {
			t.Fatal(err)
		}
		if err := v.PutWorking(ctx, "Goal", "g3", draft); err != nil {
			t.Fatal(err)
		}
		v.Close()
	}
	v2 := openVault(t, dir)
	cur, found, err := v2.GetCurrent(ctx, "Goal", "g3")
	if err != nil || !found {
		t.Fatalf("a draft did not survive a restart: %v found=%v", err, found)
	}
	if string(cur.YAML) != string(draft) {
		t.Fatalf("expected the draft, got %q", cur.YAML)
	}
	// Still not included: reopening decides nothing.
	vaultYAML, _ := os.ReadFile(filepath.Join(dir, "vault.yaml"))
	if strings.Contains(string(vaultYAML), "Goal/g3") {
		t.Fatalf("a restart included a draft:\n%s", vaultYAML)
	}
}

// A draft wins over the file it is a draft of, after a restart as well as
// before one — otherwise reopening would quietly show the old answer.
func TestDraftWinsOverTheSavedFileAfterRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	saved := []byte("metadata:\n  id: g4\n  name: Saved\n")
	draft := []byte("metadata:\n  id: g4\n  name: Draft\n")
	{
		v, err := vault.New(ctx, dir, vault.Options{Watch: false})
		if err != nil {
			t.Fatal(err)
		}
		if err := v.PutVersion(ctx, store.Version{
			Kind: "Goal", ID: "g4", Number: 1, YAML: saved, Actor: "local", Reason: "save",
		}); err != nil {
			t.Fatal(err)
		}
		if err := v.PutWorking(ctx, "Goal", "g4", draft); err != nil {
			t.Fatal(err)
		}
		v.Close()
	}
	v2 := openVault(t, dir)
	cur, found, err := v2.GetCurrent(ctx, "Goal", "g4")
	if err != nil || !found {
		t.Fatal("expected g4 after restart")
	}
	if string(cur.YAML) != string(draft) {
		t.Fatalf("the saved file shadowed the draft: %q", cur.YAML)
	}
}

// Discard is the operation that could not exist before staging, because
// the working copy was the file and there was nothing to go back to.
func TestDiscardPutsBackWhatTheVaultHolds(t *testing.T) {
	dir := t.TempDir()
	v := openVault(t, dir)
	ctx := context.Background()

	saved := []byte("metadata:\n  id: g5\n  name: Saved\n")
	if err := v.PutVersion(ctx, store.Version{
		Kind: "Goal", ID: "g5", Number: 1, YAML: saved, Actor: "local", Reason: "save",
	}); err != nil {
		t.Fatal(err)
	}
	if err := v.PutWorking(ctx, "Goal", "g5", []byte("metadata:\n  id: g5\n  name: Regretted\n")); err != nil {
		t.Fatal(err)
	}
	if err := v.DiscardWorking(ctx, "Goal", "g5"); err != nil {
		t.Fatal(err)
	}
	cur, found, err := v.GetCurrent(ctx, "Goal", "g5")
	if err != nil || !found {
		t.Fatalf("discard lost a saved manifest: %v found=%v", err, found)
	}
	if string(cur.YAML) != string(saved) {
		t.Fatalf("expected the saved manifest back, got %q", cur.YAML)
	}
	if _, err := os.Stat(stagedPath(dir, "Goal", "g5")); !os.IsNotExist(err) {
		t.Fatalf("discard left the draft in staging: %v", err)
	}
}

// Discarding something never saved leaves nothing, which is the honest
// answer rather than an empty manifest.
func TestDiscardingSomethingNeverSavedLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	v := openVault(t, dir)
	ctx := context.Background()

	if err := v.PutWorking(ctx, "Goal", "g6", []byte("metadata:\n  id: g6\n  name: Never saved\n")); err != nil {
		t.Fatal(err)
	}
	if err := v.DiscardWorking(ctx, "Goal", "g6"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := v.GetCurrent(ctx, "Goal", "g6"); found {
		t.Fatal("expected nothing after discarding a manifest that was never saved")
	}
}

// Many drafts at once, each its own: the save applies one, not the set.
func TestManyDraftsAndOneSave(t *testing.T) {
	dir := t.TempDir()
	v := openVault(t, dir)
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		if err := v.PutWorking(ctx, "Goal", id, []byte("metadata:\n  id: "+id+"\n")); err != nil {
			t.Fatal(err)
		}
	}
	staged, err := v.ListStaged(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 3 {
		t.Fatalf("expected three drafts, got %+v", staged)
	}

	if err := v.PutVersion(ctx, store.Version{
		Kind: "Goal", ID: "b", Number: 1, YAML: []byte("metadata:\n  id: b\n"), Actor: "local", Reason: "save",
	}); err != nil {
		t.Fatal(err)
	}
	staged, err = v.ListStaged(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 2 || staged[0].ID != "a" || staged[1].ID != "c" {
		t.Fatalf("a save promoted more than its own draft: %+v", staged)
	}
	// And the other two are still not in the vault.
	vaultYAML, _ := os.ReadFile(filepath.Join(dir, "vault.yaml"))
	for _, id := range []string{"a", "c"} {
		if strings.Contains(string(vaultYAML), "Goal/"+id) {
			t.Fatalf("saving one draft included another:\n%s", vaultYAML)
		}
	}
}
