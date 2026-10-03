-- Change sets (docs/adr/0022), as Postgres keeps them: a piece of work's
-- own drafts, apart from the record and from every other piece of work.

CREATE TABLE IF NOT EXISTS change_sets (
    id              TEXT PRIMARY KEY,
    title           TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    owner           TEXT NOT NULL DEFAULT '',
    agent           TEXT NOT NULL DEFAULT '',
    for_person      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'open',
    reason          TEXT NOT NULL DEFAULT '',
    waivers         TEXT NOT NULL DEFAULT '[]',
    at              TEXT NOT NULL,
    updated         TEXT NOT NULL,
    decided_by      TEXT NOT NULL DEFAULT '',
    decided_at      TEXT NOT NULL DEFAULT '',
    decision_reason TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS change_sets_for ON change_sets (for_person, status, at);
CREATE INDEX IF NOT EXISTS change_sets_owner ON change_sets (owner, status);

CREATE TABLE IF NOT EXISTS change_items (
    set_id      TEXT NOT NULL REFERENCES change_sets (id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    manifest_id TEXT NOT NULL,
    text        TEXT NOT NULL,
    base        INTEGER NOT NULL DEFAULT 0,
    included    INTEGER NOT NULL DEFAULT 1,
    by          TEXT NOT NULL DEFAULT '',
    at          TEXT NOT NULL,
    PRIMARY KEY (set_id, kind, manifest_id)
);
