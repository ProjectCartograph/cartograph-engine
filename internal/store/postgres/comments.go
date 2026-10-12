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

var _ store.CommentStore = (*ManifestStore)(nil)

const commentColumns = `id, kind, manifest_id, anchor, reply, body, mentions, author, at, resolved, resolved_by, resolved_at`

func scanComment(r pgx.Row) (store.Comment, error) {
	var c store.Comment
	var mentions string
	var resolvedAt *time.Time
	if err := r.Scan(&c.ID, &c.Kind, &c.Manifest, &c.Anchor, &c.Reply, &c.Body, &mentions, &c.Author, &c.At, &c.Resolved, &c.ResolvedBy, &resolvedAt); err != nil {
		return store.Comment{}, err
	}
	c.At = c.At.UTC()
	if resolvedAt != nil {
		c.ResolvedAt = resolvedAt.UTC()
	}
	if mentions != "" {
		if err := json.Unmarshal([]byte(mentions), &c.Mentions); err != nil {
			return store.Comment{}, fmt.Errorf("comment %s mentions: %w", c.ID, err)
		}
	}
	return c, nil
}

// PutComment keeps a comment.
func (m *ManifestStore) PutComment(ctx context.Context, c store.Comment) error {
	mentions, err := json.Marshal(c.Mentions)
	if err != nil {
		return err
	}
	if c.Mentions == nil {
		mentions = []byte("[]")
	}
	_, err = m.q.Exec(ctx, `INSERT INTO comments (`+commentColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET anchor = EXCLUDED.anchor, reply = EXCLUDED.reply, body = EXCLUDED.body,
		mentions = EXCLUDED.mentions, resolved = EXCLUDED.resolved, resolved_by = EXCLUDED.resolved_by,
		resolved_at = EXCLUDED.resolved_at`,
		c.ID, c.Kind, c.Manifest, c.Anchor, c.Reply, c.Body, string(mentions), c.Author, c.At.UTC(), c.Resolved, c.ResolvedBy, nullTime(c.ResolvedAt))
	if err != nil {
		return fmt.Errorf("put comment: %w", err)
	}
	return nil
}

// GetComment returns one comment.
func (m *ManifestStore) GetComment(ctx context.Context, id string) (store.Comment, error) {
	c, err := scanComment(m.q.QueryRow(ctx, `SELECT `+commentColumns+` FROM comments WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Comment{}, store.ErrNoComment
	}
	return c, err
}

// ListComments returns a record's comments, oldest first.
func (m *ManifestStore) ListComments(ctx context.Context, kind, manifest string) ([]store.Comment, error) {
	return m.comments(ctx, `SELECT `+commentColumns+` FROM comments WHERE kind = $1 AND manifest_id = $2 ORDER BY at, id`, kind, manifest)
}

// ListMentioning returns the comments that mention any of the parties,
// newest first: one containment test per party, which the GIN index on
// mentions answers.
func (m *ManifestStore) ListMentioning(ctx context.Context, mentions []store.Mention, limit int) ([]store.Comment, error) {
	if len(mentions) == 0 {
		return []store.Comment{}, nil
	}
	var ors []string
	var args []any
	for i, x := range mentions {
		one, err := json.Marshal([]store.Mention{x})
		if err != nil {
			return nil, err
		}
		ors = append(ors, fmt.Sprintf("mentions @> $%d::jsonb", i+1))
		args = append(args, string(one))
	}
	q := `SELECT ` + commentColumns + ` FROM comments WHERE ` + strings.Join(ors, " OR ") + ` ORDER BY at DESC, id DESC`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	return m.comments(ctx, q, args...)
}

func (m *ManifestStore) comments(ctx context.Context, q string, args ...any) ([]store.Comment, error) {
	rows, err := m.q.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
