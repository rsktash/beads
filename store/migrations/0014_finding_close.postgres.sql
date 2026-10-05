-- Migration 0014 — the actor who closed a statement (Postgres).
ALTER TABLE statements ADD COLUMN IF NOT EXISTS closed_by TEXT NOT NULL DEFAULT '';
