package render

import (
	"context"
	"strings"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// A part's HTML is read as the lines a person reads, and two versions are
// diffed line by line: kept, removed from the record, added by the change.
func TestAPartIsDiffedLineByLine(t *testing.T) {
	before := textLines(`<dl><dt>Problem</dt><dd data-field="/spec/x">Depots grade produce differently</dd></dl>` +
		`<table><tr><th>Risk</th><th>Owner</th></tr><tr><td>Turnover</td><td>Quality Lead</td></tr></table>`)
	if strings.Join(before, "|") != "Problem|Depots grade produce differently|Risk · Owner|Turnover · Quality Lead" {
		t.Fatalf("lines %q", before)
	}
	after := []string{"Problem", "Depots grade the same crates differently", "Risk · Owner", "Turnover · Quality Lead"}
	var got []string
	for _, l := range diffLines(before, after) {
		got = append(got, l.Op+":"+l.Text)
	}
	want := "same:Problem|removed:Depots grade produce differently|added:Depots grade the same crates differently|same:Risk · Owner|same:Turnover · Quality Lead"
	if strings.Join(got, "|") != want {
		t.Fatalf("diff %q", got)
	}
}

// Parts are matched by step and title: a part only the change set has is
// added, one it drops is removed where it stood, one it rewrites changed,
// and a project new in the change set is added whole.
func TestACharterIsDiffedPartByPart(t *testing.T) {
	part := func(title, step, html string) Part { return Part{Title: title, Step: step, HTML: html} }
	before := []Part{part("Problem", "aim", "<p>Depots differ</p>"), part("Budget", "costs", "<p>To be confirmed</p>"), part("Risks", "risks", "<p>Turnover</p>")}
	after := []Part{part("Problem", "aim", "<p>Depots grade differently</p>"), part("Scope", "scope", "<p>Six depots</p>"), part("Risks", "risks", "<p>Turnover</p>")}
	var got []string
	for _, p := range diffParts(before, after) {
		got = append(got, p.Title+":"+p.State)
	}
	if strings.Join(got, "|") != "Problem:changed|Budget:removed|Scope:added|Risks:same" {
		t.Fatalf("parts %q", got)
	}
	// A new project's parts and lines are all added: none was in the record.
	for _, p := range diffParts(nil, after) {
		if p.State != DiffAdded || len(p.Lines) == 0 || p.Lines[0].Op != DiffAdded {
			t.Fatalf("a new project's part %+v", p)
		}
	}
}

// A charter read the same way twice has nothing changed.
func TestACharterAgainstItselfIsTheSame(t *testing.T) {
	ctx := context.Background()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	text := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: p\n  name: P\nspec:\n  team: t\n  summary:\n    problems:\n      - id: pr-1\n        problem: {situation: depots grade produce differently}\n"
	if err := e.PutWorking(ctx, "Project", "p", []byte(text)); err != nil {
		t.Fatal(err)
	}
	parts, err := CharterDiff(ctx, ctx, e, "p")
	if err != nil || len(parts) == 0 {
		t.Fatalf("diff %v %v", parts, err)
	}
	for _, p := range parts {
		if p.State != DiffSame {
			t.Fatalf("a part changed against itself: %+v", p)
		}
	}
}
