package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// AccessStore is the SQLite adapter for store.AccessStore. Each method
// is one statement, and the database has one connection, so each is
// atomic.
type AccessStore struct {
	db *sql.DB
}

var _ store.AccessStore = (*AccessStore)(nil)

// NewAccessStore returns an AccessStore over a database Open returned.
func NewAccessStore(db *sql.DB) *AccessStore {
	return &AccessStore{db: db}
}

const personColumns = `email, name, subject, roles, teams, directory_roles, directory_teams, added_by, added_on, last_signed_in, agents_off`

func (s *AccessStore) ListPeople(ctx context.Context) ([]store.Person, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+personColumns+` FROM people ORDER BY email`)
	if err != nil {
		return nil, fmt.Errorf("list people: %w", err)
	}
	defer rows.Close()
	out := []store.Person{}
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, fmt.Errorf("list people: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *AccessStore) GetPerson(ctx context.Context, email string) (store.Person, error) {
	p, err := scanPerson(s.db.QueryRowContext(ctx, `SELECT `+personColumns+` FROM people WHERE email = ?`, email))
	if errors.Is(err, sql.ErrNoRows) {
		return store.Person{}, store.ErrNoPerson
	}
	if err != nil {
		return store.Person{}, fmt.Errorf("get person: %w", err)
	}
	return p, nil
}

func (s *AccessStore) GrantPerson(ctx context.Context, p store.Person) (store.Person, error) {
	out, err := scanPerson(s.db.QueryRowContext(ctx, `
		INSERT INTO people (email, roles, teams, added_by, added_on, agents_off) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (email) DO UPDATE SET roles = excluded.roles, teams = excluded.teams, agents_off = excluded.agents_off
		RETURNING `+personColumns,
		p.Email, list(p.Roles), list(p.Teams), p.AddedBy, stamp(p.AddedOn), p.AgentsOff))
	if err != nil {
		return store.Person{}, fmt.Errorf("grant %s: %w", p.Email, err)
	}
	return out, nil
}

func (s *AccessStore) RecordSignIn(ctx context.Context, in store.SignIn) (store.Person, error) {
	var row *sql.Row
	if in.Enrol {
		row = s.db.QueryRowContext(ctx, `
			INSERT INTO people (email, name, subject, directory_roles, directory_teams, added_by, added_on, last_signed_in)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (email) DO UPDATE SET name = excluded.name, subject = excluded.subject,
				directory_roles = excluded.directory_roles, directory_teams = excluded.directory_teams,
				last_signed_in = excluded.last_signed_in
			RETURNING `+personColumns,
			in.Email, in.Name, in.Subject, list(in.Roles), list(in.Teams), store.EnrolledByDirectory, stamp(in.At), stamp(in.At))
	} else {
		row = s.db.QueryRowContext(ctx, `
			UPDATE people SET name = ?, subject = ?, directory_roles = ?, directory_teams = ?, last_signed_in = ?
			WHERE email = ?
			RETURNING `+personColumns,
			in.Name, in.Subject, list(in.Roles), list(in.Teams), stamp(in.At), in.Email)
	}
	p, err := scanPerson(row)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Person{}, store.ErrNoPerson
	}
	if err != nil {
		return store.Person{}, fmt.Errorf("record sign-in of %s: %w", in.Email, err)
	}
	return p, nil
}

func (s *AccessStore) DeletePerson(ctx context.Context, email string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM people WHERE email = ?`, email)
	if err != nil {
		return fmt.Errorf("delete %s: %w", email, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return store.ErrNoPerson
	}
	return nil
}

type scanner interface{ Scan(dest ...any) error }

func scanPerson(r scanner) (store.Person, error) {
	var p store.Person
	var roles, teams, dirRoles, dirTeams, addedOn, signedIn string
	if err := r.Scan(&p.Email, &p.Name, &p.Subject, &roles, &teams, &dirRoles, &dirTeams, &p.AddedBy, &addedOn, &signedIn, &p.AgentsOff); err != nil {
		return store.Person{}, err
	}
	for _, f := range []struct {
		text string
		to   *[]string
	}{{roles, &p.Roles}, {teams, &p.Teams}, {dirRoles, &p.DirectoryRoles}, {dirTeams, &p.DirectoryTeams}} {
		if err := json.Unmarshal([]byte(f.text), f.to); err != nil {
			return store.Person{}, fmt.Errorf("person %s: %w", p.Email, err)
		}
	}
	var err error
	if p.AddedOn, err = unstamp(addedOn); err != nil {
		return store.Person{}, err
	}
	if p.LastSignedIn, err = unstamp(signedIn); err != nil {
		return store.Person{}, err
	}
	return p, nil
}

// list is a string list as the table keeps it: a JSON array, never null.
func list(s []string) string {
	if s == nil {
		s = []string{}
	}
	b, _ := json.Marshal(s)
	return string(b)
}

// stamp is a time as the table keeps it; the zero time is empty.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func unstamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}
