package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/structure"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerPortTools adds the tools that port a document: the whole chain, from its text to every record written.
func registerPortTools(s *sdk.Server, o Options, person identity.Principal) {
	e := o.Engine

	tool(s, o, person, &sdk.Tool{Name: "port", Description: "Port a document in one chain: the way to begin any port. First call it with the document's title and text " +
		"(straight from its file): it keeps the document, splits it into sections and names the ones that list pieces of work. Read those with read_section, " +
		"then call port again with the pieces (no text needed): it decides the structure, drafts every record, writes every register the document has " +
		"(milestones, deliverables, risks, indicators) into the main project, and hands you the first record to settle. After it: one settle a record, then propose.", Annotations: drafting},
		func(c call, in portIn) (any, error) {
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			srcs, err := e.Sources(c.ctx, cs.ID)
			if err != nil {
				return nil, err
			}
			if len(srcs) == 0 {
				if strings.TrimSpace(in.Text) == "" {
					return nil, fmt.Errorf("%w: give the document's text, straight from its file, on the first call", engine.ErrBadEdit)
				}
				if err := engine.WholeFile(in.Text, in.FileSize); err != nil {
					return nil, err
				}
				src, err := e.BringSource(c.ctx, cs.ID, in.Title, in.Text)
				if err != nil {
					return nil, err
				}
				srcs = append(srcs, src)
			}
			var pieceSections []string
			for _, sec := range srcs[0].Sections {
				for _, f := range sec.Feeds {
					if f == "pieces" {
						pieceSections = append(pieceSections, sec.ID)
						break
					}
				}
			}
			if len(in.Records) > 0 {
				return portRecords(c, cs.ID, in.Records)
			}
			if len(in.Pieces) == 0 {
				return map[string]any{"changeSet": cs.ID, "sections": srcs[0].Sections, "piecesIn": pieceSections,
					"questions": structure.Questions, "example": json.RawMessage(structureExample),
					"next": "Read sections " + strings.Join(pieceSections, ", ") + " with read_section, list every piece of work they name, answer the questions " +
						"here for each, and call port again with title and pieces, shaped as example. Do not call structure or start_work: port does both, and writes the registers."}, nil
			}
			return portPieces(c, cs.ID, srcs[0], in.Pieces)
		})

	tool(s, o, person, &sdk.Tool{Name: "bring_document", Description: "Bring the document you are porting into your change set, once, as its text: Cartograph keeps it with the work, " +
		"splits it into sections by its headings and answers with its outline, each section marked with what it feeds. From then on read only the sections a step needs, " +
		"with read_section: every check next and settle hand over names the sections that answer it. Pass the text straight from the file (with a command line helper, " +
		"have a script build the JSON from the file): never retype or paste a long document, and never read it whole.", Annotations: drafting},
		func(c call, in bringIn) (any, error) {
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			if err := engine.WholeFile(in.Text, in.FileSize); err != nil {
				return nil, err
			}
			src, err := e.BringSource(c.ctx, cs.ID, in.Title, in.Text)
			if err != nil {
				return nil, err
			}
			var first []string
			for _, sec := range src.Sections {
				for _, f := range sec.Feeds {
					if f == "pieces" {
						first = append(first, sec.ID)
						break
					}
				}
			}
			return map[string]any{"changeSet": cs.ID, "title": src.Title, "sections": src.Sections,
				"next": fmt.Sprintf("Read sections %s with read_section: they name the pieces of work. Then start_work with the pieces, in this change set.", strings.Join(first, ", "))}, nil
		})

	tool(s, o, person, &sdk.Tool{Name: "read_section", Description: "Read sections of the document brought into your change set, by the ids its outline gave (s4, or 2:s4 for a second document). " +
		"Read only those a step needs: every check names the sections that answer it.", Annotations: readOnly},
		func(c call, in readSectionIn) (any, error) {
			cs, c, found, err := c.inChangeSet(in.ChangeSet, false)
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, fmt.Errorf("%w: no change set yet: bring the document in with bring_document", engine.ErrNotFound)
			}
			return e.ReadSections(c.ctx, cs.ID, in.IDs)
		})

	tool(s, o, person, &sdk.Tool{Name: "settle_register", Description: "Write a whole register from the document in one call: the rows of a section's table " +
		"(its milestones, deliverables, risks, or the indicators a project names) are read by Cartograph and settled into a project's list, each row as an item, " +
		"names of roles and bodies found or drafted, dates read as months. Use it for every register the document has, instead of retyping its rows; " +
		"then fix what comes back refused, and add what the table did not give with settle. Give the section by the id its outline gave.", Annotations: drafting},
		func(c call, in registerIn) (any, error) {
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			reg, err := e.RegisterInto(c.ctx, cs.ID, in.Kind, in.ID, in.Section, in.Field, nil)
			if err != nil {
				return nil, err
			}
			rows, added, refused, drafted := reg.Rows, reg.Added, taught(c, in.Kind, reg.Refused), reg.Drafted
			if rows == 0 {
				return nil, fmt.Errorf("%w: section %s holds no rows Cartograph can read as %s: write them with settle", engine.ErrNotFound, in.Section, in.Field)
			}
			if c.ctx, err = e.InChangeSet(c.ctx, cs.ID); err != nil {
				return nil, err
			}
			out := map[string]any{"changeSet": cs.ID, "record": in.Kind + "/" + in.ID, "field": in.Field, "rows": rows, "added": added}
			if drafted > 0 {
				out["drafted"] = drafted
			}
			if len(refused) > 0 {
				out["refused"] = refused
				out["fix"] = "Every other row was kept. Fix the refused ones with settle, in the shape each says."
			}
			work := in.Work
			if len(work) == 0 {
				work = []string{in.Kind + "/" + in.ID}
			}
			next, err := nextOf(c, cs.ID, true, work, "")
			if err != nil {
				return nil, err
			}
			out["then"] = next
			return out, nil
		})
}
