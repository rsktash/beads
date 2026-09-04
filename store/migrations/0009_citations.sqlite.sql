-- Migration 0009 — commit used to recover stale symbol-first citations.
ALTER TABLE statements ADD COLUMN head_sha TEXT NOT NULL DEFAULT '';
