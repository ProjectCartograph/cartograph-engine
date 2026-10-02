package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var _ store.ProposalStore = (*ManifestStore)(nil)

const proposalColumns = `id, kind, manifest_id, op, text, series, item, state, base, reason, agent, for_person, at, status, decided_by, decided_at, decision_reason, version, waivers, set_id, set_index`

func scanProposal(r pgx.Row) (store.Proposal, error) {
	var p store.Proposal
	var text *string
	var decided *time.Time
	var waivers string
	if err := r.Scan(&p.ID, &p.Kind, &p.ManifestID, &p.Op, &text, &p.Series, &p.Item, &p.State, &p.Base, &p.Reason,
		&p.Agent, &p.For, &p.At, &p.Status, &p.DecidedBy, &decided, &p.DecisionReason, &p.Version, &waivers, &p.Set, &p.SetIndex); err != nil {
		return store.Proposal{}, err
	}
	if text != nil {
		p.Text = []byte(*text)
	}
	if decided != nil {
		p.DecidedAt = decided.UTC()
	}
	p.At = p.At.UTC()
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
	_, err = m.q.Exec(ctx, `INSERT INTO proposals (id, kind, manifest_id, op, text, series, item, state, base, reason, agent, for_person, at, status, waivers, set_id, set_index)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		p.ID, p.Kind, p.ManifestID, p.Op, text, p.Series, item, p.State, p.Base, p.Reason, p.Agent, p.For, stamp(p.At), p.Status, string(waivers), p.Set, p.SetIndex)
	if err != nil {
		return fmt.Errorf("put proposal: %w", err)
	}
	return nil
}

// GetProposal returns one proposal.
func (m *ManifestStore) GetProposal(ctx context.Context, id string) (store.Proposal, error) {
	p, err := scanProposal(m.q.QueryRow(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Proposal{}, store.ErrNoProposal
	}
	return p, err
}

// ListProposals returns proposals, newest first, through the indexes on
// the person and on the manifest.
func (m *ManifestStore) ListProposals(ctx context.Context, f store.ProposalFilter) ([]store.Proposal, error) {
	var where []string
	var args []any
	for _, c := range []struct{ col, val string }{{"for_person", f.For}, {"kind", f.Kind}, {"manifest_id", f.ManifestID}, {"status", f.Status}, {"set_id", f.Set}} {
		if c.val != "" {
			args = append(args, c.val)
			where = append(where, fmt.Sprintf("%s = $%d", c.col, len(args)))
		}
	}
	q := `SELECT ` + proposalColumns + ` FROM proposals`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	rows, err := m.q.Query(ctx, q+` ORDER BY at DESC, id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	defer rows.Close()
	out := []store.Proposal{}
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, fmt.Errorf("list proposals: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DecideProposal decides an open proposal in one statement: of two
// decisions at once, one updates the row and the other finds it decided.
func (m *ManifestStore) DecideProposal(ctx context.Context, id, status, by, reason string, at time.Time, version int) (store.Proposal, error) {
	p, err := scanProposal(m.q.QueryRow(ctx, `UPDATE proposals SET status = $2, decided_by = $3, decision_reason = $4, decided_at = $5, version = $6
		WHERE id = $1 AND status = 'open' RETURNING `+proposalColumns, id, status, by, reason, stamp(at), version))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := m.GetProposal(ctx, id); err != nil {
			return store.Proposal{}, err
		}
		return store.Proposal{}, store.ErrProposalDecided
	}
	return p, err
}

// DecideProposalSet decides every proposal of a set in one transaction,
// the rows locked first: of two decisions at once, one decides them all
// and the other finds them decided.
func (m *ManifestStore) DecideProposalSet(ctx context.Context, set, status, by, reason string, at time.Time, versions map[string]int) ([]store.Proposal, error) {
	var out []store.Proposal
	err := m.inTx(ctx, func(q querier) error {
		rows, err := q.Query(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE set_id = $1 ORDER BY set_index FOR UPDATE`, set)
		if err != nil {
			return err
		}
		var members []store.Proposal
		for rows.Next() {
			p, err := scanProposal(rows)
			if err != nil {
				rows.Close()
				return err
			}
			members = append(members, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(members) == 0 || set == "" {
			return store.ErrNoProposal
		}
		for _, p := range members {
			if p.Status != store.ProposalOpen {
				return store.ErrProposalDecided
			}
		}
		for _, p := range members {
			d, err := scanProposal(q.QueryRow(ctx, `UPDATE proposals SET status = $2, decided_by = $3, decision_reason = $4, decided_at = $5, version = $6
				WHERE id = $1 AND status = 'open' RETURNING `+proposalColumns, p.ID, status, by, reason, stamp(at), versions[p.ID]))
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return nil
	})
	return out, err
}
