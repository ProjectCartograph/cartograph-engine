package engine

import (
	"fmt"
	"sort"
	"strings"
)

// The words a workspace uses where a field's choices are its own
// (TAXONOMY.md D71). A field names its vocabulary in the contract
// (x-cartograph-vocabulary); the workspace may set its own list in
// settings, and a value outside the list is said by a check rather than
// refused, since the list is the organisation's to grow.

// VocabularyTerm is one choice: what a record stores, what a person
// reads, and what it means.
type VocabularyTerm struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// standardVocabularies are the terms a workspace starts from.
//
// classification is ISO/IEC 27002's example scheme (5.12): an
// organisation defines its own levels, so these are a start, not a rule.
// refresh is the EU Publications Office frequency list DCAT-AP names for
// accrualPeriodicity, with continuous and irregular as Dublin Core has
// them, and onEvent for a source an event sets off (D65). dataSourceCategory
// is the form a source takes; DCAT says it by a distribution's format, so
// it is a workspace's own list too.
var standardVocabularies = map[string][]VocabularyTerm{
	"classification": {
		{"public", "Public", "Anyone may see it; its release harms no one."},
		{"internal", "Internal", "Anyone in the organisation may see it; released outside, it would cause minor harm."},
		{"confidential", "Confidential", "Only the roles that need it may see it; released, it would cause serious harm."},
		{"restricted", "Restricted", "Only named roles may see it, under the strictest controls; released, it would cause severe harm or break the law."},
	},
	"refresh": {
		{"continuous", "Real time", "Each record lands as it is made: an operational system, a live feed."},
		{"onEvent", "When something happens", "Changes when something sets it off, such as a harvest closed, not on a calendar."},
		{"daily", "Daily", ""},
		{"weekly", "Weekly", ""},
		{"biweekly", "Every two weeks", ""},
		{"monthly", "Monthly", ""},
		{"quarterly", "Quarterly", ""},
		{"termly", "Each term", ""},
		{"semiannual", "Twice a year", ""},
		{"annual", "Annual", ""},
		{"irregular", "Irregular", "Changes with no rhythm and no event that can be named."},
		{"unknown", "Not known", "How often it changes is not known yet."},
	},
	"dataSourceCategory": {
		{"database", "Database", ""},
		{"spreadsheet", "Spreadsheet", ""},
		{"paperRecord", "Paper record", ""},
		{"formOrSurvey", "Form or survey", ""},
		{"operationalSystem", "Operational system", ""},
		{"apiOrFeed", "API or feed", ""},
		{"documentStore", "Document store", ""},
		{"warehouse", "Warehouse", "Holds more than one kind of record; each metric says which it counts (D67)."},
	},
}

// vocabulariesWith lays a workspace's own lists over the standard ones: a
// list it sets replaces the standard list of that name whole.
func vocabulariesWith(own map[string][]VocabularyTerm) map[string][]VocabularyTerm {
	out := make(map[string][]VocabularyTerm, len(standardVocabularies)+len(own))
	for k, v := range standardVocabularies {
		out[k] = v
	}
	for k, v := range own {
		if len(v) > 0 {
			out[k] = v
		}
	}
	return out
}

// vocabularyChecks says, for each field of a data source with a
// vocabulary, a value the workspace's list does not have.
func vocabularyChecks(vocab map[string][]VocabularyTerm, spec map[string]any) []ProgrammeCheck {
	fields := []struct{ key, vocabulary, words string }{
		{"classification", "classification", "classification"},
		{"refresh", "refresh", "refresh"},
		{"category", "dataSourceCategory", "form"},
	}
	var out []ProgrammeCheck
	for _, f := range fields {
		v, _ := spec[f.key].(string)
		if v == "" {
			continue
		}
		terms := vocab[f.vocabulary]
		known := false
		var labels []string
		for _, t := range terms {
			known = known || t.Value == v
			labels = append(labels, t.Label)
		}
		if known {
			continue
		}
		sort.Strings(labels)
		out = append(out, ProgrammeCheck{ID: "vocabulary-" + f.key, Section: f.key, State: programmeCheckWarn,
			Message: fmt.Sprintf("Its %s %q is not one of this workspace's: %s. Choose one, or add it to the workspace's list.", f.words, v, strings.Join(labels, ", "))})
	}
	return out
}

// VocabularyLabel is what a person reads for a stored value, from the
// workspace's list; the value itself where the list does not have it.
func (e *Engine) VocabularyLabel(vocab map[string][]VocabularyTerm, name, value string) string {
	for _, t := range vocab[name] {
		if t.Value == value {
			return t.Label
		}
	}
	return value
}
