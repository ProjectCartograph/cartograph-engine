-- The access list (docs/adr/0011): who may sign in, what an
-- administrator granted them, and what the directory gave at their last
-- sign-in. Lists are JSON arrays of strings. Times are RFC 3339 in UTC;
-- last_signed_in is empty until the first sign-in. Like documents, this
-- is not rebuildable from the files.
CREATE TABLE IF NOT EXISTS people (
    email           TEXT PRIMARY KEY,
    name            TEXT NOT NULL DEFAULT '',
    subject         TEXT NOT NULL DEFAULT '',
    roles           TEXT NOT NULL DEFAULT '[]',
    teams           TEXT NOT NULL DEFAULT '[]',
    directory_roles TEXT NOT NULL DEFAULT '[]',
    directory_teams TEXT NOT NULL DEFAULT '[]',
    added_by        TEXT NOT NULL DEFAULT '',
    added_on        TEXT NOT NULL,
    last_signed_in  TEXT NOT NULL DEFAULT ''
);
