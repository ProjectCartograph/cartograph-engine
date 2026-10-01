-- Add snapshot and bundle columns to project_state for handoff tracking
-- snapshot: version number of the snapshot that was handed off
-- bundle: directory path where the charter bundle was written (relative to vault root)
ALTER TABLE project_state ADD COLUMN snapshot INTEGER DEFAULT 0;
ALTER TABLE project_state ADD COLUMN bundle TEXT DEFAULT '';
