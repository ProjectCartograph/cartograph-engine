package engine_test

import (
	"context"
	"testing"
)

// A gap is what is wrong and how we know; a problem is what that does to a
// named group, on this piece of work. They are different things, so the
// problem cites the gap rather than restating it.
func TestGapAndTheProblemsThatCiteIt(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	if _, err := e.Commit(ctx, "Gap", "gaps-emerge-early", []byte(
		"apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: gaps-emerge-early\n  name: Gaps emerge early\nspec:\n"+
			"  statement: Learning gaps emerge early and widen over time.\n"+
			"  source: \"National reading assessment report, page 12\"\n"+
			"  measuredBy: d1\n"), "local", "seed"); err != nil {
		t.Fatal(err)
	}

	project := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-gap\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n"
	programme := "apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: prog-gap\n  name: Prog\nspec:\n  name: Prog\n  aim: {change: Better}\n  leadTeam: t1\n  problems:\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "a gap with only its citation", kind: "Gap",
			yaml: "apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: g-cited\n  name: Cited\nspec:\n  statement: Something is wrong.\n  source: \"A report, page 4\"\n",
		},
		{
			// Most of a plan's findings are cited, not measured. A gap that
			// insisted on a data source could not hold them.
			name: "a gap with no evidence beyond its statement", kind: "Gap",
			yaml: "apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: g-bare\n  name: Bare\nspec:\n  statement: Something is wrong.\n",
		},
		{
			name: "a gap measured by a source that does not exist", kind: "Gap",
			wantProblem: true, wantSubstr: "does not exist",
			yaml: "apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: g-bad\n  name: Bad\nspec:\n  statement: Something is wrong.\n  measuredBy: no-such-source\n",
		},
		{
			// Since 2026-09-30 a gap is defined by its name and its two
			// states; the quote is evidence and optional.
			name: "a gap with a name and no quote", kind: "Gap", wantProblem: false,
			yaml: "apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: g-empty\n  name: Empty\nspec:\n  source: \"A report\"\n",
		},
		{
			name: "a project problem citing a gap", kind: "Project",
			yaml: project + "      - problem: {situation: Students reach an examination before anyone knows}\n        change: {what: Somebody knows sooner}\n        gaps: [{gap: gaps-emerge-early}]\n",
		},
		{
			// Both carry problems, and the plan cites sections at both
			// levels, so both link the same way.
			name: "a programme problem citing a gap", kind: "Programme",
			yaml: programme + "    - problem: {situation: The system cannot see a falling cohort}\n      change: {what: It can}\n      gaps: [{gap: gaps-emerge-early}]\n",
		},
		{
			name: "a problem citing a gap that does not exist", kind: "Project",
			wantProblem: true, wantSubstr: "references Gap \"no-such-gap\", which does not exist",
			yaml: project + "      - problem: {situation: Students reach an examination before anyone knows}\n        change: {what: Somebody knows sooner}\n        gaps: [{gap: no-such-gap}]\n",
		},
		{
			// The same gap lands on different groups in different work, so
			// who feels it stays on the problem and never on the gap.
			name: "two problems may cite the same gap", kind: "Project",
			yaml: project + "      - problem: {situation: Students reach an examination before anyone knows}\n        change: {what: Somebody knows sooner}\n        gaps: [{gap: gaps-emerge-early}]\n" +
				"      - problem: {situation: Teachers cannot tell a weak class from a hard year}\n        change: {what: They can}\n        gaps: [{gap: gaps-emerge-early}]\n",
		},
	})
}
