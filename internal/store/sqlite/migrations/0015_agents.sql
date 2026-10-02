-- Agents and their proposals (docs/adr/0016), as Postgres keeps them.

ALTER TABLE people ADD COLUMN agents_off INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS proposals (
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL,
    manifest_id     TEXT NOT NULL,
    op              TEXT NOT NULL,
    text            TEXT,
    series          TEXT NOT NULL DEFAULT '',
    item            TEXT,
    state           TEXT NOT NULL DEFAULT '',
    base            INTEGER NOT NULL DEFAULT 0,
    reason          TEXT NOT NULL DEFAULT '',
    agent           TEXT NOT NULL,
    for_person      TEXT NOT NULL DEFAULT '',
    at              TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'open',
    decided_by      TEXT NOT NULL DEFAULT '',
    decided_at      TEXT NOT NULL DEFAULT '',
    decision_reason TEXT NOT NULL DEFAULT '',
    version         INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS proposals_for ON proposals (for_person, status, at);
CREATE INDEX IF NOT EXISTS proposals_manifest ON proposals (kind, manifest_id, at);

CREATE TRIGGER IF NOT EXISTS proposals_proposed AFTER INSERT ON proposals
BEGIN
    INSERT INTO events (at, type, kind, id, detail, actor) VALUES (NEW.at, 'proposal', NEW.kind, NEW.manifest_id, NEW.op, NEW.agent);
END;

CREATE TRIGGER IF NOT EXISTS proposals_decided AFTER UPDATE OF status ON proposals
WHEN NEW.status <> OLD.status
BEGIN
    INSERT INTO events (at, type, kind, id, detail, actor) VALUES (NEW.decided_at, 'proposal', NEW.kind, NEW.manifest_id, NEW.status, NEW.decided_by);
END;

CREATE TABLE IF NOT EXISTS agent_grants (
    id          TEXT PRIMARY KEY,
    email       TEXT NOT NULL,
    label       TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    last_used   TEXT NOT NULL DEFAULT '',
    revoked_at  TEXT NOT NULL DEFAULT '',
    generation  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS agent_grants_email ON agent_grants (email, created_at);
