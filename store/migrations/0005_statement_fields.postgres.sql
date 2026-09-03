-- Migration 0005 — statement authority fields + retired status (Postgres).
ALTER TABLE statements DROP CONSTRAINT IF EXISTS statements_status_check;
ALTER TABLE statements ADD CONSTRAINT statements_status_check
    CHECK (status IN ('active','superseded','answered','retracted','candidate','retired'));
ALTER TABLE statements ADD COLUMN IF NOT EXISTS topic TEXT NOT NULL DEFAULT '';
ALTER TABLE statements ADD COLUMN IF NOT EXISTS workspace TEXT NOT NULL DEFAULT '';
ALTER TABLE statements ADD COLUMN IF NOT EXISTS concern TEXT NOT NULL DEFAULT '';
ALTER TABLE statements ADD COLUMN IF NOT EXISTS law TEXT NOT NULL DEFAULT '';
ALTER TABLE statements ADD COLUMN IF NOT EXISTS rationale TEXT NOT NULL DEFAULT '';
ALTER TABLE statements ADD COLUMN IF NOT EXISTS verbatim TEXT NOT NULL DEFAULT '';
ALTER TABLE statements ADD COLUMN IF NOT EXISTS author TEXT NOT NULL DEFAULT '';
ALTER TABLE statements ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE statements ADD COLUMN IF NOT EXISTS msg_id TEXT NOT NULL DEFAULT '';
ALTER TABLE statements ADD COLUMN IF NOT EXISTS tool_use_id TEXT NOT NULL DEFAULT '';
ALTER TABLE statements ADD COLUMN IF NOT EXISTS retire_note TEXT NOT NULL DEFAULT '';
ALTER TABLE statements ADD COLUMN IF NOT EXISTS changed_at TIMESTAMPTZ;
ALTER TABLE comments ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE comments ADD COLUMN IF NOT EXISTS msg_id TEXT NOT NULL DEFAULT '';
ALTER TABLE comments ADD COLUMN IF NOT EXISTS tool_use_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_statements_topic ON statements(topic);
CREATE INDEX IF NOT EXISTS idx_statements_workspace ON statements(workspace);
CREATE INDEX IF NOT EXISTS idx_statements_concern ON statements(concern);
CREATE INDEX IF NOT EXISTS idx_statements_changed_at ON statements(changed_at);
