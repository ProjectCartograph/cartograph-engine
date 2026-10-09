package render

import (
	"context"
	"regexp"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// A charter as parts (TAXONOMY.md D55): each section the renderer writes,
// in order, with what it wrote under its heading and the fields that
// writing holds by JSON pointer, so an interface draws the charter as
// blocks it can edit in place, and the screen, the PDF and the pack still
// come from one renderer. A section the renderer started and had nothing
// to write under is a part with nothing in it: what belongs there, still
// to say.

// Part is one section of a charter.
type Part struct {
	Title string `json:"title"`
	// Step is the step of the project's walk the section is written in.
	Step string `json:"step,omitempty"`
	// Anchor is the section's id in the HTML, "" for an empty part.
	Anchor string `json:"anchor,omitempty"`
	// HTML is what the renderer wrote under the heading, escaped as the
	// charter is.
	HTML string `json:"html"`
	// Fields are the JSON pointers of the fields written in it as typed,
	// each editable in place.
	Fields []string `json:"fields"`
	Empty  bool     `json:"empty"`
	start  int
}

var fieldAttr = regexp.MustCompile(`data-field="([^"]+)"`)

// closePart ends the part being written at the builder's current length.
func (d *doc) closePart() {
	if !d.opened {
		return
	}
	pt := &d.parts[d.openAt]
	pt.HTML = strings.TrimSpace(d.b.String()[pt.start:])
	pt.Fields = []string{}
	for _, m := range fieldAttr.FindAllStringSubmatch(pt.HTML, -1) {
		pt.Fields = append(pt.Fields, m[1])
	}
	d.opened = false
}

// CharterParts is a project's charter as its parts, as ctx reads it.
func CharterParts(ctx context.Context, e *engine.Engine, projectID string) ([]Part, error) {
	vers, err := e.Get(ctx, "Project", projectID)
	if err != nil {
		return nil, err
	}
	d, err := projectCharterDoc(ctx, e, projectID, vers)
	if err != nil {
		return nil, err
	}
	d.closePart()
	if d.pending != "" {
		d.parts = append(d.parts, Part{Title: d.pending, Step: sectionSteps[d.pending], Empty: true, Fields: []string{}})
	}
	return d.parts, nil
}
