-- What a change set's item does to its record (docs/adr/0024): saves the
-- draft (the default, as every item did before), deletes the record, or
-- moves a project to another state.
ALTER TABLE change_items ADD COLUMN IF NOT EXISTS op text NOT NULL DEFAULT '';
ALTER TABLE change_items ADD COLUMN IF NOT EXISTS state text NOT NULL DEFAULT '';
