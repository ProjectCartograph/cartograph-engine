-- Comments on a record, beside the part they are about (TAXONOMY.md D72):
-- a conversation about the record, kept apart from its versions. Mentions
-- are kept as JSON and matched with json_each.

CREATE TABLE IF NOT EXISTS comments (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL,
    manifest_id TEXT NOT NULL,
    anchor      TEXT NOT NULL DEFAULT '',
    reply       TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL,
    mentions    TEXT NOT NULL DEFAULT '[]',
    author      TEXT NOT NULL DEFAULT '',
    at          TEXT NOT NULL,
    resolved    INTEGER NOT NULL DEFAULT 0,
    resolved_by TEXT NOT NULL DEFAULT '',
    resolved_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS comments_record ON comments (kind, manifest_id, at);
