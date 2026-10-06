package engine_test

import (
	"context"
	"testing"
)

// A governance body is a Resource named once and referenced: a mandate
// names it as its issuer, a criterion as who confirms it, a risk as whom
// it escalates to, and the body's references say which is which
// (TAXONOMY.md D43).
func TestAGovernanceBodyIsNamedOnceAndReferenced(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Resource", "steering", "p1", "apiVersion: cartograph/v1\nkind: Resource\nmetadata:\n  id: steering\n  name: Steering Committee\nspec:\n  category: governanceBody\n")
	body := "{kind: Resource, id: steering}"
	extra := "  mandate:\n    - {kind: decision, title: Approve the pilot, issuer: " + body + "}\n" +
		"  successCriteria:\n    - {id: sc-1, statement: Depots run the check unaided, metric: team, confirmedBy: " + body + ", when: atLanding}\n" +
		"  risks:\n    - {id: r1, description: Late forms, type: issue, escalate: {flag: true, reason: Delays reporting, to: " + body + "}}\n"
	mustCommit(t, e, "Project", "rollout", "p1", projectYAML("rollout", extra))

	refs, err := e.References(ctx, "Resource", "steering")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, u := range refs.Uses {
		got[u.As] = true
	}
	for _, as := range []string{"decided", "confirms", "receives"} {
		if !got[as] {
			t.Errorf("the body's uses lack %q: %+v", as, refs.Uses)
		}
	}

	both := projectYAML("rollout", "  mandate:\n    - {kind: decision, title: Approve the pilot, issuer: "+body+", issuedBy: Steering Committee}\n")
	if _, err := e.Commit(ctx, "Project", "rollout", []byte(both), "p1", "test"); !problemAt(err, "/spec/mandate/0/issuer") {
		t.Fatalf("an issuer named twice was accepted: %v", err)
	}
}
