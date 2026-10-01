CREATE TABLE manifest_versions (
    kind    TEXT    NOT NULL,
    id      TEXT    NOT NULL,
    number  INTEGER NOT NULL,
    name    TEXT    NOT NULL DEFAULT '',
    yaml    TEXT    NOT NULL,
    actor   TEXT    NOT NULL,
    reason  TEXT    NOT NULL,
    on_ts   TEXT    NOT NULL,
    PRIMARY KEY (kind, id, number)
);

CREATE INDEX idx_manifest_versions_kind ON manifest_versions (kind);

-- Versions are immutable once written: no UPDATE, no DELETE.
CREATE TRIGGER trg_manifest_versions_no_update
BEFORE UPDATE ON manifest_versions
BEGIN
    SELECT RAISE(ABORT, 'manifest versions are immutable: update not allowed');
END;

CREATE TRIGGER trg_manifest_versions_no_delete
BEFORE DELETE ON manifest_versions
BEGIN
    SELECT RAISE(ABORT, 'manifest versions are immutable: delete not allowed');
END;

CREATE TABLE manifest_references (
    from_kind TEXT NOT NULL,
    from_id   TEXT NOT NULL,
    to_kind   TEXT NOT NULL,
    to_id     TEXT NOT NULL,
    path      TEXT NOT NULL,
    PRIMARY KEY (from_kind, from_id, path)
);

CREATE INDEX idx_manifest_references_to ON manifest_references (to_kind, to_id);

CREATE TABLE proposals (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    manifest_id  TEXT NOT NULL,
    base_version INTEGER NOT NULL,
    yaml         TEXT NOT NULL,
    actor        TEXT NOT NULL,
    reason       TEXT NOT NULL,
    state        TEXT NOT NULL,
    on_ts        TEXT NOT NULL
);

CREATE INDEX idx_proposals_state ON proposals (state);
