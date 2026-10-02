-- Manifests as documents (docs/adr/0012). Every version and working
-- copy keeps its document as jsonb, and one row per manifest says what
-- it is now, so a read costs what it returns and not the size of the
-- history.
--
-- This is the expand step of an expand-then-contract change. The text
-- columns stay, written by every writer, so a replica of the previous
-- release keeps working during a rolling upgrade; the triggers below
-- keep the new tables right whichever release wrote. A later release
-- drops the text.

ALTER TABLE manifest_versions ADD COLUMN doc jsonb;
ALTER TABLE working_copies ADD COLUMN doc jsonb;

-- History compresses better with lz4, where the server has it.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_settings WHERE name = 'default_toast_compression' AND 'lz4' = ANY (enumvals)) THEN
        ALTER TABLE manifest_versions ALTER COLUMN doc SET COMPRESSION lz4;
        ALTER TABLE manifest_versions ALTER COLUMN yaml SET COMPRESSION lz4;
    END IF;
END
$$;

-- The document of a text written as JSON; null for any other text,
-- which a replica of this release decodes with the deployment's codec.
CREATE FUNCTION cartograph_text_doc(t text) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE AS $$
BEGIN
    IF left(ltrim(t), 1) <> '{' THEN
        RETURN NULL;
    END IF;
    RETURN t::jsonb;
EXCEPTION WHEN others THEN
    RETURN NULL;
END;
$$;

