package engine

import (
	"errors"
	"fmt"
)

// Sentinel errors an API or CLI layer can classify with errors.Is.
var (
	ErrUnknownKind = errors.New("unknown kind")
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("conflict")
)

// ValidationError carries the problems found by schema, reference or kind
// rule validation. Commit and Propose return it (wrapped) instead of a
// separate problems slice, since their public signatures return only a
// value and an error; an API handler recovers it with errors.As and
// answers 422 with {problems: [...]}.
type ValidationError struct {
	Problems []Problem
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 1 {
		return fmt.Sprintf("validation failed: %s: %s", e.Problems[0].Path, e.Problems[0].Message)
	}
	return fmt.Sprintf("validation failed: %d problems", len(e.Problems))
}
