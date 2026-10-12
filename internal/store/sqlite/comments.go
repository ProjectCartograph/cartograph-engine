package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var _ store.CommentStore = (*ManifestStore)(nil)

const commentColumns = `id, kind, manifest_id, anchor, reply, body, mentions, author, at, resolved, resolved_by, resolved_at`

func scanComment(r rowScanner) (store.Comment, error) {
	var c store.Comment
	var mentions, at, resolvedAt string
	var resolved int
	if err := r.Scan(&c.ID, &c.Kind, &c.Manifest, &c.Anchor, &c.Reply, &c.Body, &mentions, &c.Author, &at, &resolved, &c.ResolvedBy, &resolvedAt); err != nil {
		return store.Comment{}, err
	}
	c.At, _ = unstamp(at)
	c.ResolvedAt, _ = unstamp(resolvedAt)
	c.Resolved = resolved != 0
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
	resolved := 0
	if c.Resolved {
		resolved = 1
	}
	_, err = m.ex.ExecContext(ctx, `INSERT INTO comments (`+commentColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (id) DO UPDATE SET anchor = excluded.anchor, reply = excluded.reply, body = excluded.body,
		mentions = excluded.mentions, resolved = excluded.resolved, resolved_by = excluded.resolved_by,
		resolved_at = excluded.resolved_at`,
		c.ID, c.Kind, c.Manifest, c.Anchor, c.Reply, c.Body, string(mentions), c.Author, stamp(c.At), resolved, c.ResolvedBy, stamp(c.ResolvedAt))
	return err
}

// GetComment returns one comment.
func (m *ManifestStore) GetComment(ctx context.Context, id string) (store.Comment, error) {
	c, err := scanComment(m.ex.QueryRowContext(ctx, `SELECT `+commentColumns+` FROM comments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return store.Comment{}, store.ErrNoComment
	}
	return c, err
}

// ListComments returns a record's comments, oldest first.
func (m *ManifestStore) ListComments(ctx context.Context, kind, manifest string) ([]store.Comment, error) {
	return m.comments(ctx, `SELECT `+commentColumns+` FROM comments WHERE kind = ? AND manifest_id = ? ORDER BY at, id`, kind, manifest)
}

// ListMentioning returns the comments that mention any of the parties,
// newest first.
func (m *ManifestStore) ListMentioning(ctx context.Context, mentions []store.Mention, limit int) ([]store.Comment, error) {
	if len(mentions) == 0 {
		return []store.Comment{}, nil
	}
	var ors []string
	var args []any
	for _, x := range mentions {
		ors = append(ors, `(json_extract(value, '$.kind') = ? AND json_extract(value, '$.id') = ?)`)
		args = append(args, x.Kind, x.ID)
	}
	q := `SELECT ` + commentColumns + ` FROM comments WHERE EXISTS (SELECT 1 FROM json_each(comments.mentions) WHERE ` +
		strings.Join(ors, " OR ") + `) ORDER BY at DESC, id DESC`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	return m.comments(ctx, q, args...)
}

func (m *ManifestStore) comments(ctx context.Context, q string, args ...any) ([]store.Comment, error) {
	rows, err := m.ex.QueryContext(ctx, q, args...)
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
