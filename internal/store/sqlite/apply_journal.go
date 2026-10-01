package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
)

// ApplyJournal is the SQLite adapter for store.ApplyJournal.
// It wraps both *sql.DB and *sql.Tx through the executor interface.
type ApplyJournal struct {
	ex executor
}

// executor is an interface for methods common to both *sql.DB and *sql.Tx
type executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// NewApplyJournal creates a new ApplyJournal over the given database.
func NewApplyJournal(db *sql.DB) *ApplyJournal {
	return &ApplyJournal{ex: db}
}

// PutUnit writes a new unit with applied=false.
func (j *ApplyJournal) PutUnit(ctx context.Context, unit store.ApplyUnit) error {
	filesJSON, err := json.Marshal(unit.Files)
	if err != nil {
		return fmt.Errorf("marshal files: %w", err)
	}

	query := `INSERT INTO apply_journal (id, "on", operator, reason, files, applied)
	          VALUES (?, ?, ?, ?, ?, 0)`
	_, err = j.ex.ExecContext(ctx, query,
		unit.ID, unit.On.Format(time.RFC3339Nano), unit.Operator, unit.Reason, string(filesJSON))
	return err
}

// MarkApplied marks a unit as applied.
func (j *ApplyJournal) MarkApplied(ctx context.Context, unitID string) error {
	query := `UPDATE apply_journal SET applied = 1 WHERE id = ?`
	_, err := j.ex.ExecContext(ctx, query, unitID)
	return err
}

// ListUnapplied returns every unit with applied=false, oldest first.
func (j *ApplyJournal) ListUnapplied(ctx context.Context) ([]store.ApplyUnit, error) {
	query := `SELECT id, "on", operator, reason, files FROM apply_journal
	          WHERE applied = 0 ORDER BY "on" ASC`
	rows, err := j.ex.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var units []store.ApplyUnit
	for rows.Next() {
		var unit store.ApplyUnit
		var onStr string
		var filesJSON string

		err := rows.Scan(&unit.ID, &onStr, &unit.Operator, &unit.Reason, &filesJSON)
		if err != nil {
			return nil, err
		}

		unit.On, err = time.Parse(time.RFC3339Nano, onStr)
		if err != nil {
			return nil, err
		}

		err = json.Unmarshal([]byte(filesJSON), &unit.Files)
		if err != nil {
			return nil, err
		}

		units = append(units, unit)
	}
	return units, rows.Err()
}

// PruneOld removes applied units older than keepCount (keeps the most recent).
// A keepCount of 0 or less means unlimited (no pruning).
func (j *ApplyJournal) PruneOld(ctx context.Context, keepCount int) error {
	if keepCount <= 0 {
		return nil // No pruning
	}

	// Get the ID of the keepCount-th most recent applied unit (the cutoff)
	query := `SELECT id FROM apply_journal
	          WHERE applied = 1
	          ORDER BY "on" DESC
	          LIMIT 1 OFFSET ?`
	var cutoffID string
	err := j.ex.QueryRowContext(ctx, query, keepCount-1).Scan(&cutoffID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil // Not enough units to prune
		}
		return err
	}

	// Delete all applied units older than the cutoff
	deleteQuery := `DELETE FROM apply_journal
	                WHERE applied = 1 AND "on" <= (
	                  SELECT "on" FROM apply_journal WHERE id = ?
	                )`
	_, err = j.ex.ExecContext(ctx, deleteQuery, cutoffID)
	return err
}

// WithinTransaction runs fn in a transaction.
func (j *ApplyJournal) WithinTransaction(ctx context.Context, fn func(ctx context.Context, tx store.ApplyJournal) error) error {
	// If already in a transaction, just call fn directly
	if _, ok := j.ex.(*sql.Tx); ok {
		return fn(ctx, j)
	}

	db, ok := j.ex.(*sql.DB)
	if !ok {
		return fmt.Errorf("unable to start transaction: not a database connection")
	}

	txn, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	txJournal := &ApplyJournal{ex: txn}
	if err := fn(ctx, txJournal); err != nil {
		txn.Rollback()
		return err
	}
	return txn.Commit()
}
