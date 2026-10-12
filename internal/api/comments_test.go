package api_test

import (
	"net/http"
	"testing"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
)

// A comment is kept beside the part of a record it is about and read
// back with the record's others; a thread is resolved from its first
// comment (TAXONOMY.md D72).
func TestACommentIsKeptAndResolved(t *testing.T) {
	t.Parallel()
	_, url := newTestServer(t)
	resp := doJSON(t, "POST", url+"/manifests/Project/depot-checks/comments", map[string]any{"anchor": "section:scope", "body": "Is the pilot depot in scope?"}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add: %d", resp.StatusCode)
	}
	first := decode[apigen.Comment](t, resp)
	resp = doJSON(t, "POST", url+"/manifests/Project/depot-checks/comments", map[string]any{"reply": first.Id, "body": "Yes."}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reply: %d", resp.StatusCode)
	}
	resp = doJSON(t, "PATCH", url+"/comments/"+first.Id, map[string]any{"resolved": true}, nil)
	if resp.StatusCode != http.StatusOK || !decode[apigen.Comment](t, resp).Resolved {
		t.Fatalf("resolve: %d", resp.StatusCode)
	}
	list := decode[[]apigen.Comment](t, doJSON(t, "GET", url+"/manifests/Project/depot-checks/comments", nil, nil))
	if len(list) != 2 || list[0].Anchor != "section:scope" || list[1].Reply == nil || *list[1].Reply != first.Id || !list[0].Resolved {
		t.Fatalf("the record's comments: %+v", list)
	}
	resp = doJSON(t, "POST", url+"/manifests/Project/depot-checks/comments", map[string]any{"body": "Hi", "mentions": []map[string]string{{"kind": "team", "id": "no-team"}}}, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("asking a team that is not there: %d", resp.StatusCode)
	}
	if resp := doJSON(t, "GET", url+"/mentionables?q=x", nil, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("mentionables: %d", resp.StatusCode)
	}
}
