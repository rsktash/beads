-- Migration 0013 — provenance transcript path (SQLite).
ALTER TABLE statements ADD COLUMN transcript_path TEXT NOT NULL DEFAULT '';
ALTER TABLE comments ADD COLUMN transcript_path TEXT NOT NULL DEFAULT '';
