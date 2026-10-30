-- Migration 0016 — lane mode 'opencode'; 'codex' stays allowed for existing lanes (Postgres).
ALTER TABLE plan_lane DROP CONSTRAINT IF EXISTS plan_lane_mode_check;
ALTER TABLE plan_lane ADD CONSTRAINT plan_lane_mode_check
    CHECK (mode IN ('inline','subagent','codex','opencode'));
