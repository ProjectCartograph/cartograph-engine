# 0015. Only an attended window holds a connection

**Status:** Accepted

## Context

A deployment that scales to zero (Cloud Run, Knative, Azure Container
Apps) keeps a replica while it has a request or an open connection. The
sync socket was open for as long as a window was, and presence sent a
heartbeat every three seconds, so one forgotten tab held a replica up
all night. The server's pings, meant only to keep load balancers from
closing quiet connections, kept it open just as well.

## Decision

A window holds a connection only while someone is at it.

The interface decides, because only the page knows. Someone is there
while the page is visible and there was input within two minutes.
Hidden for a minute, or two minutes without input, and the interface
closes its socket, which stops automerge-repo's five-second retry too.
The next input, or the page shown again, opens it. Drafts are kept on
the device meanwhile, and an edit is input, so nothing is lost or
delayed.

The server keeps a backstop, `CARTOGRAPH_SYNC_IDLE` (10 minutes): a
connection that has changed no document for that long is closed with
code 4000, "idle". Only a change to a document counts. Pings, the
server's own rechecks and presence do not, because the server cannot
tell a person from a heartbeat, and only a person changes a document.
`cartograph_sync_idle_closed_total` counts these closes.

## Options considered

**Server-side only.** The server cannot see attention, and the stock
automerge-repo client reconnects five seconds after any close: an idle
tab would reconnect every ten minutes forever. Measured: closed at 60 s,
back at 65 s. Rejected as the main mechanism; kept as the backstop for
clients that do not follow attention.

**Stopping pings when idle.** A connection without pings is still open
and still holds the replica. Pings stay, for load balancers.

**Counting presence as activity on the server.** A heartbeat would then
hold every window open. Rejected.

## Consequences

- An unattended window costs nothing, and a deployment with nobody at
  it scales to zero within the interface's two minutes.
- Someone reading without editing for ten minutes is closed by the
  server and reconnected by the interface at once, because they are
  still there.
- A third-party interface should follow attention the same way;
  `DISTRIBUTIONS.md` says so. One that does not reconnects after each
  idle close and keeps its replica up.
