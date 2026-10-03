-- Change sets (docs/adr/0022): a piece of work's own drafts, apart from
-- the record and from every other piece of work, until it is accepted.
CREATE TABLE change_sets (
    id              text        PRIMARY KEY,
    title           text        NOT NULL DEFAULT '',
    description     text        NOT NULL DEFAULT '',
    owner           text        NOT NULL DEFAULT '',
    agent           text        NOT NULL DEFAULT '',
    for_person      text        NOT NULL DEFAULT '',
    status          text        NOT NULL DEFAULT 'open',
    reason          text        NOT NULL DEFAULT '',
    waivers         text        NOT NULL DEFAULT '[]',
    at              timestamptz NOT NULL,
    updated         timestamptz NOT NULL,
    decided_by      text        NOT NULL DEFAULT '',
    decided_at      timestamptz,
    decision_reason text        NOT NULL DEFAULT ''
);

CREATE INDEX change_sets_for ON change_sets (for_person, status, at DESC);
CREATE INDEX change_sets_owner ON change_sets (owner, status);

-- One draft per manifest per change set, with the version it started from.
CREATE TABLE change_items (
    set_id      text        NOT NULL REFERENCES change_sets (id) ON DELETE CASCADE,
    kind        text        NOT NULL,
    manifest_id text        NOT NULL,
    text        text        NOT NULL,
    base        integer     NOT NULL DEFAULT 0,
    included    boolean     NOT NULL DEFAULT true,
    by          text        NOT NULL DEFAULT '',
    at          timestamptz NOT NULL,
    PRIMARY KEY (set_id, kind, manifest_id)
);
