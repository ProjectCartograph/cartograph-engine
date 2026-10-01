-- I3.7a: Apply journal for vault mutations, and vault_meta to detect stale index.
-- Drop deleted_manifests table (replaced by exclusion in vault.yaml).
DROP TABLE IF EXISTS deleted_manifests;

-- Record of every mutation: write unit (applied=false), write files, mark applied,
-- update index in one transaction. On open, replay every unit not applied.
CREATE TABLE apply_journal (
    id       TEXT PRIMARY KEY,
    "on"     TEXT NOT NULL,
    operator TEXT NOT NULL,
    reason   TEXT NOT NULL,
    files    TEXT NOT NULL,  -- JSON array of {path, sha256, content}
    applied  INTEGER NOT NULL DEFAULT 0  -- 0=false, 1=true
);

-- Track vault.yaml hash to detect stale index on open.
CREATE TABLE vault_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
