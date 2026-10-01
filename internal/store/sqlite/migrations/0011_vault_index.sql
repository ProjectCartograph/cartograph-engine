-- I4.2: vault_files holds the per-file SHA-256 hash the vault compares
-- against on open and on a watcher event. Previously created ad hoc by
-- the vault package itself (CREATE TABLE IF NOT EXISTS on every open);
-- moved here so the vault depends on the store.VaultIndex port instead of
-- SQLite directly. Shape is unchanged, so an existing .cartograph/index.sqlite
-- keeps working.
CREATE TABLE IF NOT EXISTS vault_files (
    kind   TEXT NOT NULL,
    id     TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    mtime  TEXT NOT NULL,
    PRIMARY KEY (kind, id)
);
