# 0011. Access by role and team, enrolled from the directory

**Status:** Accepted

## Context

An organisation that runs Cartograph for many people already has a
directory: its people, its groups and its structure, behind a single
sign-on provider. It needs four things Cartograph did not have. Only
the people it lists may sign in. Each person holds roles that decide
what they may change. A contributor changes the work of their own
teams and of the teams beneath them, and nobody else's. An
administrator manages all of this from inside Cartograph, without a
ticket to the directory team for every change.

Cartograph already had the two ports. An `Authenticator` turns a
request into a `Principal`, and `auth/proxy` builds one from the
headers an authenticating proxy sets. An `Authorizer` decides on a
verb and a manifest, and `auth/roles` split principals into readers
and writers. Neither could say "this project belongs to a team this
person acts for", and the authorizer never saw a manifest's content.

The directory is reached through OpenID Connect. An identity broker
such as Dex speaks to the real provider (Entra ID, LDAP, SAML, another
OIDC provider) and gives Cartograph's side one OIDC issuer with a
`groups` claim. It reports a person's groups when they sign in, and
offers no way to list the directory.

TAXONOMY.md D27 settles the words: people are principals on an access
list, never manifests; teams are the `Team` kind; four roles.

## Decision

**Sign-in stays at the edge.** An OIDC proxy (oauth2-proxy) in front
of the engine does the authorization code flow with the broker and
passes the subject, email, display name and groups as headers.
`auth/proxy` reads them, now including the email. The engine holds no
session, no token and no login page, and stays stateless (ADR 0008).

**The access list is a port.** `store.AccessStore` keeps one record
per person, keyed by email address: display name, subject, roles,
teams, how they were enrolled, and when they last signed in. The
memory, SQLite, vault and Postgres stores implement it and pass one
conformance suite.

**Enrolment from the directory.** A mapping, read from a file at start
(`CARTOGRAPH_ACCESS_FILE`), names which directory groups grant which
roles and which groups are which teams, with their parents. When a
person signs in, the engine applies it: it lists a person whose groups
grant a role, refreshes the roles and teams their groups give, and
creates any mapped `Team` manifest that does not exist yet. An
administrator may also add a person by address before they sign in,
and grant roles and teams by hand. What the directory gives and what
an administrator gives are kept apart: a sign-in refreshes the first
and never touches the second.

**The policy is an authorizer.** `auth/access` decides from the
principal's roles and teams. The HTTP middleware asks it about every
request, as before, and refuses what no team could make right: a
reader's write, a contributor's change to a goal. The team check needs
the manifest's content, so the engine asks the same authorizer again
at every write (a save, a version, a deletion, a hand-off, and every
change that arrives on the sync socket) with the chain of teams the
manifest sits under before the change and after it. A contributor may
make the change when one of their teams is on both chains: they may
neither edit another team's work nor move their own work out of reach.
The engine resolves the chains from the `Team` manifests, so the
policy needs no store.

**The session says what the interface may offer.** `GET /session`
returns the person's email, roles and teams (with every team beneath
them) and, for each kind, whether they may write all of it, their
teams' part of it, or none. The interface uses it to offer editing;
the engine decides again on every write. `/access` lists the roles and
lets an administrator list, add, change and remove people.

## Options considered

**OIDC inside the engine.** The engine would run the code flow and
keep a signed session cookie. It removes one component at the edge,
but puts token handling, login pages and key rotation into every
replica, where a maintained proxy already does them well.

**Roles from directory groups only.** No access list: whatever the
directory says, Cartograph obeys. Simple, but every change of role
becomes a directory change, and "only the people listed here can sign
in" cannot be expressed.

**A Person kind.** People as manifests, in Sheets beside teams. It
reverses the rule that definitions name roles (D27 says why).

**The whole policy in a distribution.** The engine's adapters are
internal packages, so a separate module cannot add one; it would have
to fork the engine or make the ports public. The policy is general
enough for any multi-team deployment, so it lives here, and a
distribution configures it.

## Consequences

- A deployment that does nothing keeps `AllowAll` and loses nothing.
  `CARTOGRAPH_AUTHZ=access` with `CARTOGRAPH_AUTH=proxy` turns it on.
- Every write path calls the authorizer with the manifest's teams.
  A new write path must do the same; `internal/engine` has one helper
  for it.
- A person removed from the directory keeps their record until an
  administrator removes it, but loses what their groups gave at the
  next sign-in; the proxy refuses them before that.
- `cartograph-oidc` is the reference distribution: Dex, oauth2-proxy,
  a directory to try it against, compose and Helm wiring, and an
  end-to-end test.
