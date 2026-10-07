package render

import (
	"context"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// TestChartersReadAsDocuments is the control for LSS_REVIEW.md: every
// charter the example vault can produce, of every kind, prints names and
// plain labels. Measured before the rewrite, one project charter printed 20
// ids, 6 enum names, 9 empty role rows and 5 blank durations.
func TestChartersReadAsDocuments(t *testing.T) {
	ctx := context.Background()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ImportDir(ctx, "../../examples/minimal", "test", "fixture"); err != nil {
		t.Fatalf("import: %v", err)
	}

	// Every id in the vault, so the test can say which one leaked.
	ids := map[string]string{}
	for _, kind := range charterKinds {
		summaries, _ := e.List(ctx, kind, engine.Filter{}, false)
		for _, s := range summaries {
			// An id that is also its own name (a one-word id) cannot leak.
			if strings.Contains(s.ID, "-") && !strings.EqualFold(s.ID, s.Name) {
				ids[s.ID] = kind
			}
		}
	}

	enumNames := regexp.MustCompile(`\b(atClosing|atLanding|postClosingCycle|newDataSource|recordsInExistingSource|operationalOwner|technicalOwner|inProgress|notStarted|lawOrRegulation|apiOrFeed|manualReentry)\b`)
	emptyRow := regexp.MustCompile(`<tr>(<td></td>)+</tr>`)
	tags := regexp.MustCompile(`<[^>]+>`)

	check := func(t *testing.T, kind, id string, doc []byte) {
		t.Helper()
		raw := string(doc)
		if emptyRow.MatchString(raw) {
			t.Errorf("%s %s: charter prints an empty row", kind, id)
		}
		if strings.Contains(raw, "<h2></h2>") {
			t.Errorf("%s %s: charter prints an empty heading", kind, id)
		}
		body := raw[strings.Index(raw, "<body>"):]
		text := html.UnescapeString(tags.ReplaceAllString(body, " "))
		if m := enumNames.FindString(text); m != "" {
			t.Errorf("%s %s: charter prints the identifier %q", kind, id, m)
		}
		for leaked, owner := range ids {
			if regexp.MustCompile(`(^|[\s(,;:])` + regexp.QuoteMeta(leaked) + `($|[\s),;:.])`).MatchString(text) {
				t.Errorf("%s %s: charter prints the %s id %q instead of its name", kind, id, owner, leaked)
			}
		}
	}

	projects, _ := e.List(ctx, "Project", engine.Filter{}, false)
	for _, p := range projects {
		doc, _, err := Charter(ctx, e, p.ID, 0)
		if err != nil {
			t.Fatalf("project %s: %v", p.ID, err)
		}
		check(t, "Project", p.ID, doc)
	}
	programmes, _ := e.List(ctx, "Programme", engine.Filter{}, false)
	for _, p := range programmes {
		doc, err := ProgrammeCharter(ctx, e, p.ID)
		if err != nil {
			t.Fatalf("programme %s: %v", p.ID, err)
		}
		check(t, "Programme", p.ID, doc)
	}
	operations, _ := e.List(ctx, "Operation", engine.Filter{}, false)
	for _, o := range operations {
		doc, err := OperationCharter(ctx, e, o.ID)
		if err != nil {
			t.Fatalf("operation %s: %v", o.ID, err)
		}
		check(t, "Operation", o.ID, doc)
	}
	t.Logf("checked %d projects, %d programmes, %d operations against %d ids",
		len(projects), len(programmes), len(operations), len(ids))
	if len(projects) == 0 || len(programmes) == 0 || len(operations) == 0 {
		t.Fatalf("fixture lost a kind: %d projects, %d programmes, %d operations",
			len(projects), len(programmes), len(operations))
	}
}

// TestCharterEscapesText: a definition is typed by people and a charter is
// opened by others, so nothing typed can become markup.
func TestCharterEscapesText(t *testing.T) {
	var d doc
	d.p(`<script>alert(1)</script>`)
	d.fields(field{"Who", `a & b <i>`})
	out := d.b.String()
	if strings.Contains(out, "<script>") || strings.Contains(out, "<i>") {
		t.Fatalf("unescaped text in charter: %s", out)
	}
}

// The cover names the organisation, with its logo where the purpose has
// one (TAXONOMY.md D37).
func TestTheCoverCarriesTheOrganisationsLogo(t *testing.T) {
	logo := "data:image/png;base64,iVBORw0KGgo="
	d := doc{org: "Northwind Co-operative", logo: logo}
	d.head("A project", "Project charter", engine.Version{})
	out := d.b.String()
	if !strings.Contains(out, `<p class="org"><img src="`+logo+`" alt="">Northwind Co-operative</p>`) {
		t.Fatalf("the cover has no organisation and logo: %s", out)
	}
	var none doc
	none.head("A project", "Project charter", engine.Version{})
	if strings.Contains(none.b.String(), `class="org"`) {
		t.Fatalf("a cover with no organisation prints one: %s", none.b.String())
	}
}
