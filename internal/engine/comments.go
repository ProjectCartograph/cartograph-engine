package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Comments are a conversation about a record, beside the part each is
// about (TAXONOMY.md D72). They are not the record: no version, no
// change set, no check. A comment may ask parties by name: a person on
// the access list, or a team. Who can be asked comes from the access
// list, which any directory fills through the deployment's mapping, and
// from the Team kind; nothing here knows how anyone signed in.

// ErrNoComments is a deployment whose store keeps no comments.
var ErrNoComments = fmt.Errorf("%w: this deployment keeps no comments", ErrNotFound)

// The longest a comment and its anchor may be.
const (
	maxCommentBody   = 4000
	maxCommentAnchor = 300
	maxMentions      = 20
)

var commentID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)

// CommentDraft is what a person writes: where, in answer to what, the
// words, and whom they ask.
type CommentDraft struct {
	Anchor   string
	Reply    string
	Body     string
	Mentions []store.Mention
}

// Mentionable is a party a comment can ask.
type Mentionable struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (e *Engine) commentStore() (store.CommentStore, error) {
	cs, ok := e.manifests.(store.CommentStore)
	if !ok {
		return nil, ErrNoComments
	}
	return cs, nil
}

// onRecord refuses a kind that is not one, or an id that cannot be one.
func onRecord(kind, id string) error {
	if _, ok := kinds.ByName(kind); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	if !commentID.MatchString(id) {
		return &ValidationError{Problems: []Problem{{Path: "id", Message: "Not a record's id."}}}
	}
	return nil
}

// Comments returns a record's comments, oldest first.
func (e *Engine) Comments(ctx context.Context, kind, id string) ([]store.Comment, error) {
	if err := onRecord(kind, id); err != nil {
		return nil, err
	}
	cs, err := e.commentStore()
	if err != nil {
		return nil, err
	}
	return cs.ListComments(ctx, kind, id)
}

// AddComment keeps a comment by the caller on a record. A reply answers a
// thread on the same record; a mention names a person on the access list
// or a team that exists. Nothing about the record is checked or changed.
func (e *Engine) AddComment(ctx context.Context, kind, id string, d CommentDraft) (store.Comment, error) {
	if err := onRecord(kind, id); err != nil {
		return store.Comment{}, err
	}
	cs, err := e.commentStore()
	if err != nil {
		return store.Comment{}, err
	}
	d.Body = strings.TrimSpace(d.Body)
	d.Anchor = strings.TrimSpace(d.Anchor)
	var problems []Problem
	switch {
	case d.Body == "":
		problems = append(problems, Problem{Path: "body", Message: "Write the comment."})
	case len([]rune(d.Body)) > maxCommentBody:
		problems = append(problems, Problem{Path: "body", Message: fmt.Sprintf("A comment is at most %d characters.", maxCommentBody)})
	}
	if len(d.Anchor) > maxCommentAnchor || (d.Anchor != "" && !strings.HasPrefix(d.Anchor, "/") && !strings.HasPrefix(d.Anchor, "section:")) {
		problems = append(problems, Problem{Path: "anchor", Message: "Comment on a section of the walk or a part of the record."})
	}
	if d.Reply != "" {
		parent, err := cs.GetComment(ctx, d.Reply)
		switch {
		case err != nil:
			problems = append(problems, Problem{Path: "reply", Message: "The comment answered is not there."})
		case parent.Kind != kind || parent.Manifest != id:
			problems = append(problems, Problem{Path: "reply", Message: "A reply is on the record of the comment it answers."})
		case parent.Reply != "":
			// One level: a reply to a reply joins the thread.
			d.Reply = parent.Reply
			d.Anchor = parent.Anchor
		default:
			d.Anchor = parent.Anchor
		}
	}
	mentions, mp := e.mentionsOf(ctx, d.Mentions)
	problems = append(problems, mp...)
	if len(problems) > 0 {
		return store.Comment{}, &ValidationError{Problems: problems}
	}
	c := store.Comment{
		ID: newCommentID(), Kind: kind, Manifest: id, Anchor: d.Anchor, Reply: d.Reply, Body: d.Body,
		Mentions: mentions, Author: personKey(identity.PrincipalFrom(ctx)), At: time.Now().UTC(),
	}
	if err := cs.PutComment(ctx, c); err != nil {
		return store.Comment{}, err
	}
	return c, nil
}

