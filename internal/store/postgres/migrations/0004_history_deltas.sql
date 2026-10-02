-- History as deltas (docs/adr/0013). A version that is no longer a
-- manifest's latest, and is older than the rollout grace period, keeps
-- either its whole document (a keyframe, at least every 16 versions)
-- or the JSON Patch from the version before it, and no text. A
-- manifest's history then grows with what changed, not with its size
-- times its versions. The latest version always keeps its text and
-- document, so a replica of the previous release reads it unchanged.

ALTER TABLE manifest_versions ADD COLUMN patch jsonb;
ALTER TABLE manifest_versions ALTER COLUMN yaml DROP NOT NULL;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_settings WHERE name = 'default_toast_compression' AND 'lz4' = ANY (enumvals)) THEN
        ALTER TABLE manifest_versions ALTER COLUMN patch SET COMPRESSION lz4;
    END IF;
END
$$;

-- Stale means no document and no patch: a compacted version is not.
DROP INDEX manifest_versions_stale;
CREATE INDEX manifest_versions_stale ON manifest_versions (kind, id, number) WHERE doc IS NULL AND patch IS NULL;

-- What compaction has still to look at: versions with their text.
CREATE INDEX manifest_versions_full ON manifest_versions (on_ts) WHERE yaml IS NOT NULL;

-- History stays append-only, but for two changes: giving a document to
-- a version stored without one, and compacting a version that is not
-- its manifest's latest into a keyframe or a patch.
CREATE OR REPLACE FUNCTION cartograph_version_changed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND (NEW.kind, NEW.id, NEW.number, NEW.name, NEW.actor, NEW.reason, NEW.on_ts)
           IS NOT DISTINCT FROM (OLD.kind, OLD.id, OLD.number, OLD.name, OLD.actor, OLD.reason, OLD.on_ts) THEN
        -- A document for a version stored as text alone.
        IF OLD.doc IS NULL AND OLD.patch IS NULL AND NEW.doc IS NOT NULL AND NEW.patch IS NULL
           AND NEW.yaml IS NOT DISTINCT FROM OLD.yaml THEN
            UPDATE manifests SET doc = NEW.doc
            WHERE kind = NEW.kind AND id = NEW.id AND version = NEW.number;
            RETURN NEW;
        END IF;
        -- Compaction: text dropped, and either the document kept or a
        -- patch in its place; never the latest version.
        IF OLD.yaml IS NOT NULL AND NEW.yaml IS NULL AND OLD.patch IS NULL
           AND ((NEW.doc IS NOT NULL AND NEW.patch IS NULL) OR (NEW.doc IS NULL AND NEW.patch IS NOT NULL))
           AND NEW.number < (SELECT version FROM manifests WHERE kind = NEW.kind AND id = NEW.id) THEN
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION '% is append-only: % not allowed', TG_TABLE_NAME, lower(TG_OP);
END;
$$;
