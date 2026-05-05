-- Migration 0002 — additional indexes for the hot read paths.
--
-- (issue_id, type) on dependencies covers `listDependencies(issue_id)`
-- and the parent_id subquery in the web ENRICHED select. The PRIMARY KEY
-- is (issue_id, depends_on_id), which makes prefix lookups by issue_id
-- fast but still scans rows past the type filter.
CREATE INDEX IF NOT EXISTS idx_dependencies_issue_type ON dependencies(issue_id, type);

-- ready filters on (status='open', defer_until). Most issues are not in
-- defer; a partial index keeps it small and matches the bd ready path.
CREATE INDEX IF NOT EXISTS idx_issues_defer_until ON issues(defer_until)
    WHERE defer_until IS NOT NULL;

-- bd list / web list page sort by priority,created_at after a status
-- filter. Composite index avoids a sort step on larger tables.
CREATE INDEX IF NOT EXISTS idx_issues_status_priority_created
    ON issues(status, priority, created_at);
