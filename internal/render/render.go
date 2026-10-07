package render

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// Charter renders a project charter to HTML and JSON for a given snapshot.
// snapshot=0 means the working copy; snapshot>0 means a specific version.
// Returns byte slices for HTML and JSON, both deterministic.
func Charter(ctx context.Context, e *engine.Engine, projectID string, snapshot int) (html, jsonData []byte, err error) {
	var vers engine.Version
	if snapshot == 0 {
		vers, err = e.Get(ctx, "Project", projectID)
	} else {
		vers, err = e.GetVersion(ctx, "Project", projectID, snapshot)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get project: %w", err)
	}

	manifest, err := e.Codec().Decode(vers.YAML)
	if err != nil {
		return nil, nil, fmt.Errorf("decode manifest: %w", err)
	}

	projectChecks, err := e.ProjectChecks(ctx, projectID, false)
	if err != nil {
		return nil, nil, fmt.Errorf("get checks: %w", err)
	}

	htmlBytes, err := projectCharter(ctx, e, projectID, vers)
	if err != nil {
		return nil, nil, fmt.Errorf("render html: %w", err)
	}

	// The JSON beside it: manifest, version and checks, with sorted keys so
	// two renders of the same snapshot are byte-identical.
	data := map[string]any{
		"manifest": manifest,
		"version":  vers,
		"checks":   projectChecks.Items,
	}
	// The work breakdown travels in the bundle for the planning tool to
	// import, coded D1, D1.1 and so on; absent when no deliverable lists
	// tasks.
	_, spec, err := manifestOf(e, vers, projectID)
	if err != nil {
		return nil, nil, err
	}
	if wbs := workBreakdown(loadNames(ctx, e), spec); hasTasks(wbs) {
		data["workBreakdown"] = wbs
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal json: %w", err)
	}
	var sorted map[string]any
	if err := json.Unmarshal(jsonBytes, &sorted); err != nil {
		return nil, nil, fmt.Errorf("unmarshal json for formatting: %w", err)
	}
	jsonBytes, err = json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal json with indent: %w", err)
	}
	return htmlBytes, jsonBytes, nil
}

// reading prints a key result's baseline or target as a value with its
// unit and the month it is from: "30 days, June 2028".
func reading(kr, point map[string]any) string {
	value := number(point["value"])
	if value == "" {
		value = str(point["value"])
	}
	if value == "" {
		return ""
	}
	switch str(kr["kind"]) {
	case "percent":
		value += "%"
	default:
		if unit := str(kr["unit"]); unit != "" {
			value += " " + unit
		}
	}
	if when := month(str(point["date"])); when != "" {
		value += ", " + when
	}
	return value
}

// month prints "2028-06" as "June 2028", and anything else as written.
func month(s string) string {
	if t, err := time.Parse("2006-01", s); err == nil {
		return t.Format("January 2006")
	}
	return s
}

// WorkItem is one line of a project's work breakdown: a deliverable
// (D2) or one task under it (D2.1).
type WorkItem struct {
	Code        string `json:"code"`
	Deliverable string `json:"deliverable"`
	Task        string `json:"task,omitempty"`
	Name        string `json:"name"`
	Role        string `json:"role,omitempty"`
	Note        string `json:"note,omitempty"`
}

// workBreakdown lists the deliverables in their written order, each
// followed by its tasks. The codes number by position, so they are the
// same for the same version and need no stored field.
func workBreakdown(n names, spec map[string]any) []WorkItem {
	var out []WorkItem
	for i, dv := range list(spec["deliverables"]) {
		code := fmt.Sprintf("D%d", i+1)
		out = append(out, WorkItem{Code: code, Deliverable: str(dv["id"]), Name: str(dv["name"])})
		for j, t := range list(dv["tasks"]) {
			out = append(out, WorkItem{
				Code:        fmt.Sprintf("%s.%d", code, j+1),
				Deliverable: str(dv["id"]),
				Task:        str(t["id"]),
				Name:        str(t["name"]),
				Role:        n.ref(t["role"], "Resource", spec),
				Note:        str(t["note"]),
			})
		}
	}
	return out
}

func hasTasks(wbs []WorkItem) bool {
	for _, w := range wbs {
		if w.Task != "" {
			return true
		}
	}
	return false
}
