-- Series items and events (docs/adr/0013). Both are merge-on-read:
-- appended once, combined by whoever reads them.

-- One row per item recorded in a series (a KPI's readings): what was
-- recorded, when and by whom. A period's value at any time is the row
-- recorded last for it by then; a null item records a removal.
CREATE TABLE series_items (
    seq         bigint      GENERATED ALWAYS AS IDENTITY,
    kind        text        NOT NULL,
    id          text        NOT NULL,
    series      text        NOT NULL,
    key         text        NOT NULL,
    item        jsonb,
    recorded_at timestamptz NOT NULL,
    recorded_by text        NOT NULL,
    reason      text        NOT NULL DEFAULT '',
    PRIMARY KEY (kind, series, id, key, seq)
);

CREATE INDEX series_items_as_of ON series_items (kind, series, id, key, recorded_at DESC, seq DESC);

CREATE TRIGGER series_items_append_only
BEFORE UPDATE OR DELETE ON series_items
FOR EACH ROW EXECUTE FUNCTION cartograph_refuse_change();

-- Everything that happened, in order: written by triggers, so every
-- writer writes them, in the transaction of the change. tx lets a
-- reader take only events older than any transaction still open, so a
-- cursor never steps past an event that commits late.
CREATE TABLE events (
    seq    bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tx     xid8        NOT NULL DEFAULT pg_current_xact_id(),
    at     timestamptz NOT NULL,
    type   text        NOT NULL,
    kind   text        NOT NULL,
    id     text        NOT NULL,
    number integer     NOT NULL DEFAULT 0,
    detail text        NOT NULL DEFAULT '',
    actor  text        NOT NULL DEFAULT ''
);

CREATE TRIGGER events_append_only
BEFORE UPDATE OR DELETE ON events
FOR EACH ROW EXECUTE FUNCTION cartograph_refuse_change();

CREATE FUNCTION cartograph_event() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    CASE TG_TABLE_NAME
    WHEN 'manifest_versions' THEN
        INSERT INTO events (at, type, kind, id, number, actor)
        VALUES (NEW.on_ts, 'version', NEW.kind, NEW.id, NEW.number, NEW.actor);
    WHEN 'project_state' THEN
        INSERT INTO events (at, type, kind, id, detail, actor)
        VALUES (NEW.on_ts, 'state', 'Project', NEW.project_id, NEW.state, NEW.actor);
    WHEN 'series_items' THEN
        INSERT INTO events (at, type, kind, id, detail, actor)
        VALUES (NEW.recorded_at, 'series', NEW.kind, NEW.id, NEW.key, NEW.recorded_by);
    END CASE;
    RETURN NULL;
END;
$$;

CREATE TRIGGER manifest_versions_event AFTER INSERT ON manifest_versions
FOR EACH ROW EXECUTE FUNCTION cartograph_event();
CREATE TRIGGER project_state_event AFTER INSERT ON project_state
FOR EACH ROW EXECUTE FUNCTION cartograph_event();
CREATE TRIGGER series_items_event AFTER INSERT ON series_items
FOR EACH ROW EXECUTE FUNCTION cartograph_event();
