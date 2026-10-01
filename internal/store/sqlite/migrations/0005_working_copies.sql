-- working_copies stores autosave copies (manifest's working copy, not versioned).
-- Replaces manifest_draft from operational store.
CREATE TABLE working_copies (
    kind  TEXT NOT NULL,
    id    TEXT NOT NULL,
    yaml  TEXT NOT NULL,
    name  TEXT NOT NULL,
    on_ts TEXT NOT NULL,
    PRIMARY KEY (kind, id)
);
