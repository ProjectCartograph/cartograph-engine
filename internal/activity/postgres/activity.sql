-- The people's trace (docs/adr/0034): one row an act, appended and never
-- changed, the act whole as JSON under its OpenTelemetry names. When and
-- whose are columns of their own, for the reads by window and person.
CREATE TABLE IF NOT EXISTS cartograph_activity (
    id     bigserial PRIMARY KEY,
    at     timestamptz NOT NULL,
    person text NOT NULL DEFAULT '',
    act    jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS cartograph_activity_at ON cartograph_activity (at);
CREATE INDEX IF NOT EXISTS cartograph_activity_person_at ON cartograph_activity (person, at);
