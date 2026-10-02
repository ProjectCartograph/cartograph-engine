-- The access list (docs/adr/0011): who may sign in, what an
-- administrator granted them, and what the directory gave at their last
-- sign-in. last_signed_in is null until the first sign-in.
CREATE TABLE people (
    email           text        PRIMARY KEY,
    name            text        NOT NULL DEFAULT '',
    subject         text        NOT NULL DEFAULT '',
    roles           text[]      NOT NULL DEFAULT '{}',
    teams           text[]      NOT NULL DEFAULT '{}',
    directory_roles text[]      NOT NULL DEFAULT '{}',
    directory_teams text[]      NOT NULL DEFAULT '{}',
    added_by        text        NOT NULL DEFAULT '',
    added_on        timestamptz NOT NULL,
    last_signed_in  timestamptz
);
