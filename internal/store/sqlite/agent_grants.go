package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

const agentGrantColumns = `id, email, label, created_at, expires_at, last_used, revoked_at, generation`

func scanAgentGrant(r rowScanner) (store.AgentGrant, error) {
	var t store.AgentGrant
	var created, expires, used, revoked string
	if err := r.Scan(&t.ID, &t.Email, &t.Label, &created, &expires, &used, &revoked, &t.Generation); err != nil {
		return store.AgentGrant{}, err
	}
	t.CreatedAt, _ = time.Parse(seriesLayout, created)
	t.ExpiresAt, _ = time.Parse(seriesLayout, expires)
	if used != "" {
		t.LastUsed, _ = time.Parse(seriesLayout, used)
	}
	if revoked != "" {
		t.RevokedAt, _ = time.Parse(seriesLayout, revoked)
	}
	return t, nil
}

func fixed(t time.Time) string { return t.UTC().Format(seriesLayout) }

func (s *AccessStore) PutAgentGrant(ctx context.Context, t store.AgentGrant) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_grants (id, email, label, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.Email, t.Label, fixed(t.CreatedAt), fixed(t.ExpiresAt))
	return err
}

func (s *AccessStore) GetAgentGrant(ctx context.Context, id string) (store.AgentGrant, error) {
	t, err := scanAgentGrant(s.db.QueryRowContext(ctx, `SELECT `+agentGrantColumns+` FROM agent_grants WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return store.AgentGrant{}, store.ErrNoAgentGrant
	}
	return t, err
}

func (s *AccessStore) ListAgentGrants(ctx context.Context, email string) ([]store.AgentGrant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentGrantColumns+` FROM agent_grants WHERE ? = '' OR email = ? ORDER BY created_at DESC, id DESC`, email, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.AgentGrant{}
	for rows.Next() {
		t, err := scanAgentGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *AccessStore) RevokeAgentGrant(ctx context.Context, id string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE agent_grants SET revoked_at = CASE WHEN revoked_at = '' THEN ? ELSE revoked_at END WHERE id = ?`, fixed(at), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNoAgentGrant
	}
	return nil
}

func (s *AccessStore) TouchAgentGrant(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_grants SET last_used = ? WHERE id = ?`, fixed(at), id)
	return err
}

func (s *AccessStore) RotateAgentGrant(ctx context.Context, id string, from int) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE agent_grants SET generation = generation + 1 WHERE id = ? AND generation = ?`, id, from)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
