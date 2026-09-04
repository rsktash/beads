-- Migration 0011 — comment retract mark (Postgres).
ALTER TABLE comments ADD COLUMN IF NOT EXISTS retracted_at TIMESTAMPTZ;
ALTER TABLE comments ADD COLUMN IF NOT EXISTS retracted_by TEXT NOT NULL DEFAULT '';
ALTER TABLE comments ADD COLUMN IF NOT EXISTS retract_note TEXT NOT NULL DEFAULT '';
