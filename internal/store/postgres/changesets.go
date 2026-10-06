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

var _ store.ChangeSetStore = (*ManifestStore)(nil)

const changeSetColumns = `id, title, description, owner, agent, for_person, status, reason, waivers, at, updated, decided_by, decided_at, decision_reason`

func scanChangeSet(r pgx.Row) (store.ChangeSet, error) {
	var cs store.ChangeSet
	var decided *time.Time
	var waivers string
	if err := r.Scan(&cs.ID, &cs.Title, &cs.Description, &cs.Owner, &cs.Agent, &cs.For, &cs.Status, &cs.Reason, &waivers,
		&cs.At, &cs.Updated, &cs.DecidedBy, &decided, &cs.DecisionReason); err != nil {
		return store.ChangeSet{}, err
	}
	cs.At, cs.Updated = cs.At.UTC(), cs.Updated.UTC()
	if decided != nil {
		cs.DecidedAt = decided.UTC()
	}
	if waivers != "" {
		if err := json.Unmarshal([]byte(waivers), &cs.Waivers); err != nil {
			return store.ChangeSet{}, fmt.Errorf("change set %s waivers: %w", cs.ID, err)
		}
	}
	return cs, nil
}

// nullTime is a time the table keeps, or NULL for the zero time.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// PutChangeSet keeps a change set.
func (m *ManifestStore) PutChangeSet(ctx context.Context, cs store.ChangeSet) error {
	waivers, err := json.Marshal(cs.Waivers)
	if err != nil {
		return err
	}
	if cs.Waivers == nil {
		waivers = []byte("[]")
	}
	_, err = m.q.Exec(ctx, `INSERT INTO change_sets (`+changeSetColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, description = EXCLUDED.description, owner = EXCLUDED.owner,
		agent = EXCLUDED.agent, for_person = EXCLUDED.for_person, status = EXCLUDED.status, reason = EXCLUDED.reason,
		waivers = EXCLUDED.waivers, updated = EXCLUDED.updated, decided_by = EXCLUDED.decided_by,
		decided_at = EXCLUDED.decided_at, decision_reason = EXCLUDED.decision_reason`,
		cs.ID, cs.Title, cs.Description, cs.Owner, cs.Agent, cs.For, cs.Status, cs.Reason, string(waivers),
		cs.At.UTC(), cs.Updated.UTC(), cs.DecidedBy, nullTime(cs.DecidedAt), cs.DecisionReason)
	if err != nil {
		return fmt.Errorf("put change set: %w", err)
	}
	return nil
}

// GetChangeSet returns one change set.
func (m *ManifestStore) GetChangeSet(ctx context.Context, id string) (store.ChangeSet, error) {
	cs, err := scanChangeSet(m.q.QueryRow(ctx, `SELECT `+changeSetColumns+` FROM change_sets WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ChangeSet{}, store.ErrNoChangeSet
	}
	return cs, err
}

// ListChangeSets returns change sets, newest first, through the indexes
// on the person and on the owner.
func (m *ManifestStore) ListChangeSets(ctx context.Context, f store.ChangeSetFilter) ([]store.ChangeSet, error) {
	q := `SELECT ` + changeSetColumns + ` FROM change_sets`
	var where []string
	var args []any
	for _, c := range []struct{ col, val string }{{"for_person", f.For}, {"owner", f.Owner}, {"status", f.Status}} {
		if c.val != "" {
			args = append(args, c.val)
			where = append(where, fmt.Sprintf("%s = $%d", c.col, len(args)))
		}
	}
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	rows, err := m.q.Query(ctx, q+` ORDER BY at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.ChangeSet{}
	for rows.Next() {
		cs, err := scanChangeSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cs)
	}
	return out, rows.Err()
}

// MoveChangeSet moves a change set from one status to another, in one
// statement, so of two moves from the same status one wins.
func (m *ManifestStore) MoveChangeSet(ctx context.Context, id, from, to, by, reason string, at time.Time) (store.ChangeSet, error) {
	set := `status = $1, updated = $2`
	args := []any{to, at.UTC()}
	if by != "" {
		set += `, decided_by = $3, decided_at = $2, decision_reason = $4`
		args = append(args, by, reason)
	}
	args = append(args, id, from)
	q := fmt.Sprintf(`UPDATE change_sets SET %s WHERE id = $%d AND status = $%d RETURNING %s`, set, len(args)-1, len(args), changeSetColumns)
	cs, err := scanChangeSet(m.q.QueryRow(ctx, q, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := m.GetChangeSet(ctx, id); err != nil {
			return store.ChangeSet{}, err
		}
		return store.ChangeSet{}, store.ErrChangeSetMoved
	}
	return cs, err
}

const changeItemColumns = `set_id, kind, manifest_id, text, base, included, by, at, op, state`

func scanChangeItem(r pgx.Row) (store.ChangeItem, error) {
	var it store.ChangeItem
	var text string
	if err := r.Scan(&it.Set, &it.Kind, &it.ID, &text, &it.Base, &it.Included, &it.By, &it.At, &it.Op, &it.State); err != nil {
		return store.ChangeItem{}, err
	}
	it.Text, it.At = []byte(text), it.At.UTC()
	return it, nil
}

// PutChangeItem keeps an item.
func (m *ManifestStore) PutChangeItem(ctx context.Context, it store.ChangeItem) error {
	_, err := m.q.Exec(ctx, `INSERT INTO change_items (`+changeItemColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (set_id, kind, manifest_id) DO UPDATE SET text = EXCLUDED.text, base = EXCLUDED.base,
		included = EXCLUDED.included, by = EXCLUDED.by, at = EXCLUDED.at, op = EXCLUDED.op, state = EXCLUDED.state`,
		it.Set, it.Kind, it.ID, string(it.Text), it.Base, it.Included, it.By, it.At.UTC(), it.Op, it.State)
	if err != nil {
		return fmt.Errorf("put change item: %w", err)
	}
	return nil
}

// GetChangeItem returns one item.
func (m *ManifestStore) GetChangeItem(ctx context.Context, set, kind, id string) (store.ChangeItem, bool, error) {
	it, err := scanChangeItem(m.q.QueryRow(ctx, `SELECT `+changeItemColumns+` FROM change_items WHERE set_id = $1 AND kind = $2 AND manifest_id = $3`, set, kind, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ChangeItem{}, false, nil
	}
	return it, err == nil, err
}

// ListChangeItems returns a change set's items, oldest first.
func (m *ManifestStore) ListChangeItems(ctx context.Context, set string) ([]store.ChangeItem, error) {
	rows, err := m.q.Query(ctx, `SELECT `+changeItemColumns+` FROM change_items WHERE set_id = $1 ORDER BY at, kind, manifest_id`, set)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.ChangeItem{}
	for rows.Next() {
		it, err := scanChangeItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// DeleteChangeItem drops an item.
func (m *ManifestStore) DeleteChangeItem(ctx context.Context, set, kind, id string) error {
	_, err := m.q.Exec(ctx, `DELETE FROM change_items WHERE set_id = $1 AND kind = $2 AND manifest_id = $3`, set, kind, id)
	return err
}
