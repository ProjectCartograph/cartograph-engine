package engine

import (
	"context"
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// CommitProject validates and stores a new version of a Project, exactly
// like the generic Commit.
func (e *Engine) CommitProject(ctx context.Context, id string, yamlBytes []byte, actor, reason string) (Version, error) {
	doc, problems, err := e.validate(ctx, "Project", yamlBytes, nil)
	if err != nil {
		return Version{}, err
	}
	if metaID, ok := docID(doc); !ok || metaID != id {
		return Version{}, fmt.Errorf("%w: manifest metadata.id %q does not match %q", ErrConflict, metaID, id)
	}
	if p, err := e.checkActor(ctx, actor); err != nil {
		return Version{}, err
	} else if p != nil {
		problems = append(problems, *p)
	}
	if len(problems) > 0 {
		return Version{}, &ValidationError{Problems: problems}
	}
	if err := e.guardDoc(ctx, "Project", id, doc); err != nil {
		return Version{}, err
	}

	current, found, err := e.manifests.GetCurrent(ctx, "Project", id)
	if err != nil {
		return Version{}, err
	}
	number := 1
	if found {
		number = current.Number + 1
	}
	v := Version{Kind: "Project", ID: id, Number: number, YAML: yamlBytes, Actor: actor, Reason: reason, On: timeNow().UTC()}
	refs := extractRefs(doc, e.refRules["Project"])
	storeRefs := toStoreRefs(refs)

	err = e.manifests.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		if err := tx.PutVersion(ctx, v); err != nil {
			return err
		}
		if err := tx.IndexReferences(ctx, "Project", id, storeRefs); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Version{}, err
	}
	e.afterVersion(ctx, v)
	return v, nil
}

func toStoreRefs(refs []foundRef) []store.Ref {
	out := make([]store.Ref, len(refs))
	for i, r := range refs {
		out[i] = store.Ref{Path: r.path, ToKind: r.kind, ToID: r.id}
	}
	return out
}
