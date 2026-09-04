-- Migration 0010 — finding back-links to the source bead (Postgres).
ALTER TABLE statements ADD COLUMN IF NOT EXISTS source_issue_id TEXT REFERENCES issues(id);
CREATE INDEX IF NOT EXISTS idx_statements_source_issue_id ON statements(source_issue_id);
