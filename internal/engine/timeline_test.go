package engine_test

import "testing"

// I3a (2026-09-17): Project.spec.timeline becomes {start (required),
// phases (required, minItems 1, each {name (required, maxLength 40),
// months (required, integer 1..60)})}. The end month is derived, never
// stored (see TestDerivedEnd in projectchecks_test.go). A beneficiary
func TestProjectTimelineSchema(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-timeline\n  name: Project Timeline\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "valid timeline, one phase",
			kind: "Project",
			yaml: base + "  timeline:\n    start: \"2025-09\"\n    phases:\n      - name: Pilot\n        months: 6\n",
		},
		{
			name: "valid timeline, several phases",
			kind: "Project",
			yaml: base + "  timeline:\n    start: \"2025-09\"\n    phases:\n      - name: Pilot\n        months: 3\n      - name: Rollout\n        months: 6\n",
		},
		{
			name: "missing start", kind: "Project", wantProblem: true, wantSubstr: "start",
			yaml: base + "  timeline:\n    phases:\n      - name: Pilot\n        months: 6\n",
		},
		{
			name: "missing phases", kind: "Project", wantProblem: true, wantSubstr: "phases",
			yaml: base + "  timeline:\n    start: \"2025-09\"\n",
		},
		{
			name: "empty phases violates minItems", kind: "Project", wantProblem: true,
			yaml: base + "  timeline:\n    start: \"2025-09\"\n    phases: []\n",
		},
		{
			name: "phase missing months", kind: "Project", wantProblem: true, wantSubstr: "months",
			yaml: base + "  timeline:\n    start: \"2025-09\"\n    phases:\n      - name: Pilot\n",
		},
		{
			name: "phase months over maximum", kind: "Project", wantProblem: true,
			yaml: base + "  timeline:\n    start: \"2025-09\"\n    phases:\n      - name: Pilot\n        months: 61\n",
		},
		{
			name: "phase months under minimum", kind: "Project", wantProblem: true,
			yaml: base + "  timeline:\n    start: \"2025-09\"\n    phases:\n      - name: Pilot\n        months: 0\n",
		},
		{
			name: "phase name over maxLength 40", kind: "Project", wantProblem: true,
			yaml: base + "  timeline:\n    start: \"2025-09\"\n    phases:\n      - name: " + repeatChar("p", 41) + "\n        months: 3\n",
		},
		{
			name: "phase end is no longer a property", kind: "Project", wantProblem: true,
			yaml: base + "  timeline:\n    start: \"2025-09\"\n    phases:\n      - name: Pilot\n        months: 3\n        end: \"2025-12\"\n",
		},
		{
			name: "timeline end is no longer a property", kind: "Project", wantProblem: true,
			yaml: base + "  timeline:\n    start: \"2025-09\"\n    end: \"2026-06\"\n    phases:\n      - name: Pilot\n        months: 3\n",
		},
	})
}

func repeatChar(s string, n int) string {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, s[0])
	}
	return string(out)
}
