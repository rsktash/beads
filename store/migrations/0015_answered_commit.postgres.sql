-- Migration 0015 — the commit that answered a question (Postgres).
ALTER TABLE statements ADD COLUMN IF NOT EXISTS answered_commit TEXT NOT NULL DEFAULT '';
