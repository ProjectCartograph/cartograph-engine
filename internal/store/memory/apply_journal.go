package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// ApplyJournal is the in-memory adapter for store.ApplyJournal.
type ApplyJournal struct {
	mu    sync.RWMutex
	units map[string]store.ApplyUnit
}

// NewApplyJournal creates a new in-memory ApplyJournal.
func NewApplyJournal() *ApplyJournal {
	return &ApplyJournal{
		units: make(map[string]store.ApplyUnit),
	}
}

// PutUnit writes a new unit with applied=false.
func (j *ApplyJournal) PutUnit(_ context.Context, unit store.ApplyUnit) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, exists := j.units[unit.ID]; exists {
		return fmt.Errorf("unit already exists: %s", unit.ID)
	}
	unit.Applied = false
	j.units[unit.ID] = unit
	return nil
}

// MarkApplied marks a unit as applied.
func (j *ApplyJournal) MarkApplied(_ context.Context, unitID string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	unit, ok := j.units[unitID]
	if !ok {
		return fmt.Errorf("unit not found: %s", unitID)
	}
	unit.Applied = true
	j.units[unitID] = unit
	return nil
}

// ListUnapplied returns every unit with applied=false, oldest first.
func (j *ApplyJournal) ListUnapplied(_ context.Context) ([]store.ApplyUnit, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	var units []store.ApplyUnit
	for _, u := range j.units {
		if !u.Applied {
			units = append(units, u)
		}
	}

	// Sort by On time (oldest first)
	for i := 0; i < len(units); i++ {
		for j := i + 1; j < len(units); j++ {
			if units[j].On.Before(units[i].On) {
				units[i], units[j] = units[j], units[i]
			}
		}
	}

	return units, nil
}

// PruneOld removes applied units older than keepCount (keeps the most recent).
// A keepCount of 0 or less means unlimited (no pruning).
func (j *ApplyJournal) PruneOld(_ context.Context, keepCount int) error {
	if keepCount <= 0 {
		return nil // No pruning
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	// Collect all applied units, sorted by time
	var applied []store.ApplyUnit
	for _, u := range j.units {
		if u.Applied {
			applied = append(applied, u)
		}
	}

	// Sort by time (newest first)
	for i := 0; i < len(applied); i++ {
		for k := i + 1; k < len(applied); k++ {
			if applied[k].On.After(applied[i].On) {
				applied[i], applied[k] = applied[k], applied[i]
			}
		}
	}

	// Keep only the most recent keepCount units
	if len(applied) > keepCount {
		for i := keepCount; i < len(applied); i++ {
			delete(j.units, applied[i].ID)
		}
	}

	return nil
}

// WithinTransaction runs fn. Memory adapter doesn't need true transactions.
func (j *ApplyJournal) WithinTransaction(ctx context.Context, fn func(ctx context.Context, tx store.ApplyJournal) error) error {
	return fn(ctx, j)
}
