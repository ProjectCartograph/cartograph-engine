package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

const agentGrantColumns = `id, email, label, created_at, expires_at, last_used, revoked_at, generation`

func scanAgentGrant(r pgx.Row) (store.AgentGrant, error) {
	var t store.AgentGrant
	var used, revoked *time.Time
	if err := r.Scan(&t.ID, &t.Email, &t.Label, &t.CreatedAt, &t.ExpiresAt, &used, &revoked, &t.Generation); err != nil {
		return store.AgentGrant{}, err
	}
	t.CreatedAt, t.ExpiresAt = t.CreatedAt.UTC(), t.ExpiresAt.UTC()
	if used != nil {
		t.LastUsed = used.UTC()
	}
	if revoked != nil {
		t.RevokedAt = revoked.UTC()
	}
	return t, nil
}

func (s *AccessStore) PutAgentGrant(ctx context.Context, t store.AgentGrant) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO agent_grants (id, email, label, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		t.ID, t.Email, t.Label, stamp(t.CreatedAt), stamp(t.ExpiresAt))
	if err != nil {
		return fmt.Errorf("put agent grant: %w", err)
	}
	return nil
}

func (s *AccessStore) GetAgentGrant(ctx context.Context, id string) (store.AgentGrant, error) {
	t, err := scanAgentGrant(s.pool.QueryRow(ctx, `SELECT `+agentGrantColumns+` FROM agent_grants WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.AgentGrant{}, store.ErrNoAgentGrant
	}
	return t, err
}

func (s *AccessStore) ListAgentGrants(ctx context.Context, email string) ([]store.AgentGrant, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+agentGrantColumns+` FROM agent_grants WHERE $1 = '' OR email = $1 ORDER BY created_at DESC, id DESC`, email)
	if err != nil {
		return nil, fmt.Errorf("list agent grants: %w", err)
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
	tag, err := s.pool.Exec(ctx, `UPDATE agent_grants SET revoked_at = coalesce(revoked_at, $2) WHERE id = $1`, id, stamp(at))
	if err != nil {
		return fmt.Errorf("revoke agent grant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNoAgentGrant
	}
	return nil
}

func (s *AccessStore) TouchAgentGrant(ctx context.Context, id string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE agent_grants SET last_used = $2 WHERE id = $1`, id, stamp(at))
	return err
}

func (s *AccessStore) RotateAgentGrant(ctx context.Context, id string, from int) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE agent_grants SET generation = generation + 1 WHERE id = $1 AND generation = $2`, id, from)
	if err != nil {
		return false, fmt.Errorf("rotate agent grant: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
