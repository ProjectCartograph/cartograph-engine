-- Track deleted manifests (I3.6): a lightweight marker so GET returns 404 for deleted
-- manifests, while version history is kept readable in ListVersions and snapshots.
CREATE TABLE deleted_manifests (
    kind   TEXT NOT NULL,
    id     TEXT NOT NULL,
    PRIMARY KEY (kind, id)
);

-- Drop the old manifest_tombstone table (replaced by the simpler approach above).
DROP TABLE IF EXISTS manifest_tombstone;
