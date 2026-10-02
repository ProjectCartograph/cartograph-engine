package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

type importFile struct {
	path string
	kind string
	id   string
	yaml []byte
	doc  map[string]any
}

// ImportDir reads every .yaml/.yml file under dir (recursively; a
// manifest's own kind and metadata.id decide where it goes, not its file
// path), validates the whole set together so members can reference each
// other, and if every file is valid commits them all in one transaction.
// If any file has a problem, nothing is written.
func (e *Engine) ImportDir(ctx context.Context, dir, actor, reason string) (Report, error) {
	paths, err := e.findManifestFiles(dir)
	if err != nil {
		return Report{}, err
	}

	files := make([]importFile, 0, len(paths))
	overlay := map[string]map[string]map[string]any{}
	report := Report{Imported: []ImportedItem{}, Problems: []FileProblems{}}

	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Report{}, fmt.Errorf("read %s: %w", path, err)
		}
		docs, err := e.splitDocuments(raw)
		if err != nil {
			report.Problems = append(report.Problems, FileProblems{
				File:     path,
				Problems: []Problem{{Path: "", Message: "invalid yaml: " + err.Error()}},
			})
			continue
		}
		for di, d := range docs {
			// A file holding several manifests names each one after the
			// document it came from, so a problem points at something a
			// person can find.
			p := path
			if len(docs) > 1 {
				p = fmt.Sprintf("%s [%d]", path, di+1)
			}
			doc, raw := d.doc, d.yaml
			kind, _ := doc["kind"].(string)
			if _, ok := kinds.ByName(kind); !ok {
				report.Problems = append(report.Problems, FileProblems{
					File:     p,
					Problems: []Problem{{Path: "/kind", Message: fmt.Sprintf("unknown kind %q", kind)}},
				})
				continue
			}
			// One release of backward compatibility for names an earlier schema retired
			// (Goal level "team" -> "functional", KeyResult/KPI baseline.asOf
			// and target.by -> baseline.date and target.date): rewrite doc in
			// place and re-marshal raw so both validation and the version
			// actually committed use the current field names, never the old
			// ones. A file already in the current shape is untouched.
			if notes := rewriteLegacyFields(kind, doc); len(notes) > 0 {
				rewritten, err := e.codec.Encode(doc)
				if err != nil {
					return Report{}, fmt.Errorf("re-marshal %s after legacy rewrite: %w", p, err)
				}
				raw = rewritten
				problems := make([]Problem, len(notes))
				for i, n := range notes {
					problems[i] = Problem{Message: n}
				}
				report.Notes = append(report.Notes, FileProblems{File: p, Problems: problems})
			}
			id, _ := docID(doc)
			if id == "" {
				report.Problems = append(report.Problems, FileProblems{
					File:     p,
					Problems: []Problem{{Path: "/metadata/id", Message: "missing metadata.id"}},
				})
				continue
			}
			if overlay[kind] == nil {
				overlay[kind] = map[string]map[string]any{}
			}
			if _, dup := overlay[kind][id]; dup {
				report.Problems = append(report.Problems, FileProblems{
					File:     p,
					Problems: []Problem{{Path: "/metadata/id", Message: fmt.Sprintf("duplicate %s/%s in this import", kind, id)}},
				})
				continue
			}
			overlay[kind][id] = doc
			files = append(files, importFile{path: p, kind: kind, id: id, yaml: raw, doc: doc})
		}
	}

	// Phase A: validate every file against the batch as a whole.
	for _, f := range files {
		_, problems, err := e.validate(ctx, f.kind, f.yaml, overlay)
		if err != nil {
			return Report{}, fmt.Errorf("validate %s: %w", f.path, err)
		}
		if len(problems) > 0 {
			report.Problems = append(report.Problems, FileProblems{File: f.path, Problems: problems})
		}
	}
	if len(report.Problems) > 0 {
		return report, nil
	}

	if actorProblem, err := e.checkActor(ctx, actor); err != nil {
		return Report{}, err
	} else if actorProblem != nil {
		report.Problems = append(report.Problems, FileProblems{File: "(import)", Problems: []Problem{*actorProblem}})
		return report, nil
	}

	// Phase B: everything validated, write it all in one transaction.
	err = e.manifests.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		for _, f := range files {
			// The next number follows the highest saved version, as Commit
			// does: the current manifest may be a working copy, which has
			// no number of its own.
			latest, err := latestNumber(ctx, tx, f.kind, f.id)
			if err != nil {
				return err
			}
			v := Version{Kind: f.kind, ID: f.id, Number: latest + 1, YAML: f.yaml, Actor: actor, Reason: reason, On: timeNow().UTC(), Doc: e.storedDoc(f.kind, f.doc)}
			if err := tx.PutVersion(ctx, v); err != nil {
				return fmt.Errorf("commit %s/%s: %w", f.kind, f.id, err)
			}
			if err := e.recordSeries(ctx, tx, v, f.doc); err != nil {
				return fmt.Errorf("commit %s/%s: %w", f.kind, f.id, err)
			}
			refs := extractRefs(f.doc, e.refRules[f.kind])
			storeRefs := make([]store.Ref, len(refs))
			for i, r := range refs {
				storeRefs[i] = store.Ref{Path: r.path, ToKind: r.kind, ToID: r.id}
			}
			if err := tx.IndexReferences(ctx, f.kind, f.id, storeRefs); err != nil {
				return err
			}
			report.Imported = append(report.Imported, ImportedItem{Kind: f.kind, ID: f.id, Version: v.Number})
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}

	sort.Slice(report.Imported, func(i, j int) bool {
		if report.Imported[i].Kind != report.Imported[j].Kind {
			return report.Imported[i].Kind < report.Imported[j].Kind
		}
		return report.Imported[i].ID < report.Imported[j].ID
	})
	return report, nil
}

