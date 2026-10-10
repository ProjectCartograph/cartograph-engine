package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var _ store.ChangeSetStore = (*ManifestStore)(nil)

const changeSetColumns = `id, title, description, owner, agent, for_person, status, reason, waivers, at, updated, decided_by, decided_at, decision_reason, assumptions`

func scanChangeSet(r rowScanner) (store.ChangeSet, error) {
	var cs store.ChangeSet
	var waivers, at, updated, decided, assumptions string
	if err := r.Scan(&cs.ID, &cs.Title, &cs.Description, &cs.Owner, &cs.Agent, &cs.For, &cs.Status, &cs.Reason, &waivers,
		&at, &updated, &cs.DecidedBy, &decided, &cs.DecisionReason, &assumptions); err != nil {
		return store.ChangeSet{}, err
	}
	cs.At, _ = unstamp(at)
	cs.Updated, _ = unstamp(updated)
	cs.DecidedAt, _ = unstamp(decided)
	if waivers != "" {
		if err := json.Unmarshal([]byte(waivers), &cs.Waivers); err != nil {
			return store.ChangeSet{}, fmt.Errorf("change set %s waivers: %w", cs.ID, err)
		}
	}
	if assumptions != "" {
		if err := json.Unmarshal([]byte(assumptions), &cs.Assumptions); err != nil {
			return store.ChangeSet{}, fmt.Errorf("change set %s assumptions: %w", cs.ID, err)
		}
	}
	return cs, nil
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
	assumptions, err := json.Marshal(cs.Assumptions)
	if err != nil {
		return err
	}
	if cs.Assumptions == nil {
		assumptions = []byte("[]")
	}
	_, err = m.ex.ExecContext(ctx, `INSERT INTO change_sets (`+changeSetColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (id) DO UPDATE SET title = excluded.title, description = excluded.description, owner = excluded.owner,
		agent = excluded.agent, for_person = excluded.for_person, status = excluded.status, reason = excluded.reason,
		waivers = excluded.waivers, updated = excluded.updated, decided_by = excluded.decided_by,
		decided_at = excluded.decided_at, decision_reason = excluded.decision_reason, assumptions = excluded.assumptions`,
		cs.ID, cs.Title, cs.Description, cs.Owner, cs.Agent, cs.For, cs.Status, cs.Reason, string(waivers),
		stamp(cs.At), stamp(cs.Updated), cs.DecidedBy, stamp(cs.DecidedAt), cs.DecisionReason, string(assumptions))
	return err
}

// GetChangeSet returns one change set.
func (m *ManifestStore) GetChangeSet(ctx context.Context, id string) (store.ChangeSet, error) {
	cs, err := scanChangeSet(m.ex.QueryRowContext(ctx, `SELECT `+changeSetColumns+` FROM change_sets WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return store.ChangeSet{}, store.ErrNoChangeSet
	}
	return cs, err
}

// ListChangeSets returns change sets, newest first.
func (m *ManifestStore) ListChangeSets(ctx context.Context, f store.ChangeSetFilter) ([]store.ChangeSet, error) {
	q := `SELECT ` + changeSetColumns + ` FROM change_sets`
	var where []string
	var args []any
	for _, c := range []struct{ col, val string }{{"for_person", f.For}, {"owner", f.Owner}, {"status", f.Status}} {
		if c.val != "" {
			where = append(where, c.col+" = ?")
			args = append(args, c.val)
		}
	}
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	rows, err := m.ex.QueryContext(ctx, q+` ORDER BY at DESC, id DESC`, args...)
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

// MoveChangeSet moves a change set from one status to another.
func (m *ManifestStore) MoveChangeSet(ctx context.Context, id, from, to, by, reason string, at time.Time) (store.ChangeSet, error) {
	set := `status = ?, updated = ?`
	args := []any{to, stamp(at)}
	if by != "" {
		set += `, decided_by = ?, decided_at = ?, decision_reason = ?`
		args = append(args, by, stamp(at), reason)
	}
	args = append(args, id, from)
	cs, err := scanChangeSet(m.ex.QueryRowContext(ctx, `UPDATE change_sets SET `+set+` WHERE id = ? AND status = ? RETURNING `+changeSetColumns, args...))
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := m.GetChangeSet(ctx, id); err != nil {
			return store.ChangeSet{}, err
		}
		return store.ChangeSet{}, store.ErrChangeSetMoved
	}
	return cs, err
}

const changeItemColumns = `set_id, kind, manifest_id, text, base, included, by, at, op, state`

func scanChangeItem(r rowScanner) (store.ChangeItem, error) {
	var it store.ChangeItem
	var text, at string
	var included int
	if err := r.Scan(&it.Set, &it.Kind, &it.ID, &text, &it.Base, &included, &it.By, &at, &it.Op, &it.State); err != nil {
		return store.ChangeItem{}, err
	}
	it.Text, it.Included = []byte(text), included != 0
	it.At, _ = unstamp(at)
	return it, nil
}

// PutChangeItem keeps an item.
func (m *ManifestStore) PutChangeItem(ctx context.Context, it store.ChangeItem) error {
	included := 0
	if it.Included {
		included = 1
	}
	_, err := m.ex.ExecContext(ctx, `INSERT INTO change_items (`+changeItemColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (set_id, kind, manifest_id) DO UPDATE SET text = excluded.text, base = excluded.base,
		included = excluded.included, by = excluded.by, at = excluded.at, op = excluded.op, state = excluded.state`,
		it.Set, it.Kind, it.ID, string(it.Text), it.Base, included, it.By, stamp(it.At), it.Op, it.State)
	return err
}

// GetChangeItem returns one item.
func (m *ManifestStore) GetChangeItem(ctx context.Context, set, kind, id string) (store.ChangeItem, bool, error) {
	it, err := scanChangeItem(m.ex.QueryRowContext(ctx, `SELECT `+changeItemColumns+` FROM change_items WHERE set_id = ? AND kind = ? AND manifest_id = ?`, set, kind, id))
	if errors.Is(err, sql.ErrNoRows) {
		return store.ChangeItem{}, false, nil
	}
	return it, err == nil, err
}

// ListChangeItems returns a change set's items, oldest first.
func (m *ManifestStore) ListChangeItems(ctx context.Context, set string) ([]store.ChangeItem, error) {
	rows, err := m.ex.QueryContext(ctx, `SELECT `+changeItemColumns+` FROM change_items WHERE set_id = ? ORDER BY at, kind, manifest_id`, set)
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
	_, err := m.ex.ExecContext(ctx, `DELETE FROM change_items WHERE set_id = ? AND kind = ? AND manifest_id = ?`, set, kind, id)
	return err
}
