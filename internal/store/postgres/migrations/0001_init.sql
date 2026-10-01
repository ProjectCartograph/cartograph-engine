-- The whole state of a stateless deployment (docs/adr/0008). Every row
-- is live: there is no state manifest, no file hash and no journal,
-- because there are no files.

-- Versions are immutable once written.
CREATE TABLE manifest_versions (
    kind   text        NOT NULL,
    id     text        NOT NULL,
    number integer     NOT NULL CHECK (number > 0),
    name   text        NOT NULL DEFAULT '',
    yaml   text        NOT NULL,
    actor  text        NOT NULL,
    reason text        NOT NULL,
    on_ts  timestamptz NOT NULL,
    PRIMARY KEY (kind, id, number)
);

CREATE INDEX manifest_versions_on_ts ON manifest_versions (on_ts DESC, kind, id, number DESC);

CREATE FUNCTION cartograph_refuse_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% is append-only: % not allowed', TG_TABLE_NAME, lower(TG_OP);
END;
$$;

CREATE TRIGGER manifest_versions_immutable
BEFORE UPDATE OR DELETE ON manifest_versions
FOR EACH ROW EXECUTE FUNCTION cartograph_refuse_change();

-- One working copy per manifest, replaced whole on every save.
CREATE TABLE working_copies (
    kind  text        NOT NULL,
    id    text        NOT NULL,
    name  text        NOT NULL DEFAULT '',
    yaml  text        NOT NULL,
    on_ts timestamptz NOT NULL,
    PRIMARY KEY (kind, id)
);

-- The outgoing references of each manifest's current content.
CREATE TABLE manifest_references (
    from_kind text NOT NULL,
    from_id   text NOT NULL,
    path      text NOT NULL,
    to_kind   text NOT NULL,
    to_id     text NOT NULL,
    PRIMARY KEY (from_kind, from_id, path)
);

CREATE INDEX manifest_references_to ON manifest_references (to_kind, to_id);

CREATE TABLE exclusions (
    kind     text        NOT NULL,
    id       text        NOT NULL,
    name     text        NOT NULL,
    on_ts    timestamptz NOT NULL,
    reason   text        NOT NULL,
    operator text        NOT NULL,
    PRIMARY KEY (kind, id)
);

-- Project state history, append-only; seq breaks ties between rows
-- with the same time.
CREATE TABLE project_state (
    seq        bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id text        NOT NULL,
    state      text        NOT NULL,
    actor      text        NOT NULL,
    reason     text        NOT NULL DEFAULT '',
    on_ts      timestamptz NOT NULL,
    snapshot   integer     NOT NULL DEFAULT 0,
    bundle     text        NOT NULL DEFAULT ''
);

CREATE INDEX project_state_project ON project_state (project_id, on_ts, seq);

CREATE TRIGGER project_state_append_only
BEFORE UPDATE OR DELETE ON project_state
FOR EACH ROW EXECUTE FUNCTION cartograph_refuse_change();

-- Handoff bundles: the files a handoff rendered, beside the version
-- they came from.
CREATE TABLE bundle_files (
    location text        NOT NULL,
    name     text        NOT NULL,
    content  bytea       NOT NULL,
    on_ts    timestamptz NOT NULL,
    PRIMARY KEY (location, name)
);

-- Shared drafts as CRDT documents (docs/adr/0007): a snapshot that
-- includes every chunk up to compacted_seq, and the chunks after it.
-- next_seq is the last sequence number handed out; appenders take it
-- under the row's lock, which is what keeps sequence numbers rising.
CREATE TABLE documents (
    doc_id        text   PRIMARY KEY,
    kind          text   NOT NULL,
    id            text   NOT NULL,
    snapshot      bytea  NOT NULL,
    compacted_seq bigint NOT NULL DEFAULT 0,
    next_seq      bigint NOT NULL DEFAULT 0,
    UNIQUE (kind, id)
);

CREATE TABLE document_chunks (
    doc_id text   NOT NULL REFERENCES documents (doc_id) ON DELETE CASCADE,
    seq    bigint NOT NULL,
    data   bytea  NOT NULL,
    PRIMARY KEY (doc_id, seq)
);
