-- What an agent decided on its person's behalf, with no document and no
-- answer behind it (docs/adr/0033): kept with the change set, as JSON, so
-- the person reviews each one.
ALTER TABLE change_sets ADD COLUMN assumptions TEXT NOT NULL DEFAULT '[]';
