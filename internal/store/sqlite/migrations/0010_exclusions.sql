-- I3.7a: Exclusion tracking for manifests excluded from vault.yaml.
-- Tracks which manifests have been excluded, when, why, and by whom.
CREATE TABLE IF NOT EXISTS exclusions (
    kind     TEXT NOT NULL,
    id       TEXT NOT NULL,
    name     TEXT NOT NULL,           -- Name at time of exclusion
    "on"     TEXT NOT NULL,            -- When excluded
    reason   TEXT NOT NULL,            -- Why excluded
    operator TEXT NOT NULL,            -- Who excluded it
    PRIMARY KEY (kind, id)
);
