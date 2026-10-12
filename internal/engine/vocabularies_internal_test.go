package engine

import (
	"strings"
	"testing"
)

// A workspace's own list replaces the standard one of that name, and the
// others stand (TAXONOMY.md D71).
func TestAWorkspaceSetsItsOwnWords(t *testing.T) {
	t.Parallel()
	got := vocabulariesWith(map[string][]VocabularyTerm{"classification": {{Value: "official", Label: "Official"}, {Value: "secret", Label: "Secret"}}})
	if len(got["classification"]) != 2 || got["classification"][1].Value != "secret" {
		t.Fatalf("classification: %+v", got["classification"])
	}
	if len(got["refresh"]) != len(standardVocabularies["refresh"]) {
		t.Fatal("a list the workspace did not set was not the standard one")
	}
}

// A value outside the workspace's list is said, with the words it has,
// and not refused.
func TestAValueOutsideTheListIsSaid(t *testing.T) {
	t.Parallel()
	vocab := vocabulariesWith(map[string][]VocabularyTerm{"classification": {{Value: "official", Label: "Official"}}})
	checks := vocabularyChecks(vocab, map[string]any{"classification": "secret", "refresh": "biweekly"})
	if len(checks) != 1 || checks[0].ID != "vocabulary-classification" || !strings.Contains(checks[0].Message, "Official") {
		t.Fatalf("checks: %+v", checks)
	}
	if got := vocabularyChecks(vocab, map[string]any{"classification": "official"}); len(got) != 0 {
		t.Fatalf("a value the list has was said: %+v", got)
	}
}

// Every field the contract gives a vocabulary names one the engine has
// standard terms for, so no workspace starts with an empty choice.
func TestEveryVocabularyAFieldNamesHasStandardTerms(t *testing.T) {
	t.Parallel()
	ss, err := loadSchemas()
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if name, ok := v["x-cartograph-vocabulary"].(string); ok {
				found++
				if len(standardVocabularies[name]) == 0 {
					t.Errorf("a field names the vocabulary %q, which has no standard terms", name)
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	for _, doc := range ss.raw {
		walk(doc)
	}
	if found == 0 {
		t.Fatal("no field names a vocabulary; this test would never fail")
	}
}
