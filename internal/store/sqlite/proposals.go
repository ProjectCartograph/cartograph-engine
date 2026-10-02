package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var _ store.ProposalStore = (*ManifestStore)(nil)

const proposalColumns = `id, kind, manifest_id, op, text, series, item, state, base, reason, agent, for_person, at, status, decided_by, decided_at, decision_reason, version`

type rowScanner interface{ Scan(...any) error }

func scanProposal(r rowScanner) (store.Proposal, error) {
	var p store.Proposal
	var text, item sql.NullString
	var at, decided string
	if err := r.Scan(&p.ID, &p.Kind, &p.ManifestID, &p.Op, &text, &p.Series, &item, &p.State, &p.Base, &p.Reason,
		&p.Agent, &p.For, &at, &p.Status, &p.DecidedBy, &decided, &p.DecisionReason, &p.Version); err != nil {
		return store.Proposal{}, err
	}
	if text.Valid {
		p.Text = []byte(text.String)
	}
	if item.Valid {
		p.Item = []byte(item.String)
	}
	p.At, _ = time.Parse(seriesLayout, at)
	if decided != "" {
		p.DecidedAt, _ = time.Parse(seriesLayout, decided)
	}
	return p, nil
}

// PutProposal keeps a proposal.
func (m *ManifestStore) PutProposal(ctx context.Context, p store.Proposal) error {
	var text, item any
	if p.Text != nil {
		text = string(p.Text)
	}
	if p.Item != nil {
		item = string(p.Item)
	}
	_, err := m.ex.ExecContext(ctx, `INSERT INTO proposals (id, kind, manifest_id, op, text, series, item, state, base, reason, agent, for_person, at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Kind, p.ManifestID, p.Op, text, p.Series, item, p.State, p.Base, p.Reason, p.Agent, p.For, p.At.UTC().Format(seriesLayout), p.Status)
	return err
}

// GetProposal returns one proposal.
func (m *ManifestStore) GetProposal(ctx context.Context, id string) (store.Proposal, error) {
	p, err := scanProposal(m.ex.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return store.Proposal{}, store.ErrNoProposal
	}
	return p, err
}

// ListProposals returns proposals, newest first.
func (m *ManifestStore) ListProposals(ctx context.Context, f store.ProposalFilter) ([]store.Proposal, error) {
	var where []string
	var args []any
	for _, c := range []struct{ col, val string }{{"for_person", f.For}, {"kind", f.Kind}, {"manifest_id", f.ManifestID}, {"status", f.Status}} {
		if c.val != "" {
			where = append(where, c.col+" = ?")
			args = append(args, c.val)
		}
	}
	q := `SELECT ` + proposalColumns + ` FROM proposals`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	rows, err := m.ex.QueryContext(ctx, q+` ORDER BY at DESC, id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	defer rows.Close()
	out := []store.Proposal{}
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DecideProposal decides an open proposal, in one statement.
func (m *ManifestStore) DecideProposal(ctx context.Context, id, status, by, reason string, at time.Time, version int) (store.Proposal, error) {
	p, err := scanProposal(m.ex.QueryRowContext(ctx, `UPDATE proposals SET status = ?, decided_by = ?, decision_reason = ?, decided_at = ?, version = ?
		WHERE id = ? AND status = 'open' RETURNING `+proposalColumns, status, by, reason, at.UTC().Format(seriesLayout), version, id))
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := m.GetProposal(ctx, id); err != nil {
			return store.Proposal{}, err
		}
		return store.Proposal{}, store.ErrProposalDecided
	}
	return p, err
}
