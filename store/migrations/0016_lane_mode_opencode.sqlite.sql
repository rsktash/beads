-- Migration 0016 — lane mode 'opencode'; 'codex' stays allowed for existing lanes (SQLite).
-- foreign_keys must be OFF before BEGIN (the pragma is a no-op inside a transaction):
-- with it ON, DROP TABLE plan_lane would cascade-delete every plan_handoff row.
PRAGMA foreign_keys=OFF;
BEGIN;
CREATE TABLE plan_lane_new (
    plan_id TEXT NOT NULL REFERENCES execution_plan(id) ON DELETE CASCADE,
    lane TEXT NOT NULL,
    queue TEXT NOT NULL DEFAULT '',
    cursor INTEGER NOT NULL DEFAULT 0,
    mode TEXT NOT NULL DEFAULT 'subagent' CHECK (mode IN ('inline','subagent','codex','opencode')),
    rulings TEXT NOT NULL DEFAULT '',
    holder TEXT,
    claimed_at TIMESTAMP,
    PRIMARY KEY (plan_id, lane)
);
INSERT INTO plan_lane_new (plan_id, lane, queue, cursor, mode, rulings, holder, claimed_at)
    SELECT plan_id, lane, queue, cursor, mode, rulings, holder, claimed_at FROM plan_lane;
DROP TABLE plan_lane;
ALTER TABLE plan_lane_new RENAME TO plan_lane;
COMMIT;
PRAGMA foreign_key_check;
PRAGMA foreign_keys=ON;
