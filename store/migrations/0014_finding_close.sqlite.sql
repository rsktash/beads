-- Migration 0014 — the actor who closed a statement (SQLite).
ALTER TABLE statements ADD COLUMN closed_by TEXT NOT NULL DEFAULT '';
