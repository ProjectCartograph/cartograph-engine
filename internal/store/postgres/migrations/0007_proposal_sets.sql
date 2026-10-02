-- The checks an agent left open on a proposal, each with its reason
-- (docs/adr/0016), as a JSON array.
ALTER TABLE proposals ADD COLUMN waivers text NOT NULL DEFAULT '[]';

-- Proposals that stand or fall together, and the order they are saved in.
ALTER TABLE proposals ADD COLUMN set_id text NOT NULL DEFAULT '';
ALTER TABLE proposals ADD COLUMN set_index integer NOT NULL DEFAULT 0;
CREATE INDEX proposals_set ON proposals (set_id) WHERE set_id <> '';
