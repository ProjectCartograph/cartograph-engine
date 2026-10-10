package api_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
)

// A screen reads the workspace as if a change set were accepted by
// naming it: the change set's drafts stand in for the records they
// change, the records it creates are there too, and each is marked
// (docs/adr/0024). Without it, the record reads as it is.
func TestTheWorkspaceReadsAsIfAChangeSetWereAccepted(t *testing.T) {
	t.Parallel()
	_, base := newTestServer(t)
	commitTeam(t, base, "t1", "anyone")
	resp := doJSON(t, http.MethodPost, base+"/changesets", map[string]string{"title": "Two teams"}, nil)
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("start a change set: %d %s", resp.StatusCode, b)
	}
	set := decode[apigen.ChangeSet](t, resp).Id
	put := func(id, name string) {
		yaml := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: " + id + "\n  name: " + name + "\nspec:\n  description: Team\n"
		resp := doJSON(t, http.MethodPut, base+"/changesets/"+set+"/items/Team/"+id, apigen.ChangeSetEdit{Yaml: yaml}, nil)
		if resp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("draft %s: %d %s", id, resp.StatusCode, b)
		}
		resp.Body.Close()
	}
	put("t1", "Grading team")
	put("t9", "Intake team")

	resp = doJSON(t, http.MethodGet, base+"/manifests/Team/t9?changeSet="+set, nil, nil)
	view := decode[apigen.ManifestView](t, resp)
	if view.Proposed == nil || *view.Proposed != apigen.ProposedNew || !strings.Contains(view.Yaml, "Intake team") {
		t.Fatalf("a record the change set creates: %+v", view)
	}
	if resp := doJSON(t, http.MethodGet, base+"/manifests/Team/t9", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a proposed record read without the change set: %d", resp.StatusCode)
	}
	resp = doJSON(t, http.MethodGet, base+"/manifests/Team?changeSet="+set, nil, nil)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	list := string(b)
	if !strings.Contains(list, `"name":"Grading team"`) || !strings.Contains(list, `"proposed":"changed"`) || !strings.Contains(list, `"proposed":"new"`) {
		t.Fatalf("the list as proposed: %s", list)
	}
	// With each record's spec too: the drafts' specs, not the saved ones.
	resp = doJSON(t, http.MethodGet, base+"/manifests/Team?expand=spec&changeSet="+set, nil, nil)
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Count(string(b), `"description":"Team"`) < 2 {
		t.Fatalf("the list with specs as proposed: %s", b)
	}
	goal := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g-sound\n  name: Sound fruit\nspec:\n  level: goal\n  objective: Deliver sound fruit to every buyer\n"
	resp = doJSON(t, http.MethodPut, base+"/changesets/"+set+"/items/Goal/g-sound", apigen.ChangeSetEdit{Yaml: goal}, nil)
	resp.Body.Close()
	resp = doJSON(t, http.MethodGet, base+"/goals/tree?changeSet="+set, nil, nil)
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), `"id":"g-sound"`) || !strings.Contains(string(b), `"proposed":"new"`) {
		t.Fatalf("the strategy as proposed: %s", b)
	}
	if resp := doJSON(t, http.MethodGet, base+"/manifests/Goal/g-sound/checks?changeSet="+set, nil, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("a proposed goal's checks: %d", resp.StatusCode)
	}
	if resp := doJSON(t, http.MethodGet, base+"/manifests/Goal/g-sound/checks", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a proposed goal's checks read without the change set: %d", resp.StatusCode)
	}
	resp = doJSON(t, http.MethodGet, base+"/goals/tree", nil, nil)
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(b), "g-sound") {
		t.Fatalf("the strategy as it is shows a proposed goal: %s", b)
	}
	if resp := doJSON(t, http.MethodGet, base+"/manifests/Team?changeSet=nope", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("an unknown change set: %d", resp.StatusCode)
	}
}

// A change set can delete a record as well as save one: the review says
// so, and rolling it in deletes it.
func TestAChangeSetDeletesOverHTTP(t *testing.T) {
	t.Parallel()
	_, base := newTestServer(t)
	commitTeam(t, base, "t5", "anyone")
	resp := doJSON(t, http.MethodPost, base+"/changesets", map[string]string{"title": "Tidy"}, nil)
	set := decode[apigen.ChangeSet](t, resp).Id
	if resp := doJSON(t, http.MethodPut, base+"/changesets/"+set+"/items/Team/t5/removal", nil, nil); resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("mark the delete: %d %s", resp.StatusCode, b)
	}
	resp = doJSON(t, http.MethodGet, base+"/changesets/"+set, nil, nil)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), `"op":"delete"`) {
		t.Fatalf("the review: %s", b)
	}
	if resp := doJSON(t, http.MethodPut, base+"/changesets/"+set+"/items/Team/none/removal", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleting what is not there: %d", resp.StatusCode)
	}
}

// A change set's charter reads against the record: a project new in the
// change set is added whole, its lines added (engine issue 30).
func TestAChangeSetsCharterReadsAgainstTheRecord(t *testing.T) {
	t.Parallel()
	_, base := newTestServer(t)
	commitTeam(t, base, "t1", "anyone")
	resp := doJSON(t, http.MethodPost, base+"/changesets", map[string]string{"title": "A project"}, nil)
	set := decode[apigen.ChangeSet](t, resp).Id
	yaml := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: p1\n  name: Depot checks\nspec:\n  team: t1\n  summary:\n    problems:\n" +
		"      - id: pr-1\n        problem: {situation: depots grade produce differently}\n"
	if resp := doJSON(t, http.MethodPut, base+"/changesets/"+set+"/items/Project/p1", apigen.ChangeSetEdit{Yaml: yaml}, nil); resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("draft: %d %s", resp.StatusCode, b)
	}
	resp = doJSON(t, http.MethodGet, base+"/changesets/"+set+"/items/Project/p1/charter", nil, nil)
	parts := decode[[]apigen.CharterPartDiff](t, resp)
	found := false
	for _, p := range parts {
		if p.State != apigen.CharterPartDiffStateAdded {
			t.Fatalf("a part of a new project is not added: %+v", p)
		}
		for _, l := range p.Lines {
			found = found || (l.Op == apigen.CharterPartDiffLinesOpAdded && l.Text == "Lead team")
		}
	}
	if !found {
		t.Fatalf("the lead team is not added in the diff: %+v", parts)
	}
}
