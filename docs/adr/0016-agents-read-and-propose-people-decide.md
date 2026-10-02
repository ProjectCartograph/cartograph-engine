# 0016. Agents read and propose; people decide

**Status:** Accepted

## Context

Agents can help people define their work: walk them through a flow,
draft a project from a meeting's notes, record the month's readings,
spot a goal with no indicator. MCP is how they connect. But an agent is
not accountable for the record, and a person is. MCP tool annotations
say which tools change things, and clients may ignore them, so any rule
about what an agent may do has to hold in Cartograph, whatever the
client.

## Decision

**An agent acts for a person, with that person's authority and no
more.** It signs in through the organisation's provider like its
person, and every request carries both: the principal is the person,
with the agent named beside it (`identity.Principal.Agent`). What it
does is recorded as "ada@example.org via Claude". When the person
loses access, so does their agent.

**An agent reads and proposes; it does not make the record.** It may
read everything its person may, validate, run checks, and edit drafts,
where the people on the same manifest see its changes and see it
working (presence, drawn in one colour for every agent). Saving a
version, recording a reading, moving or handing off a project,
deleting, recovering, importing and changing the access list are
refused to it by the engine itself, at every one of those operations,
with or without an access policy. That includes a local agent over
stdio.

**Its person confirms in Cartograph.** What an agent would have made
the record becomes a proposal: the exact manifest, reading or state
change, the version it was proposed against, the agent and the
reason. Only the person it was made for accepts or declines it, never
an agent. Accepting makes the record as that person, under their own
authority, with "proposed by" in the reason. A proposal whose manifest
changed since is refused, so nobody accepts over a change they have
not seen. Proposals and decisions are events.

**Agents are off unless a deployment turns them on, then by role.**
`CARTOGRAPH_MCP=on` serves MCP at `/api/v1/mcp`. With an access list,
its mapping's `agents:` key names the roles whose people may use one;
an administrator may turn one person's agents off on the Access page.

**Stateless.** MCP is served sessionless (the 2026 transport), one
request at a time, so agents hold nothing open and a deployment still
scales to zero (ADR 0015). An agent follows changes through the
`events` tool, from its cursor.

**Sign-in is MCP's browser flow, and who runs it is a port.** A person
adds Cartograph's MCP address to their client, signs in, and allows the
agent. Cartograph serves RFC 9728 protected resource metadata naming
the authorization server (`CARTOGRAPH_MCP_ISSUER`), and one of two
adapters, chosen by `CARTOGRAPH_MCP_AUTH`, authenticates the agent's
requests:

- `proxy`: the deployment's own stack is the authorization server (an
  identity provider behind oauth2-proxy, or Laravel with Passport); the
  proxy checks the token and passes the same identity headers as for a
  browser. Cartograph keeps nothing about tokens.
- `cartograph`: Cartograph's own OAuth 2.1 authorization server
  (`internal/oauth`), because the providers most organisations use for
  sign-in (Entra ID, Dex) make an administrator register every client.
  Clients register themselves, by Client ID Metadata Document or dynamic
  registration; the person signs in through the deployment's usual
  authenticator and consents on Cartograph's page. A grant records the
  consent (the person, the agent's name, its expiry), and the person or
  an administrator revokes it on the Access page. Tokens are signed with
  `CARTOGRAPH_AGENT_KEY` and kept nowhere: an access token lasts an hour,
  a refresh token is good once, and a code or refresh token presented
  twice revokes its grant. On `/api/v1/mcp` only the token counts; no
  proxy header is read.

The engine knows grants and their rules (who may grant, that an agent
may not, that a grant ends with its person's place on the access list),
never a token. An agent's request carries no directory groups, so a
delegated principal's roles are those recorded at the person's last
sign-in, for 30 days after it.

## The dependency

The official MCP Go SDK (`modelcontextprotocol/go-sdk`), the protocol's
reference for Go, maintained with Google. Pure Go, so the binary stays
static. Only `internal/mcp` imports it; its package also holds a client
that can start servers as processes, so the dependency rule allows
`os/exec` there, unused (ADR 0010).

## Options considered

**Agents with identities of their own only.** Clear accountability per
agent, but a person's agent could not see what the person sees without
separate grants. Delegation covers the common case.

**Confirmation in the MCP client (elicitation).** Smooth where a client
supports it, but the server cannot tell a person answered.

**Only the identity provider's tokens.** What the MCP specification
describes first, and the right choice where the provider registers
clients itself; kept as the `proxy` adapter. As the only way, it would
make an administrator register every client with Entra ID or Dex.

**Only tokens a person pastes.** Works with any client and needs no
browser flow, but it is outside the specification and a step people do
by hand. Kept as a fallback on the Access page.

**Everything the person may.** Fastest, and it would rely on the client
to ask first, which nothing guarantees.

## Consequences

- Nothing an agent does is in the record until a person says so.
- What an agent may propose, and how it is guided, is ADR 0017: the
  same checks a person meets, proposals in sets, reviewed before they
  are decided.
- An interface shows proposals: an inbox for its person, and a notice
  on the manifest for everyone who reads it.
- Handoffs still render their charter where the API runs; accepting a
  proposed handoff goes through the same path.
- A future service agent (a pipeline with its own identity) is a
  further decision, not covered here.
