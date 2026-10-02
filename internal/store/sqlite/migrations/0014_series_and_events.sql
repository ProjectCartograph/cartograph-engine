-- Series items and events (docs/adr/0013), as Postgres keeps them, so a
-- vault offers every function a database does. Like everything in the
-- index they are a cache of what the files say: a rebuild starts their
-- history again.

CREATE TABLE IF NOT EXISTS series_items (
    seq         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind        TEXT NOT NULL,
    id          TEXT NOT NULL,
    series      TEXT NOT NULL,
    key         TEXT NOT NULL,
    item        TEXT,
    recorded_at TEXT NOT NULL,
    recorded_by TEXT NOT NULL,
    reason      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS series_items_as_of ON series_items (kind, series, id, key, recorded_at, seq);

-- One writer at a time, so a row's seq is the order of commits and a
-- cursor needs no more care than that.
CREATE TABLE IF NOT EXISTS events (
    seq    INTEGER PRIMARY KEY AUTOINCREMENT,
    at     TEXT    NOT NULL,
    type   TEXT    NOT NULL,
    kind   TEXT    NOT NULL,
    id     TEXT    NOT NULL,
    number INTEGER NOT NULL DEFAULT 0,
    detail TEXT    NOT NULL DEFAULT '',
    actor  TEXT    NOT NULL DEFAULT ''
);

-- A file edited by hand becomes a version when the vault reads it, so it
-- is an event too.
CREATE TRIGGER IF NOT EXISTS manifest_versions_event AFTER INSERT ON manifest_versions
BEGIN
    INSERT INTO events (at, type, kind, id, number, actor) VALUES (NEW.on_ts, 'version', NEW.kind, NEW.id, NEW.number, NEW.actor);
END;

CREATE TRIGGER IF NOT EXISTS project_state_event AFTER INSERT ON project_state
BEGIN
    INSERT INTO events (at, type, kind, id, detail, actor) VALUES (NEW.on_ts, 'state', 'Project', NEW.project_id, NEW.state, NEW.actor);
END;

CREATE TRIGGER IF NOT EXISTS series_items_event AFTER INSERT ON series_items
BEGIN
    INSERT INTO events (at, type, kind, id, detail, actor) VALUES (NEW.recorded_at, 'series', NEW.kind, NEW.id, NEW.key, NEW.recorded_by);
END;
