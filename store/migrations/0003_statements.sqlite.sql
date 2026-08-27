-- Migration 0003 — statements authority layer (SQLite).
CREATE TABLE IF NOT EXISTS statements (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('ruling','question','finding')),
    issue_id TEXT REFERENCES issues(id) ON DELETE CASCADE,
    text TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    filed_by TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','superseded','answered','retracted','candidate')),
    scope TEXT NOT NULL DEFAULT 'inherit' CHECK (scope IN ('inherit','self')),
    supersedes_id TEXT REFERENCES statements(id),
    answered_by TEXT REFERENCES statements(id),
    source_comment_id TEXT REFERENCES comments(id) ON DELETE SET NULL,
    evidence TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_statements_issue_id ON statements(issue_id);
CREATE INDEX IF NOT EXISTS idx_statements_kind_status ON statements(kind, status);
CREATE INDEX IF NOT EXISTS idx_statements_supersedes_id ON statements(supersedes_id);

CREATE TABLE IF NOT EXISTS statement_counters (
    kind TEXT PRIMARY KEY,
    last_id INTEGER NOT NULL DEFAULT 0
);
