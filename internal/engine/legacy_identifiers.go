package engine

import (
	"fmt"
	"strings"
)

// Enum values are identifiers, so they are written like identifiers.
//
// Until 2026-09-29 they were written as prose — "at closing", "manual
// re-entry", "law or regulation" — which reads well in one place and badly
// everywhere else: it cannot be a key, it cannot be a CSS class or a test
// name without quoting, and a space is the one character every tool
// disagrees about. Kubernetes settled this long ago and Cartograph follows it:
// lowerCamelCase, no spaces, no hyphens.
//
// The three lifecycle values mirror the phase names Cartograph already uses
// (initiation, closing, landing) rather than shortening them, so the
// identifier and the phase it refers to read alike.
//
// Files written before the change still say the old thing, so every read
// maps them forward. The map is by field rather than global: "paper" is a
// DataSource category and a Handoff, and nothing should rename one because
// the other changed.
var renamedEnums = map[string]map[string]string{
	// "irregular" is the Dublin Core frequency term (2026-09-30).
	"refresh": {"ad hoc": "irregular", "adHoc": "irregular"},
	"handoff": {
		"api or feed":     "apiOrFeed",
		"file transfer":   "fileTransfer",
		"shared database": "sharedDatabase",
		"manual re-entry": "manualReentry",
	},
	"mandateKind": {
		"law or regulation": "lawOrRegulation",
		"business case":     "businessCase",
	},
	"dataOutput": {
		"a new data source":             "newDataSource",
		"records in an existing source": "recordsInExistingSource",
		"documents or files":            "documentsOrFiles",
		"an extract or report":          "extractOrReport",
	},
	"dataSourceCategory": {
		"paper record":         "paperRecord",
		"form or survey":       "formOrSurvey",
		"operational system":   "operationalSystem",
		"api or feed":          "apiOrFeed",
		"document store":       "documentStore",
		"external third party": "externalThirdParty",
	},
	"direction": {"needed-by": "neededBy"},
	// Roles took their standard names on 2026-09-30: "owner" had been
	// printed as "Responsible", a RACI letter, and "filer" was a word no
	// standard uses (TAXONOMY.md D23).
	"role": {
		"technical-owner":   "technicalLead",
		"technicalOwner":    "technicalLead",
		"data-owner":        "dataOwner",
		"operational-owner": "serviceOwner",
		"operationalOwner":  "serviceOwner",
		"lead":              "manager",
		"owner":             "teamMember",
		"custodian":         "dataCustodian",
		"filer":             "projectSupport",
	},
	"when": {
		"at closing":               "atClosing",
		"at landing":               "atLanding",
		"each cycle after closing": "postClosingCycle",
	},
	"complianceStatus": {
		"not started":    "notStarted",
		"in progress":    "inProgress",
		"not applicable": "notApplicable",
	},
	"resourceCategory": {
		"person-role":    "personRole",
		"external-party": "externalParty",
		// "unit" read as a unit of measure, which is another kind.
		"unit": "orgUnit",
	},
	// Success dimensions took Shenhar and Dvir's five on 2026-09-30; the
	// four HEART words all measured the customer.
	"metric": {
		"product":    "customer",
		"happiness":  "customer",
		"adoption":   "customer",
		"engagement": "customer",
	},
}

// renameEnums maps every prose enum value in one manifest forward to its
// identifier. Runs on read, before validation, so an old file opens.
func renameEnums(kind string, spec map[string]any) []string {
	var notes []string
	set := func(holder map[string]any, field, table string) {
		if holder == nil {
			return
		}
		old, _ := holder[field].(string)
		if next, renamed := renamedEnums[table][old]; renamed {
			holder[field] = next
			notes = append(notes, fmt.Sprintf("%s %q renamed to %q", field, old, next))
		}
	}
	eachItem := func(list any, fn func(map[string]any)) {
		items, ok := list.([]any)
		if !ok {
			return
		}
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				fn(m)
			}
		}
	}

	switch kind {
	case "Resource":
		set(spec, "category", "resourceCategory")
	case "DataSource":
		set(spec, "category", "dataSourceCategory")
		// Who runs a source is provenance, not a category (DCAT).
		if spec["category"] == "externalThirdParty" {
			delete(spec, "category")
			spec["provenance"] = "external"
			notes = append(notes, `category "externalThirdParty" moved to provenance "external"`)
		}
		if c, ok := spec["classification"].(string); ok {
			switch lc := strings.ToLower(strings.TrimSpace(c)); lc {
			case "public", "internal", "confidential", "restricted":
				if lc != c {
					spec["classification"] = lc
					notes = append(notes, fmt.Sprintf("classification %q renamed to %q", c, lc))
				}
			default:
				delete(spec, "classification")
				notes = append(notes, fmt.Sprintf("classification %q is not in the scheme and was dropped", c))
			}
		}
		if r, ok := spec["refresh"].(map[string]any); ok {
			set(r, "cadence", "refresh")
		}
		set(spec, "refresh", "refresh")
	}

	eachItem(spec["mandate"], func(m map[string]any) { set(m, "kind", "mandateKind") })
	eachItem(spec["resources"], func(m map[string]any) { set(m, "role", "role") })
	eachItem(spec["successCriteria"], func(m map[string]any) {
		set(m, "when", "when")
		set(m, "metric", "metric")
	})
	eachItem(spec["compliance"], func(m map[string]any) { set(m, "status", "complianceStatus") })
	eachItem(spec["risks"], func(m map[string]any) {
		if d, ok := m["depends"].(map[string]any); ok {
			set(d, "direction", "direction")
		}
	})
	if data, ok := spec["data"].(map[string]any); ok {
		eachItem(data["consume"], func(m map[string]any) {
			set(m, "handoff", "handoff")
			set(m, "refresh", "refresh")
		})
		eachItem(data["produce"], func(m map[string]any) {
			set(m, "output", "dataOutput")
			set(m, "handoff", "handoff")
			set(m, "refresh", "refresh")
		})
	}
	return notes
}
