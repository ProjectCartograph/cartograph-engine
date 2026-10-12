package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// A comment sits beside the part of a record it is about, asks people on
// the access list or teams by name, and is found again by whom it asks
// (TAXONOMY.md D72). Nothing about the record changes.
func TestACommentAsksAPersonOrATeam(t *testing.T) {
	t.Parallel()
	e := accessEngine(t)
	if _, err := e.GrantPerson(context.Background(), "ada@example.org", nil, []string{"assessment"}, false, "admin"); err != nil {
		t.Fatal(err)
	}
	ben := as("ben@example.org")
	first, err := e.AddComment(ben, "Project", "depot-checks", engine.CommentDraft{
		Anchor: "section:scope", Body: "  Is the pilot depot in scope?  ",
		Mentions: []store.Mention{{Kind: store.MentionPerson, ID: "Ada@Example.org"}, {Kind: store.MentionTeam, ID: "curriculum"}, {Kind: store.MentionTeam, ID: "curriculum"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Body != "Is the pilot depot in scope?" || first.Author != "ben@example.org" || len(first.Mentions) != 2 || first.Mentions[0].ID != "ada@example.org" {
		t.Fatalf("the comment kept: %+v", first)
	}

	// A reply joins the thread where it sits, whatever anchor it names.
	reply, err := e.AddComment(as("ada@example.org"), "Project", "depot-checks", engine.CommentDraft{Reply: first.ID, Anchor: "/spec/elsewhere", Body: "Yes, the first depot."})
	if err != nil || reply.Anchor != "section:scope" || reply.Reply != first.ID {
		t.Fatalf("a reply: %+v, %v", reply, err)
	}
	again, err := e.AddComment(ben, "Project", "depot-checks", engine.CommentDraft{Reply: reply.ID, Body: "Thank you."})
	if err != nil || again.Reply != first.ID {
		t.Fatalf("a reply to a reply joins the thread: %+v, %v", again, err)
	}

	// Who is asked finds it: Ada by her address, and through her team.
	if _, err := e.AddComment(ben, "Project", "depot-checks", engine.CommentDraft{Body: "Assessment, please read the targets.", Mentions: []store.Mention{{Kind: store.MentionTeam, ID: "assessment"}}}); err != nil {
		t.Fatal(err)
	}
	mine, err := e.MyMentions(as("ada@example.org"), 0)
	if err != nil || len(mine) != 2 || mine[0].Body != "Assessment, please read the targets." || mine[1].ID != first.ID {
		t.Fatalf("Ada's mentions, newest first: %+v, %v", mine, err)
	}

	// Only who exists can be asked.
	_, err = e.AddComment(ben, "Project", "depot-checks", engine.CommentDraft{Body: "Hello", Mentions: []store.Mention{{Kind: store.MentionPerson, ID: "nobody@example.org"}, {Kind: store.MentionTeam, ID: "no-team"}}})
	var invalid *engine.ValidationError
	if !errors.As(err, &invalid) || len(invalid.Problems) != 2 {
		t.Fatalf("asking who is not there: %v", err)
	}
	if _, err := e.AddComment(ben, "Project", "depot-checks", engine.CommentDraft{Body: "   "}); !errors.As(err, &invalid) {
		t.Fatalf("an empty comment: %v", err)
	}

	// A thread is closed from the comment that starts it.
	closed, err := e.ResolveComment(ben, first.ID, true)
	if err != nil || !closed.Resolved || closed.ResolvedBy != "ben@example.org" {
		t.Fatalf("resolved: %+v, %v", closed, err)
	}
	if _, err := e.ResolveComment(ben, reply.ID, true); !errors.As(err, &invalid) {
		t.Fatalf("a reply resolved: %v", err)
	}
	list, err := e.Comments(ben, "Project", "depot-checks")
	if err != nil || len(list) != 4 || !list[0].Resolved {
		t.Fatalf("the record's comments: %+v, %v", list, err)
	}
}

// Who a comment can ask: the people on the access list and the teams,
// by what is typed.
func TestMentionablesAreThePeopleAndTheTeams(t *testing.T) {
	t.Parallel()
	e := accessEngine(t)
	if _, err := e.GrantPerson(context.Background(), "ada@example.org", nil, nil, false, "admin"); err != nil {
		t.Fatal(err)
	}
	all, err := e.Mentionables(context.Background(), "", 0)
	if err != nil || len(all) < 2 || all[0].Kind != store.MentionPerson || all[0].ID != "ada@example.org" {
		t.Fatalf("everyone: %+v, %v", all, err)
	}
	some, err := e.Mentionables(context.Background(), "assess", 0)
	if err != nil || len(some) != 1 || some[0].Kind != store.MentionTeam || some[0].ID != "assessment" {
		t.Fatalf("by what is typed: %+v, %v", some, err)
	}
}

// Without an access list, a comment asks teams.
func TestWithoutAnAccessListACommentAsksTeams(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	_, err := e.AddComment(context.Background(), "Project", "p1", engine.CommentDraft{Body: "Hello", Mentions: []store.Mention{{Kind: store.MentionPerson, ID: "ada@example.org"}}})
	var invalid *engine.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("a person asked with no access list: %v", err)
	}
}
