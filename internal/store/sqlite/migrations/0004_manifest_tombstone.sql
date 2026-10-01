-- manifest_tombstone (I3.2): a soft delete, one row per deleted (kind, id).
-- Every committed version stays in manifest_versions untouched (that
-- table's own immutability triggers are unaffected by this migration); a
-- tombstone only stops the engine from listing, picking or resolving
-- kind/id as a reference target going forward. Used so far only for Goal
-- ("delete a goal", allowed only when nothing references it).
CREATE TABLE manifest_tombstone (
    kind   TEXT NOT NULL,
    id     TEXT NOT NULL,
    actor  TEXT NOT NULL,
    reason TEXT NOT NULL,
    on_ts  TEXT NOT NULL,
    PRIMARY KEY (kind, id)
);
