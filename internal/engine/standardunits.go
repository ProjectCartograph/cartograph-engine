package engine

import (
	"context"
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/units"
)

// SeedStandardUnits writes the standard units into a store that holds no
// Unit at all, and reports which it wrote.
//
// Guarded twice, and the second guard is the one that matters. The first
// is "none at all" rather than each one, so a vault that has dropped a
// unit it does not want keeps it dropped. The second refuses to write
// over a manifest that already exists, because the first guard reads a
// listing, and a listing is empty for two different reasons: the vault
// holds no units, or the vault's index has not been built yet. Opening
// a vault without its (generated, uncommitted) index once took the second
// for the first and rewrote every one of its unit files, reordering their
// keys and dropping their quoting, on a read-only visit.
//
// Writes through the ordinary commit path, so a seeded unit is a manifest
// like any other and can be edited, renamed or deleted.
func (e *Engine) SeedStandardUnits(ctx context.Context) ([]string, error) {
	if err := refuseAgent(ctx); err != nil {
		return nil, err
	}
	existing, err := e.manifests.ListSummaries(ctx, "Unit", "", nil)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, nil
	}
	var wrote []string
	for _, u := range units.Standard {
		// A file that is already there is somebody's, whatever a listing
		// says.
		if _, err := e.Get(ctx, "Unit", u.ID); err == nil {
			continue
		}
		doc := map[string]any{
			"apiVersion": "cartograph/v1",
			"kind":       "Unit",
			"metadata":   map[string]any{"id": u.ID, "name": u.Name},
			"spec": func() map[string]any {
				spec := map[string]any{"dimension": u.Dimension}
				if u.Symbol != "" {
					spec["symbol"] = u.Symbol
				}
				if u.Description != "" {
					spec["description"] = u.Description
				}
				return spec
			}(),
		}
		body, err := e.codec.Encode(doc)
		if err != nil {
			return wrote, err
		}
		if _, err := e.Commit(ctx, "Unit", u.ID, body, "cartograph", "standard unit"); err != nil {
			return wrote, fmt.Errorf("seed unit %s: %w", u.ID, err)
		}
		wrote = append(wrote, u.ID)
	}
	return wrote, nil
}

// StandardUnitIDs names the units a vault is seeded with, so a test can
// hold the example instance to the same set.
func StandardUnitIDs() []string {
	out := make([]string, len(units.Standard))
	for i, u := range units.Standard {
		out[i] = u.ID
	}
	return out
}
