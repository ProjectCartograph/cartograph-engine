package store

import (
	"context"
	"errors"
	"time"
)

// ErrNoPerson is returned for an address that is not on the access list.
var ErrNoPerson = errors.New("not on the access list")

// Person is one entry on the access list (docs/adr/0011, TAXONOMY.md
// D27): someone who may sign in, and what they hold. It is operational
// state about a principal, never a manifest.
//
// What an administrator grants and what the directory grants are kept
// apart, so a sign-in refreshing the one never undoes the other.
type Person struct {
	// Email is the key: the organisation address, lower-case.
	Email string
	// Name is the display name the directory gave at the last sign-in,
	// empty until the first.
	Name string
	// Subject is the identity provider's stable identifier, from the
	// last sign-in.
	Subject string
	// Roles and Teams are what an administrator granted.
	Roles []string
	Teams []string
	// DirectoryRoles and DirectoryTeams are what the directory's groups
	// gave at the last sign-in.
	DirectoryRoles []string
	DirectoryTeams []string
	// AddedBy is the actor who listed the person: an administrator, or
	// the directory when a sign-in enrolled them.
	AddedBy string
	AddedOn time.Time
	// LastSignedIn is zero until the first sign-in.
	LastSignedIn time.Time
}

// EnrolledByDirectory is the AddedBy of a person a sign-in listed.
const EnrolledByDirectory = "directory"

// SignIn is what a sign-in reports about a person.
type SignIn struct {
	Email, Name, Subject string
	// Roles and Teams are what the person's directory groups give now.
	Roles, Teams []string
	At           time.Time
	// Enrol lists the person when they are not listed yet, added by
	// EnrolledByDirectory at At; otherwise an unlisted person's sign-in
	// is ErrNoPerson.
	Enrol bool
}

// AccessStore keeps the access list. Every method is atomic for one
// person: two replicas recording the same person's sign-in, or a
// sign-in racing an administrator's change, both leave every field
// either writer set.
type AccessStore interface {
	// ListPeople returns everyone on the list, by address.
	ListPeople(ctx context.Context) ([]Person, error)
	// GetPerson returns one person, or ErrNoPerson.
	GetPerson(ctx context.Context, email string) (Person, error)
	// GrantPerson lists a person if they are not listed, with AddedBy and
	// AddedOn from p, and sets the roles and teams an administrator
	// grants. It leaves everything a sign-in records as it is.
	GrantPerson(ctx context.Context, p Person) (Person, error)
	// RecordSignIn sets the name, subject, directory roles and teams and
	// the time of a sign-in, listing the person first when s.Enrol says
	// so. It leaves what an administrator granted as it is.
	RecordSignIn(ctx context.Context, s SignIn) (Person, error)
	// DeletePerson removes a person from the list, or returns ErrNoPerson.
	DeletePerson(ctx context.Context, email string) error
}