func (e *Engine) findManifestFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Below the root, nothing hidden is a manifest. .cartograph is the
		// vault's own bookkeeping: the index, the journal, and the staging
		// directory that holds unfinished drafts, which are not manifests
		// yet (validating once reported a half-filled funding line in a
		// draft, 2026-09-29). A Kubernetes ConfigMap mounted as a
		// directory holds each file twice, under a hidden ..data
		// directory and through a link beside it, so walking into the
		// hidden one imports everything twice.
		if p != dir && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// vault.yaml at the root is the vault's own index, which serve
		// regenerates; it is not a manifest and importing it reports an
		// unknown kind "Vault" every time (seen on three separate vaults,
		// 2026-09-27).
		if rel, err := filepath.Rel(dir, p); err == nil && rel == "vault.yaml" {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == e.codec.Extension() || (e.codec.Name() == "yaml" && ext == ".yml") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// ExportDir writes the current version of every manifest to
// <dir>/<kind>/<id>.yaml, using the exact YAML text that was committed, so
// export is byte-for-byte reproducible. If the vault is vault-backed,
// also exports vault.yaml.
func (e *Engine) ExportDir(ctx context.Context, dir string) error {
	return e.ExportDirAs(ctx, dir, e.codec)
}

// ExportDirAs is ExportDir in another syntax: the migration path between
// codecs. With the engine's own codec the committed text is written byte
// for byte; with another, every manifest is decoded and re-encoded, so
// the export opens as a vault of that codec and nothing else changes.
func (e *Engine) ExportDirAs(ctx context.Context, dir string, target codec.Codec) error {
	for _, spec := range kinds.All {
		ids, err := e.manifests.ListIDs(ctx, spec.Name)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			continue
		}
		kindDir := filepath.Join(dir, spec.Name)
		if err := os.MkdirAll(kindDir, 0o755); err != nil {
			return err
		}
		for _, id := range ids {
			v, found, err := e.manifests.GetCurrent(ctx, spec.Name, id)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			text := v.YAML
			if target.Name() != e.codec.Name() {
				doc, err := e.codec.Decode(v.YAML)
				if err != nil {
					return fmt.Errorf("%s/%s: %w", spec.Name, id, err)
				}
				if text, err = target.Encode(doc); err != nil {
					return fmt.Errorf("%s/%s: %w", spec.Name, id, err)
				}
			}
			out := filepath.Join(kindDir, id+target.Extension())
			if err := os.WriteFile(out, text, 0o644); err != nil {
				return err
			}
		}
	}

	// A store with an apply gate exports its state manifest too, so the
	// export opens as the same vault.
	if e.state != nil {
		data, found, err := e.state.StateYAML(ctx)
		if err != nil {
			return err
		}
		if found {
			if err := os.WriteFile(filepath.Join(dir, "vault.yaml"), data, 0o644); err != nil {
				return err
			}
		}
	}

	return nil
}

// yamlDocument is one manifest out of a file, with the exact bytes it was
// written as: what gets committed is the text the person wrote, not a
// re-marshalled copy of it.
type yamlDocument struct {
	doc  map[string]any
	yaml []byte
}

// A file may hold several manifests (YAML's "---", the way every
// Kubernetes example is written); reading only the first silently lost
// the rest. Whether a syntax allows that,
// and how each document keeps its own bytes, is the codec's business.
// splitDocuments reads the manifests in one file through the codec,
// which decides whether a file can hold several.
func (e *Engine) splitDocuments(raw []byte) ([]yamlDocument, error) {
	docs, err := e.codec.DecodeAll(raw)
	if err != nil {
		return nil, err
	}
	out := make([]yamlDocument, 0, len(docs))
	for _, d := range docs {
		out = append(out, yamlDocument{doc: d.Doc, yaml: d.Text})
	}
	return out, nil
}
