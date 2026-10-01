-- Shared drafts as CRDT documents (docs/adr/0007): a snapshot that
-- includes every chunk up to compacted_seq, and the chunks after it.
-- next_seq is the last sequence number handed out. Unlike the rest of
-- the index this is not rebuildable from the files: it is the draft
-- between versions, and losing it loses only unsaved edits.
CREATE TABLE IF NOT EXISTS documents (
    doc_id        TEXT    PRIMARY KEY,
    kind          TEXT    NOT NULL,
    id            TEXT    NOT NULL,
    snapshot      BLOB    NOT NULL,
    compacted_seq INTEGER NOT NULL DEFAULT 0,
    next_seq      INTEGER NOT NULL DEFAULT 0,
    UNIQUE (kind, id)
);

CREATE TABLE IF NOT EXISTS document_chunks (
    doc_id TEXT    NOT NULL REFERENCES documents (doc_id) ON DELETE CASCADE,
    seq    INTEGER NOT NULL,
    data   BLOB    NOT NULL,
    PRIMARY KEY (doc_id, seq)
);
