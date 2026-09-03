-- Migration 0006 — explicit binds + blocks inheritance (SQLite).
ALTER TABLE statements ADD COLUMN binds_id TEXT REFERENCES issues(id);
CREATE INDEX IF NOT EXISTS idx_statements_binds_id ON statements(binds_id);
DROP VIEW IF EXISTS resolved_statements;
CREATE VIEW resolved_statements AS
WITH RECURSIVE ancestor_tree(target_id, origin_id, depth) AS (
    SELECT id, id, 0 FROM issues
    UNION ALL
    SELECT t.target_id, d.depends_on_id, t.depth + 1
    FROM ancestor_tree t
    JOIN dependencies d ON d.issue_id = t.origin_id
    WHERE d.type = 'parent-child' AND t.depth < 8
),
deduped AS (
    SELECT target_id, origin_id, MIN(depth) AS depth
    FROM ancestor_tree
    GROUP BY target_id, origin_id
),
active_statements AS (
    SELECT s.* FROM statements s
    WHERE s.status = 'active'
      AND NOT EXISTS (SELECT 1 FROM statements s2 WHERE s2.supersedes_id = s.id)
)
SELECT
    d.target_id AS issue_id,
    s.id AS statement_id,
    s.kind,
    s.text,
    s.created_at,
    s.filed_by,
    s.evidence,
    'self' AS origin_kind,
    NULL AS origin_issue_id,
    0 AS depth
FROM deduped d
JOIN active_statements s ON s.issue_id = d.origin_id AND d.depth = 0
UNION ALL
SELECT
    d.target_id AS issue_id,
    s.id AS statement_id,
    s.kind,
    s.text,
    s.created_at,
    s.filed_by,
    s.evidence,
    'epic' AS origin_kind,
    s.issue_id AS origin_issue_id,
    d.depth AS depth
FROM deduped d
JOIN active_statements s ON s.issue_id = d.origin_id
WHERE d.depth > 0 AND d.depth <= 8
  AND s.kind = 'ruling'
  AND s.scope != 'self'
UNION ALL
SELECT
    i.id AS issue_id,
    s.id AS statement_id,
    s.kind,
    s.text,
    s.created_at,
    s.filed_by,
    s.evidence,
    'project' AS origin_kind,
    NULL AS origin_issue_id,
    NULL AS depth
FROM issues i
CROSS JOIN active_statements s
WHERE s.issue_id IS NULL
  AND s.kind = 'ruling'
UNION ALL
SELECT
    d.issue_id AS issue_id,
    s.id AS statement_id,
    s.kind,
    s.text,
    s.created_at,
    s.filed_by,
    s.evidence,
    'blocks' AS origin_kind,
    s.issue_id AS origin_issue_id,
    1 AS depth
FROM dependencies d
JOIN active_statements s ON s.issue_id = d.depends_on_id
WHERE d.type = 'blocks' AND s.kind = 'ruling' AND s.scope != 'self'
UNION ALL
SELECT
    s.binds_id AS issue_id,
    s.id AS statement_id,
    s.kind,
    s.text,
    s.created_at,
    s.filed_by,
    s.evidence,
    'binds' AS origin_kind,
    s.issue_id AS origin_issue_id,
    0 AS depth
FROM active_statements s
WHERE s.binds_id IS NOT NULL AND s.kind = 'ruling';
