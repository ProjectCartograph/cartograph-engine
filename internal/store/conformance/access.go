package conformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// RunAccessStore exercises every store.AccessStore method against a
// freshly constructed adapter, including a sign-in racing an
// administrator's grant and two replicas enrolling one person at once.
func RunAccessStore(t *testing.T, newStore func(t *testing.T) store.AccessStore) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)

	t.Run("an empty list", func(t *testing.T) {
		s := newStore(t)
		people, err := s.ListPeople(ctx)
		must(t, err)
		if len(people) != 0 {
			t.Fatalf("expected nobody, got %v", people)
		}
		if _, err := s.GetPerson(ctx, "nobody@example.org"); !errors.Is(err, store.ErrNoPerson) {
			t.Fatalf("get of an unlisted address: %v", err)
		}
		if err := s.DeletePerson(ctx, "nobody@example.org"); !errors.Is(err, store.ErrNoPerson) {
			t.Fatalf("delete of an unlisted address: %v", err)
		}
	})

	t.Run("an administrator lists a person, then changes what they hold", func(t *testing.T) {
		s := newStore(t)
		p, err := s.GrantPerson(ctx, store.Person{Email: "lee@example.org", Roles: []string{"reader"}, AddedBy: "admin@example.org", AddedOn: at})
		must(t, err)
		if p.Email != "lee@example.org" || !same(p.Roles, "reader") || p.AddedBy != "admin@example.org" || !p.AddedOn.Equal(at) || !p.LastSignedIn.IsZero() {
			t.Fatalf("listed: %+v", p)
		}
		p, err = s.GrantPerson(ctx, store.Person{Email: "lee@example.org", Roles: []string{"contributor", "reader"}, Teams: []string{"early-grades"}, AddedBy: "someone-else", AddedOn: at.Add(time.Hour)})
		must(t, err)
		if !same(p.Roles, "contributor", "reader") || !same(p.Teams, "early-grades") {
			t.Fatalf("changed: %+v", p)
		}
		if p.AddedBy != "admin@example.org" || !p.AddedOn.Equal(at) {
			t.Fatalf("a change must keep who listed them and when: %+v", p)
		}
		got, err := s.GetPerson(ctx, "lee@example.org")
		must(t, err)
		if !reflect.DeepEqual(norm(got), norm(p)) {
			t.Fatalf("get returned %+v, grant returned %+v", got, p)
		}
	})

	t.Run("a sign-in records the directory's part and keeps the administrator's", func(t *testing.T) {
		s := newStore(t)
		_, err := s.GrantPerson(ctx, store.Person{Email: "lee@example.org", Roles: []string{"administrator"}, Teams: []string{"assessment"}, AddedBy: "admin@example.org", AddedOn: at})
		must(t, err)
		p, err := s.RecordSignIn(ctx, store.SignIn{Email: "lee@example.org", Name: "Lee Okafor", Subject: "sub-1", Roles: []string{"reader"}, Teams: []string{"early-grades"}, At: at.Add(time.Hour)})
		must(t, err)
		if p.Name != "Lee Okafor" || p.Subject != "sub-1" || !same(p.DirectoryRoles, "reader") || !same(p.DirectoryTeams, "early-grades") || !p.LastSignedIn.Equal(at.Add(time.Hour)) {
			t.Fatalf("sign-in: %+v", p)
		}
		if !same(p.Roles, "administrator") || !same(p.Teams, "assessment") {
			t.Fatalf("a sign-in must keep what an administrator granted: %+v", p)
		}
		// The directory's part is replaced, not added to, at each sign-in.
		p, err = s.RecordSignIn(ctx, store.SignIn{Email: "lee@example.org", Name: "Lee Okafor", Subject: "sub-1", At: at.Add(2 * time.Hour)})
		must(t, err)
		if len(p.DirectoryRoles) != 0 || len(p.DirectoryTeams) != 0 {
			t.Fatalf("groups gone from the directory must be gone here: %+v", p)
		}
		// And an administrator's change keeps the sign-in's part.
		p, err = s.GrantPerson(ctx, store.Person{Email: "lee@example.org", Roles: []string{"reader"}})
		must(t, err)
		if p.Name != "Lee Okafor" || !p.LastSignedIn.Equal(at.Add(2*time.Hour)) {
			t.Fatalf("a grant must keep what the sign-in recorded: %+v", p)
		}
	})

	t.Run("an unlisted person is refused unless the sign-in enrols them", func(t *testing.T) {
		s := newStore(t)
		_, err := s.RecordSignIn(ctx, store.SignIn{Email: "new@example.org", Name: "New Person", At: at})
		if !errors.Is(err, store.ErrNoPerson) {
			t.Fatalf("an unlisted sign-in without enrolment: %v", err)
		}
		p, err := s.RecordSignIn(ctx, store.SignIn{Email: "new@example.org", Name: "New Person", Subject: "sub-2", Roles: []string{"reader"}, At: at, Enrol: true})
		must(t, err)
		if p.Email != "new@example.org" || p.AddedBy != store.EnrolledByDirectory || !p.AddedOn.Equal(at) || !same(p.DirectoryRoles, "reader") || len(p.Roles) != 0 {
			t.Fatalf("enrolled: %+v", p)
		}
	})

	t.Run("listed by address, and removed", func(t *testing.T) {
		s := newStore(t)
		for _, e := range []string{"c@example.org", "a@example.org", "b@example.org"} {
			_, err := s.GrantPerson(ctx, store.Person{Email: e, Roles: []string{"reader"}, AddedBy: "admin", AddedOn: at})
			must(t, err)
		}
		people, err := s.ListPeople(ctx)
		must(t, err)
		if len(people) != 3 || people[0].Email != "a@example.org" || people[2].Email != "c@example.org" {
			t.Fatalf("listed: %v", people)
		}
		must(t, s.DeletePerson(ctx, "b@example.org"))
		if _, err := s.GetPerson(ctx, "b@example.org"); !errors.Is(err, store.ErrNoPerson) {
			t.Fatalf("after delete: %v", err)
		}
		people, err = s.ListPeople(ctx)
		must(t, err)
		if len(people) != 2 {
			t.Fatalf("after delete: %v", people)
		}
	})

	t.Run("replicas enrolling one person at once both succeed, once", func(t *testing.T) {
		s := newStore(t)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := s.RecordSignIn(ctx, store.SignIn{Email: "same@example.org", Name: fmt.Sprintf("replica %d", i), At: at, Enrol: true, Roles: []string{"reader"}})
				errs <- err
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			must(t, err)
		}
		people, err := s.ListPeople(ctx)
		must(t, err)
		if len(people) != 1 {
			t.Fatalf("expected one person, got %d", len(people))
		}
	})

	t.Run("a sign-in racing a grant keeps both", func(t *testing.T) {
		s := newStore(t)
		_, err := s.GrantPerson(ctx, store.Person{Email: "lee@example.org", AddedBy: "admin", AddedOn: at})
		must(t, err)
		var wg sync.WaitGroup
		errs := make(chan error, 40)
		for i := 0; i < 20; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, err := s.GrantPerson(ctx, store.Person{Email: "lee@example.org", Roles: []string{"administrator"}})
				errs <- err
			}()
			go func() {
				defer wg.Done()
				_, err := s.RecordSignIn(ctx, store.SignIn{Email: "lee@example.org", Name: "Lee Okafor", Teams: []string{"assessment"}, At: at})
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			must(t, err)
		}
		p, err := s.GetPerson(ctx, "lee@example.org")
		must(t, err)
		if !same(p.Roles, "administrator") || !same(p.DirectoryTeams, "assessment") || p.Name != "Lee Okafor" {
			t.Fatalf("one writer undid the other: %+v", p)
		}
	})
}

// same reports whether got holds exactly want, in order.
func same(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// norm makes empty and nil slices compare equal, and times compare by
// instant, so adapters that read back an empty array as nil, or a time
// in another location, still match.
func norm(p store.Person) store.Person {
	for _, s := range []*[]string{&p.Roles, &p.Teams, &p.DirectoryRoles, &p.DirectoryTeams} {
		if len(*s) == 0 {
			*s = nil
		}
	}
	p.AddedOn = p.AddedOn.UTC()
	p.LastSignedIn = p.LastSignedIn.UTC()
	return p
}
