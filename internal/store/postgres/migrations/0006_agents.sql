-- Agents and their proposals (docs/adr/0016).

-- An administrator may turn one person's agents off.
ALTER TABLE people ADD COLUMN agents_off boolean NOT NULL DEFAULT false;

-- What agents proposed for their people to confirm, kept exactly as
-- proposed, with the version it was proposed against.
CREATE TABLE proposals (
    id              text        PRIMARY KEY,
    kind            text        NOT NULL,
    manifest_id     text        NOT NULL,
    op              text        NOT NULL,
    text            text,
    series          text        NOT NULL DEFAULT '',
    item            jsonb,
    state           text        NOT NULL DEFAULT '',
    base            integer     NOT NULL DEFAULT 0,
    reason          text        NOT NULL DEFAULT '',
    agent           text        NOT NULL,
    for_person      text        NOT NULL DEFAULT '',
    at              timestamptz NOT NULL,
    status          text        NOT NULL DEFAULT 'open',
    decided_by      text        NOT NULL DEFAULT '',
    decided_at      timestamptz,
    decision_reason text        NOT NULL DEFAULT '',
    version         integer     NOT NULL DEFAULT 0
);

CREATE INDEX proposals_for ON proposals (for_person, status, at DESC);
CREATE INDEX proposals_manifest ON proposals (kind, manifest_id, at DESC);

-- Proposed and decided are events, like everything else that happens.
CREATE FUNCTION cartograph_proposal_event() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO events (at, type, kind, id, detail, actor) VALUES (NEW.at, 'proposal', NEW.kind, NEW.manifest_id, NEW.op, NEW.agent);
    ELSIF NEW.status <> OLD.status THEN
        INSERT INTO events (at, type, kind, id, detail, actor) VALUES (NEW.decided_at, 'proposal', NEW.kind, NEW.manifest_id, NEW.status, NEW.decided_by);
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER proposals_event AFTER INSERT OR UPDATE ON proposals
FOR EACH ROW EXECUTE FUNCTION cartograph_proposal_event();

-- What people issued their agents (the tokens themselves are signed and
-- never stored), so they can be listed and revoked.
CREATE TABLE agent_grants (
    id          text        PRIMARY KEY,
    email       text        NOT NULL,
    label       text        NOT NULL,
    created_at  timestamptz NOT NULL,
    expires_at  timestamptz NOT NULL,
    last_used   timestamptz,
    revoked_at  timestamptz,
    generation  integer     NOT NULL DEFAULT 0
);

CREATE INDEX agent_grants_email ON agent_grants (email, created_at DESC);
