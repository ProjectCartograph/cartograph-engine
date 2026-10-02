package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// AccessStore is the Postgres adapter for store.AccessStore: one row per
// person. Each method is one statement; an upsert on the address is what
// makes two replicas enrolling one person, or a sign-in racing a grant,
// leave every field either writer set.
type AccessStore struct {
	pool *pgxpool.Pool
}

var _ store.AccessStore = (*AccessStore)(nil)

// NewAccessStore returns an AccessStore over a pool Open returned.
func NewAccessStore(pool *pgxpool.Pool) *AccessStore {
	return &AccessStore{pool: pool}
}

const personColumns = `email, name, subject, roles, teams, directory_roles, directory_teams, added_by, added_on, last_signed_in`

func (s *AccessStore) ListPeople(ctx context.Context) ([]store.Person, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+personColumns+` FROM people ORDER BY email`)
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
	p, err := scanPerson(s.pool.QueryRow(ctx, `SELECT `+personColumns+` FROM people WHERE email = $1`, email))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Person{}, store.ErrNoPerson
	}
	if err != nil {
		return store.Person{}, fmt.Errorf("get person: %w", err)
	}
	return p, nil
}

func (s *AccessStore) GrantPerson(ctx context.Context, p store.Person) (store.Person, error) {
	out, err := scanPerson(s.pool.QueryRow(ctx, `
		INSERT INTO people (email, roles, teams, added_by, added_on) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (email) DO UPDATE SET roles = excluded.roles, teams = excluded.teams
		RETURNING `+personColumns,
		p.Email, list(p.Roles), list(p.Teams), p.AddedBy, p.AddedOn))
	if err != nil {
		return store.Person{}, fmt.Errorf("grant %s: %w", p.Email, err)
	}
	return out, nil
}

func (s *AccessStore) RecordSignIn(ctx context.Context, in store.SignIn) (store.Person, error) {
	var row pgx.Row
	if in.Enrol {
		row = s.pool.QueryRow(ctx, `
			INSERT INTO people (email, name, subject, directory_roles, directory_teams, added_by, added_on, last_signed_in)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
			ON CONFLICT (email) DO UPDATE SET name = excluded.name, subject = excluded.subject,
				directory_roles = excluded.directory_roles, directory_teams = excluded.directory_teams,
				last_signed_in = excluded.last_signed_in
			RETURNING `+personColumns,
			in.Email, in.Name, in.Subject, list(in.Roles), list(in.Teams), store.EnrolledByDirectory, in.At)
	} else {
		row = s.pool.QueryRow(ctx, `
			UPDATE people SET name = $2, subject = $3, directory_roles = $4, directory_teams = $5, last_signed_in = $6
			WHERE email = $1
			RETURNING `+personColumns,
			in.Email, in.Name, in.Subject, list(in.Roles), list(in.Teams), in.At)
	}
	p, err := scanPerson(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Person{}, store.ErrNoPerson
	}
	if err != nil {
		return store.Person{}, fmt.Errorf("record sign-in of %s: %w", in.Email, err)
	}
	return p, nil
}

func (s *AccessStore) DeletePerson(ctx context.Context, email string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM people WHERE email = $1`, email)
	if err != nil {
		return fmt.Errorf("delete %s: %w", email, err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNoPerson
	}
	return nil
}

func scanPerson(r pgx.Row) (store.Person, error) {
	var p store.Person
	var signedIn *time.Time
	if err := r.Scan(&p.Email, &p.Name, &p.Subject, &p.Roles, &p.Teams, &p.DirectoryRoles, &p.DirectoryTeams, &p.AddedBy, &p.AddedOn, &signedIn); err != nil {
		return store.Person{}, err
	}
	if signedIn != nil {
		p.LastSignedIn = *signedIn
	}
	return p, nil
}

// list is a string list as the table keeps it: an array, never null.
func list(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
