package engine

import (
	"context"
	"fmt"
)

// The units every vault starts with.
//
// "Sensible defaults with an open extension" is the whole shape of D10: a
// closed enum would refuse whatever an organisation measures that nobody
// anticipated, and sending that number into a note is worse than a list
// somebody can add to. So these are ordinary manifests, written into a
// vault that holds none, and an instance declares its own beside them
// with the picker's own add. There is no second code path for a custom
// unit, which is the point.
//
// Only when a vault has no Unit at all. Deleting one does not bring it
// back: these are a starting point, not a policy.
//
// Money is deliberately absent. A money unit is a currency, the funding
// line already picks from the ISO 4217 list, and seeding one currency
// would be Cartograph guessing which country it is in.
var standardUnits = []struct {
	ID, Name, Symbol, Dimension, Description string
}{
	{"percent", "Percent", "%", "percent",
		"A share of a whole, out of a hundred."},
	{"count", "Count", "", "count",
		"A plain number of things. Say what is being counted in the indicator's own definition."},
	{"ratio", "Ratio", "", "ratio",
		"One quantity divided by another, written as a single number."},
	{"score", "Score", "", "ratio",
		"A number on a scale somebody defined. The indicator's definition says what the scale is."},
	{"index", "Index", "", "ratio",
		"A number set against a base period, so movement rather than level is what it shows."},
	{"days", "Days", "d", "duration", "Whole days."},
	{"hours", "Hours", "hrs", "duration", "Hours, to the nearest hour unless the indicator says otherwise."},
	{"months", "Months", "mo", "duration", "Whole months."},
}

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
	for _, u := range standardUnits {
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
	out := make([]string, len(standardUnits))
	for i, u := range standardUnits {
		out[i] = u.ID
	}
	return out
}
