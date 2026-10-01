-- project_state is append-only: one row per state transition. The current
-- state of a project is its most recent row (by on_ts, then rowid to break
-- ties); a project with no rows is implicitly "draft".
CREATE TABLE project_state (
    project_id TEXT    NOT NULL,
    state      TEXT    NOT NULL,
    actor      TEXT    NOT NULL,
    reason     TEXT    NOT NULL DEFAULT '',
    on_ts      TEXT    NOT NULL
);

CREATE INDEX idx_project_state_project ON project_state (project_id);

CREATE TRIGGER trg_project_state_no_update
BEFORE UPDATE ON project_state
BEGIN
    SELECT RAISE(ABORT, 'project state history is append-only: update not allowed');
END;

CREATE TRIGGER trg_project_state_no_delete
BEFORE DELETE ON project_state
BEGIN
    SELECT RAISE(ABORT, 'project state history is append-only: delete not allowed');
END;

-- project_draft holds at most one working-copy row per project, replaced
-- whole on every draft save.
CREATE TABLE project_draft (
    project_id TEXT PRIMARY KEY,
    yaml       TEXT NOT NULL,
    actor      TEXT NOT NULL,
    on_ts      TEXT NOT NULL
);