// mentionsOf keeps the parties a comment may ask, once each: a person on
// the access list, a team that exists.
func (e *Engine) mentionsOf(ctx context.Context, in []store.Mention) ([]store.Mention, []Problem) {
	var out []store.Mention
	var problems []Problem
	if len(in) > maxMentions {
		return nil, []Problem{{Path: "mentions", Message: fmt.Sprintf("A comment asks at most %d.", maxMentions)}}
	}
	for i, m := range in {
		at := fmt.Sprintf("mentions/%d", i)
		switch m.Kind {
		case store.MentionPerson:
			m.ID = strings.ToLower(strings.TrimSpace(m.ID))
			if e.access == nil {
				problems = append(problems, Problem{Path: at, Message: "No one is listed here to ask; ask a team."})
				continue
			}
			if _, err := e.access.GetPerson(ctx, m.ID); err != nil {
				problems = append(problems, Problem{Path: at, Message: "That person is not on the access list."})
				continue
			}
		case store.MentionTeam:
			if _, err := e.Get(ctx, "Team", m.ID); err != nil {
				problems = append(problems, Problem{Path: at, Message: "That team is not there."})
				continue
			}
		default:
			problems = append(problems, Problem{Path: at, Message: "Ask a person or a team."})
			continue
		}
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	return out, problems
}

// ResolveComment closes or reopens the thread a comment starts.
func (e *Engine) ResolveComment(ctx context.Context, id string, resolved bool) (store.Comment, error) {
	cs, err := e.commentStore()
	if err != nil {
		return store.Comment{}, err
	}
	c, err := cs.GetComment(ctx, id)
	if err != nil {
		return store.Comment{}, fmt.Errorf("%w: comment %s", ErrNotFound, id)
	}
	if c.Reply != "" {
		return store.Comment{}, &ValidationError{Problems: []Problem{{Path: "resolved", Message: "A thread is resolved from the comment that starts it."}}}
	}
	c.Resolved = resolved
	if resolved {
		c.ResolvedBy, c.ResolvedAt = personKey(identity.PrincipalFrom(ctx)), time.Now().UTC()
	} else {
		c.ResolvedBy, c.ResolvedAt = "", time.Time{}
	}
	if err := cs.PutComment(ctx, c); err != nil {
		return store.Comment{}, err
	}
	return c, nil
}

// Mentionables returns the parties a comment can ask whose name, address
// or id holds q: the people on the access list, then the teams, by name,
// at most limit.
func (e *Engine) Mentionables(ctx context.Context, q string, limit int) ([]Mentionable, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	has := func(parts ...string) bool {
		if q == "" {
			return true
		}
		for _, p := range parts {
			if strings.Contains(strings.ToLower(p), q) {
				return true
			}
		}
		return false
	}
	var people, teams []Mentionable
	if e.access != nil {
		list, err := e.access.ListPeople(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range list {
			if has(p.Name, p.Email) {
				name := p.Name
				if name == "" {
					name = p.Email
				}
				people = append(people, Mentionable{Kind: store.MentionPerson, ID: p.Email, Name: name})
			}
		}
	}
	list, err := e.List(ctx, "Team", Filter{}, false)
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		if has(t.Name, t.ID) {
			teams = append(teams, Mentionable{Kind: store.MentionTeam, ID: t.ID, Name: t.Name})
		}
	}
	byName := func(l []Mentionable) {
		sort.SliceStable(l, func(i, j int) bool { return strings.ToLower(l[i].Name) < strings.ToLower(l[j].Name) })
	}
	byName(people)
	byName(teams)
	out := append(people, teams...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// MyMentions returns the comments that ask the caller, by their address
// or through a team they are on, newest first.
func (e *Engine) MyMentions(ctx context.Context, limit int) ([]store.Comment, error) {
	cs, err := e.commentStore()
	if err != nil {
		return nil, err
	}
	p := identity.PrincipalFrom(ctx)
	me := personKey(p)
	if me == "" {
		return []store.Comment{}, nil
	}
	parties := []store.Mention{{Kind: store.MentionPerson, ID: me}}
	if e.access != nil {
		if person, err := e.access.GetPerson(ctx, me); err == nil {
			for _, t := range union(person.Teams, person.DirectoryTeams) {
				parties = append(parties, store.Mention{Kind: store.MentionTeam, ID: t})
			}
		}
	}
	return cs.ListMentioning(ctx, parties, limit)
}

func newCommentID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "comment-" + hex.EncodeToString(b)
}
