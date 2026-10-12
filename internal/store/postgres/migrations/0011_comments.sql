-- Comments on a record, beside the part they are about (TAXONOMY.md D72):
-- a conversation about the record, kept apart from its versions. Mentions
-- are kept as jsonb and matched by containment.

CREATE TABLE IF NOT EXISTS comments (
    id          text PRIMARY KEY,
    kind        text NOT NULL,
    manifest_id text NOT NULL,
    anchor      text NOT NULL DEFAULT '',
    reply       text NOT NULL DEFAULT '',
    body        text NOT NULL,
    mentions    jsonb NOT NULL DEFAULT '[]',
    author      text NOT NULL DEFAULT '',
    at          timestamptz NOT NULL,
    resolved    boolean NOT NULL DEFAULT false,
    resolved_by text NOT NULL DEFAULT '',
    resolved_at timestamptz
);

CREATE INDEX IF NOT EXISTS comments_record ON comments (kind, manifest_id, at);
CREATE INDEX IF NOT EXISTS comments_mentions ON comments USING gin (mentions jsonb_path_ops);
