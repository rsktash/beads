-- Migration 0015 — the commit that answered a question (SQLite).
ALTER TABLE statements ADD COLUMN answered_commit TEXT NOT NULL DEFAULT '';
