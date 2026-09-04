-- Migration 0013 — provenance transcript path (Postgres).
ALTER TABLE statements ADD COLUMN IF NOT EXISTS transcript_path TEXT NOT NULL DEFAULT '';
ALTER TABLE comments ADD COLUMN IF NOT EXISTS transcript_path TEXT NOT NULL DEFAULT '';