-- One row per manifest: its highest version (0 for none) with that
-- version's name and document, and its working copy's. What a list
-- shows (title, labels, team) is derived from them and indexed.
CREATE TABLE manifests (
    kind         text        NOT NULL,
    id           text        NOT NULL,
    version      integer     NOT NULL DEFAULT 0,
    name         text        NOT NULL DEFAULT '',
    doc          jsonb,
    updated_on   timestamptz,
    working      jsonb,
    has_working  boolean     NOT NULL DEFAULT false,
    working_name text,
    working_on   timestamptz,
    title        text  GENERATED ALWAYS AS (CASE WHEN version > 0 THEN name ELSE coalesce(working_name, '') END) STORED,
    labels       jsonb GENERATED ALWAYS AS (CASE WHEN version > 0 THEN doc ELSE working END -> 'metadata' -> 'labels') STORED,
    team         text  GENERATED ALWAYS AS (coalesce(doc #>> '{spec,team}', doc #>> '{spec,leadTeam}')) STORED,
    PRIMARY KEY (kind, id)
);

CREATE INDEX manifests_team ON manifests (kind, team) WHERE team IS NOT NULL;
CREATE INDEX manifests_doc ON manifests USING gin (doc jsonb_path_ops);

-- History stays append-only, but for one change: giving a document to
-- a version that was stored without one.
CREATE FUNCTION cartograph_version_changed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.doc IS NULL AND NEW.doc IS NOT NULL
       AND (NEW.kind, NEW.id, NEW.number, NEW.name, NEW.yaml, NEW.actor, NEW.reason, NEW.on_ts)
           IS NOT DISTINCT FROM (OLD.kind, OLD.id, OLD.number, OLD.name, OLD.yaml, OLD.actor, OLD.reason, OLD.on_ts) THEN
        UPDATE manifests SET doc = NEW.doc
        WHERE kind = NEW.kind AND id = NEW.id AND version = NEW.number;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION '% is append-only: % not allowed', TG_TABLE_NAME, lower(TG_OP);
END;
$$;

DROP TRIGGER manifest_versions_immutable ON manifest_versions;
CREATE TRIGGER manifest_versions_immutable
BEFORE UPDATE OR DELETE ON manifest_versions
FOR EACH ROW EXECUTE FUNCTION cartograph_version_changed();

-- Backfill from what the previous release wrote. Text written as JSON
-- becomes its document here; YAML waits for a replica to decode it.
UPDATE manifest_versions SET doc = cartograph_text_doc(yaml) WHERE doc IS NULL AND cartograph_text_doc(yaml) IS NOT NULL;
UPDATE working_copies SET doc = cartograph_text_doc(yaml) WHERE doc IS NULL AND cartograph_text_doc(yaml) IS NOT NULL;

INSERT INTO manifests (kind, id, version, name, doc, updated_on)
SELECT DISTINCT ON (kind, id) kind, id, number, name, doc, on_ts
FROM manifest_versions ORDER BY kind, id, number DESC;

INSERT INTO manifests AS m (kind, id, working, has_working, working_name, working_on)
SELECT kind, id, doc, true, name, on_ts FROM working_copies
ON CONFLICT (kind, id) DO UPDATE
SET working = excluded.working, has_working = true, working_name = excluded.working_name, working_on = excluded.working_on;

-- Name search: trigram indexes where the server lets this database
-- create pg_trgm (a trusted extension since Postgres 13). Without them
-- a search reads the kind's rows, which is still bounded by the kind.
DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA public;
    CREATE INDEX manifests_title_trgm ON manifests USING gin (lower(title) public.gin_trgm_ops);
    CREATE INDEX manifests_id_trgm ON manifests USING gin (lower(id) public.gin_trgm_ops);
EXCEPTION WHEN others THEN
    RAISE NOTICE 'pg_trgm unavailable (%); name search scans the kind', SQLERRM;
END
$$;

-- A version is accepted only as one more than the manifest's row says,
-- with that row locked: one indexed row decides a race, for every
-- writer. The row then moves to the new version.
CREATE FUNCTION cartograph_version_inserted() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    cur integer;
BEGIN
    IF NEW.doc IS NULL THEN
        NEW.doc := cartograph_text_doc(NEW.yaml);
    END IF;
    SELECT version INTO cur FROM manifests WHERE kind = NEW.kind AND id = NEW.id FOR UPDATE;
    IF coalesce(cur, 0) + 1 <> NEW.number THEN
        RAISE EXCEPTION 'version % of %/% is not one more than the current version %',
            NEW.number, NEW.kind, NEW.id, coalesce(cur, 0)
            USING ERRCODE = 'CG001';
    END IF;
    INSERT INTO manifests (kind, id, version, name, doc, updated_on)
    VALUES (NEW.kind, NEW.id, NEW.number, NEW.name, NEW.doc, NEW.on_ts)
    ON CONFLICT (kind, id) DO UPDATE
    SET version = excluded.version, name = excluded.name, doc = excluded.doc, updated_on = excluded.updated_on;
    RETURN NEW;
END;
$$;

CREATE TRIGGER manifest_versions_inserted
BEFORE INSERT ON manifest_versions
FOR EACH ROW EXECUTE FUNCTION cartograph_version_inserted();

-- A working copy written or replaced: the manifest's row carries it.
CREATE FUNCTION cartograph_working_written() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.doc IS NULL THEN
        NEW.doc := cartograph_text_doc(NEW.yaml);
    END IF;
    INSERT INTO manifests (kind, id, working, has_working, working_name, working_on)
    VALUES (NEW.kind, NEW.id, NEW.doc, true, NEW.name, NEW.on_ts)
    ON CONFLICT (kind, id) DO UPDATE
    SET working = excluded.working, has_working = true, working_name = excluded.working_name, working_on = excluded.working_on;
    RETURN NEW;
END;
$$;

CREATE TRIGGER working_copies_written
BEFORE INSERT OR UPDATE ON working_copies
FOR EACH ROW EXECUTE FUNCTION cartograph_working_written();

-- A working copy discarded: a manifest that was never saved is gone.
CREATE FUNCTION cartograph_working_discarded() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM manifests WHERE kind = OLD.kind AND id = OLD.id AND version = 0;
    UPDATE manifests SET working = NULL, has_working = false, working_name = NULL, working_on = NULL
    WHERE kind = OLD.kind AND id = OLD.id;
    RETURN OLD;
END;
$$;

CREATE TRIGGER working_copies_discarded
AFTER DELETE ON working_copies
FOR EACH ROW EXECUTE FUNCTION cartograph_working_discarded();

-- What still waits for a document, kept small by being partial.
CREATE INDEX manifest_versions_stale ON manifest_versions (kind, id, number) WHERE doc IS NULL;
CREATE INDEX working_copies_stale ON working_copies (kind, id) WHERE doc IS NULL;

-- A reference belongs to a manifest that exists. NOT VALID: the rows
-- already there are not checked, only what is written from now on.
ALTER TABLE manifest_references
    ADD CONSTRAINT manifest_references_from FOREIGN KEY (from_kind, from_id)
    REFERENCES manifests (kind, id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED NOT VALID;
