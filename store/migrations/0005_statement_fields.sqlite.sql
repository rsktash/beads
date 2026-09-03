-- Migration 0005 — statement authority fields + retired status (SQLite).
-- The status CHECK cannot be altered in place, so the table is rebuilt. The two
-- self-referencing FKs are DEFERRABLE so one INSERT...SELECT can carry a chain.
-- resolved_statements is dropped before the rebuild and recreated after: SQLite
-- re-validates every view's schema on ALTER TABLE ... RENAME TO, and the view
-- still names the old `statements` table at that point.
DROP VIEW IF EXISTS resolved_statements;
CREATE TABLE IF NOT EXISTS statements_new (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('ruling','question','finding')),
    issue_id TEXT REFERENCES issues(id) ON DELETE CASCADE,
    text TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    filed_by TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','superseded','answered','retracted','candidate','retired')),
    scope TEXT NOT NULL DEFAULT 'inherit' CHECK (scope IN ('inherit','self')),
    supersedes_id TEXT REFERENCES statements_new(id) DEFERRABLE INITIALLY DEFERRED,
    answered_by TEXT REFERENCES statements_new(id) DEFERRABLE INITIALLY DEFERRED,
    source_comment_id TEXT REFERENCES comments(id) ON DELETE SET NULL,
    evidence TEXT NOT NULL DEFAULT '',
    topic TEXT NOT NULL DEFAULT '',
    workspace TEXT NOT NULL DEFAULT '',
    concern TEXT NOT NULL DEFAULT '',
    law TEXT NOT NULL DEFAULT '',
    rationale TEXT NOT NULL DEFAULT '',
    verbatim TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    msg_id TEXT NOT NULL DEFAULT '',
    tool_use_id TEXT NOT NULL DEFAULT '',
    retire_note TEXT NOT NULL DEFAULT '',
    changed_at TIMESTAMP
);
INSERT INTO statements_new (id, kind, issue_id, text, created_at, filed_by, status, scope, supersedes_id, answered_by, source_comment_id, evidence)
SELECT id, kind, issue_id, text, created_at, filed_by, status, scope, supersedes_id, answered_by, source_comment_id, evidence FROM statements;
DROP TABLE statements;
ALTER TABLE statements_new RENAME TO statements;
CREATE INDEX IF NOT EXISTS idx_statements_issue_id ON statements(issue_id);
CREATE INDEX IF NOT EXISTS idx_statements_kind_status ON statements(kind, status);
CREATE INDEX IF NOT EXISTS idx_statements_supersedes_id ON statements(supersedes_id);
CREATE INDEX IF NOT EXISTS idx_statements_topic ON statements(topic);
CREATE INDEX IF NOT EXISTS idx_statements_workspace ON statements(workspace);
CREATE INDEX IF NOT EXISTS idx_statements_concern ON statements(concern);
CREATE INDEX IF NOT EXISTS idx_statements_changed_at ON statements(changed_at);
ALTER TABLE comments ADD COLUMN session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE comments ADD COLUMN msg_id TEXT NOT NULL DEFAULT '';
ALTER TABLE comments ADD COLUMN tool_use_id TEXT NOT NULL DEFAULT '';
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
  AND s.kind = 'ruling';
