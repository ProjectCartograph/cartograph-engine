-- The standard reports as views, for SQL and BI tools (docs/adr/0014):
-- read-only, computed on read over the store's tables. This adapter
-- creates them when a deployment chooses CARTOGRAPH_REPORTS=postgres,
-- and replaces them at every start; the store itself never does. The
-- reporting conformance suite holds them to the computed reports.

-- A manifest as a person sees it now: its working copy where there is
-- one, else its latest version.
CREATE OR REPLACE VIEW report_current AS
SELECT kind, id, coalesce(working, doc) AS doc,
       CASE WHEN has_working THEN 0 ELSE version END AS version,
       CASE WHEN has_working THEN working_on ELSE updated_on END AS updated_on
FROM manifests;

CREATE OR REPLACE VIEW report_teams AS
WITH RECURSIVE team AS (
    SELECT id, coalesce(nullif(doc #>> '{metadata,name}', ''), doc #>> '{spec,name}', '') AS name,
           coalesce(doc #>> '{spec,parent}', '') AS parent
    FROM report_current WHERE kind = 'Team'
), up (team, above, depth, at, path) AS (
    SELECT id, parent, 1, parent, ARRAY[id] FROM team WHERE parent <> ''
    UNION ALL
    SELECT up.team, t.parent, up.depth + 1, t.parent, up.path || up.at
    FROM up JOIN team t ON t.id = up.at
    WHERE t.parent <> '' AND NOT t.parent = ANY (up.path || up.at) AND up.depth < 64
)
SELECT t.id AS team, t.name, t.parent,
       coalesce((SELECT string_agg(u.at, ' ' ORDER BY u.depth) FROM up u WHERE u.team = t.id), '') AS above
FROM team t;

CREATE OR REPLACE VIEW report_projects AS
SELECT p.id, coalesce(p.doc #>> '{metadata,name}', '') AS name,
       coalesce(p.doc #>> '{spec,team}', '') AS team,
       coalesce(t.name, '') AS team_name,
       coalesce((SELECT s.state FROM project_state s WHERE s.project_id = p.id ORDER BY s.on_ts DESC, s.seq DESC LIMIT 1), 'draft') AS state,
       p.version, p.updated_on,
       coalesce((SELECT string_agg(r.to_id, ' ' ORDER BY r.to_id) FROM manifest_references r
                 WHERE r.from_kind = 'Project' AND r.from_id = p.id AND r.to_kind = 'Goal'), '') AS goals
FROM report_current p
LEFT JOIN report_teams t ON t.team = p.doc #>> '{spec,team}'
WHERE p.kind = 'Project';

-- Readings in force: a series' working copy where it has one (as every
-- report shows the record as people see it now), else its saved items.
-- Who recorded a reading, and when, is its row's where the row agrees.
CREATE OR REPLACE VIEW report_kpi_readings AS
WITH saved AS (
    SELECT DISTINCT ON (id, key) id, key, item, recorded_at, recorded_by FROM series_items
    WHERE kind = 'KPIReadings' AND series = '/spec/readings'
    ORDER BY id, key, recorded_at DESC, seq DESC
), current AS (
    SELECT m.id, e.item ->> 'period' AS key, e.item, m.working_on AS at
    FROM manifests m, jsonb_array_elements(m.working -> 'spec' -> 'readings') AS e(item)
    WHERE m.kind = 'KPIReadings' AND m.has_working
    UNION ALL
    SELECT s.id, s.key, s.item, NULL
    FROM saved s JOIN manifests m ON m.kind = 'KPIReadings' AND m.id = s.id AND NOT m.has_working
    WHERE s.item IS NOT NULL
)
SELECT coalesce(c.doc #>> '{spec,kpi}', '') AS kpi,
       coalesce(k.title, '') AS kpi_name,
       r.id AS readings, r.key AS period,
       r.item -> 'value' AS value,
       coalesce((r.item ->> 'provisional')::boolean, false) AS provisional,
       coalesce(r.item ->> 'note', '') AS note,
       CASE WHEN s.item = r.item THEN s.recorded_at ELSE r.at END AS recorded_at,
       CASE WHEN s.item = r.item THEN s.recorded_by ELSE 'local' END AS recorded_by
FROM current r
JOIN report_current c ON c.kind = 'KPIReadings' AND c.id = r.id
LEFT JOIN saved s ON s.id = r.id AND s.key = r.key
LEFT JOIN manifests k ON k.kind = 'KPI' AND k.id = c.doc #>> '{spec,kpi}';

CREATE OR REPLACE VIEW report_alignment AS
SELECT g.id AS goal, g.title AS goal_name, m.kind, m.id, m.title AS name
FROM manifest_references r
JOIN manifests g ON g.kind = 'Goal' AND g.id = r.to_id
JOIN manifests m ON m.kind = r.from_kind AND m.id = r.from_id
WHERE r.to_kind = 'Goal'
GROUP BY g.id, g.title, m.kind, m.id, m.title;
