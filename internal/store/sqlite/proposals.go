package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var _ store.ProposalStore = (*ManifestStore)(nil)

const proposalColumns = `id, kind, manifest_id, op, text, series, item, state, base, reason, agent, for_person, at, status, decided_by, decided_at, decision_reason, version, waivers, set_id, set_index`

type rowScanner interface{ Scan(...any) error }

func scanProposal(r rowScanner) (store.Proposal, error) {
	var p store.Proposal
	var text, item sql.NullString
	var at, decided, waivers string
	if err := r.Scan(&p.ID, &p.Kind, &p.ManifestID, &p.Op, &text, &p.Series, &item, &p.State, &p.Base, &p.Reason,
		&p.Agent, &p.For, &at, &p.Status, &p.DecidedBy, &decided, &p.DecisionReason, &p.Version, &waivers, &p.Set, &p.SetIndex); err != nil {
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
	if waivers != "" {
		if err := json.Unmarshal([]byte(waivers), &p.Waivers); err != nil {
			return store.Proposal{}, fmt.Errorf("proposal %s waivers: %w", p.ID, err)
		}
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
	waivers, err := json.Marshal(p.Waivers)
	if err != nil {
		return err
	}
	if p.Waivers == nil {
		waivers = []byte("[]")
	}
	_, err = m.ex.ExecContext(ctx, `INSERT INTO proposals (id, kind, manifest_id, op, text, series, item, state, base, reason, agent, for_person, at, status, waivers, set_id, set_index)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Kind, p.ManifestID, p.Op, text, p.Series, item, p.State, p.Base, p.Reason, p.Agent, p.For, p.At.UTC().Format(seriesLayout), p.Status, string(waivers), p.Set, p.SetIndex)
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
	for _, c := range []struct{ col, val string }{{"for_person", f.For}, {"kind", f.Kind}, {"manifest_id", f.ManifestID}, {"status", f.Status}, {"set_id", f.Set}} {
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

// DecideProposalSet decides every proposal of a set in one transaction:
// all of them, or none when any is decided already.
func (m *ManifestStore) DecideProposalSet(ctx context.Context, set, status, by, reason string, at time.Time, versions map[string]int) ([]store.Proposal, error) {
	decide := func(s *ManifestStore) ([]store.Proposal, error) {
		members, err := s.ListProposals(ctx, store.ProposalFilter{Set: set})
		if err != nil {
			return nil, err
		}
		if len(members) == 0 || set == "" {
			return nil, store.ErrNoProposal
		}
		for _, p := range members {
			if p.Status != store.ProposalOpen {
				return nil, store.ErrProposalDecided
			}
		}
		var out []store.Proposal
		for _, p := range members {
			d, err := s.DecideProposal(ctx, p.ID, status, by, reason, at, versions[p.ID])
			if err != nil {
				return nil, err
			}
			out = append(out, d)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].SetIndex < out[j].SetIndex })
		return out, nil
	}
	if m.db == nil {
		return decide(m)
	}
	var out []store.Proposal
	err := m.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		var err error
		out, err = decide(tx.(*ManifestStore))
		return err
	})
	return out, err
}
