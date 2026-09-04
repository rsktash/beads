-- Migration 0011 — comment retract mark (SQLite).
ALTER TABLE comments ADD COLUMN retracted_at TIMESTAMP;
ALTER TABLE comments ADD COLUMN retracted_by TEXT NOT NULL DEFAULT '';
ALTER TABLE comments ADD COLUMN retract_note TEXT NOT NULL DEFAULT '';
