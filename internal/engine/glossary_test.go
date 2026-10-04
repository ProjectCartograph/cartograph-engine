package engine_test

import (
	"regexp"
	"strings"
	"testing"
)

// Every word of the taxonomy is defined as a dictionary defines it, for a
// person meeting it for the first time (TAXONOMY.md D29): one plain
// sentence or two that say what it is without repeating the word, short
// enough to read at a glance, naming no standard, field or identifier,
// and an example. The glossary lists them in the order of work.
func TestTheGlossaryDefinesEveryWordPlainly(t *testing.T) {
	g, err := newTestEngine(t).Glossary("en")
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, e := range g {
		keys = append(keys, e.Key)
	}
	if got := strings.Join(keys, " "); !strings.HasPrefix(got, "purpose goal objective outcome kpi gap") || !strings.Contains(got, "Team") ||
		!strings.Contains(got, "project component") {
		t.Fatalf("the glossary is not in the order of work: %s", got)
	}
	jargon := regexp.MustCompile(`(?i)\b(KPI|manifest|spec|schema|ref|id|TAXONOMY|D\d+|PMI|MSP|ITIL|PRINCE2|GovS|logframe|ADR)\b|/`)
	for _, e := range g {
		name := strings.ToLower(e.Kind)
		if e.Level != "" {
			name = e.Level
		} else if e.Key != e.Kind && e.Key == strings.ToLower(e.Key) && !strings.EqualFold(e.Key, e.Kind) && e.Key != "kpi" && e.Key != "stakeholders" {
			name = e.Key
		}
		words := len(strings.Fields(e.Summary))
		switch {
		case e.Summary == "" || e.Example == "":
			t.Errorf("%s: no summary or no example", e.Key)
		case words > 30:
			t.Errorf("%s: %d words is more than a glance: %q", e.Key, words, e.Summary)
		case jargon.MatchString(e.Summary) || jargon.MatchString(e.Example):
			t.Errorf("%s: names jargon a newcomer cannot read: %q / %q", e.Key, e.Summary, e.Example)
		case strings.HasPrefix(strings.ToLower(e.Summary), "a "+name) || strings.HasPrefix(strings.ToLower(e.Summary), name):
			t.Errorf("%s: repeats its own word: %q", e.Key, e.Summary)
		case !strings.HasSuffix(e.Summary, "."):
			t.Errorf("%s: not a sentence: %q", e.Key, e.Summary)
		}
	}
}
