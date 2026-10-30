package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsktash/beads/store"
)

// TestMigration0016_SQLiteKeepsHandoffs re-runs the plan_lane rebuild over a
// lane that already carries handoffs, on a connection that enforces foreign
// keys. Without foreign_keys OFF around the rebuild, dropping plan_lane would
// cascade-delete every plan_handoff row.
func TestMigration0016_SQLiteKeepsHandoffs(t *testing.T) {
	ctx := context.Background()
	dsn := "sqlite:file:" + filepath.Join(t.TempDir(), "m16.db") + "?_fk=1"
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	db := st.DB()

	if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: "p1", Title: "night run"}); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := st.AddLane(ctx, &store.PlanLane{PlanID: "p1", Lane: "A", Queue: []string{"bd-1"}, Mode: "codex"}); err != nil {
		t.Fatalf("AddLane codex: %v", err)
	}
	now := time.Now().UTC()
	for _, id := range []string{"h1", "h2"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO plan_handoff (id, plan_id, lane, session_id, created_at) VALUES (?, 'p1', 'A', 's1', ?)`, id, now); err != nil {
			t.Fatalf("insert handoff %s: %v", id, err)
		}
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = 16`); err != nil {
		t.Fatalf("unrecord 0016: %v", err)
	}
	if err := st.ForceMigrate(ctx); err != nil {
		t.Fatalf("re-run 0016: %v", err)
	}

	count := func() int {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plan_handoff`).Scan(&n); err != nil {
			t.Fatalf("count handoffs: %v", err)
		}
		return n
	}
	if n := count(); n != 2 {
		t.Fatalf("plan_handoff rows after 0016 = %d, want 2", n)
	}
	var mode string
	if err := db.QueryRowContext(ctx, `SELECT mode FROM plan_lane WHERE plan_id = 'p1' AND lane = 'A'`).Scan(&mode); err != nil {
		t.Fatalf("read lane: %v", err)
	}
	if mode != "codex" {
		t.Fatalf("existing lane mode = %q, want codex kept", mode)
	}

	if err := st.AddLane(ctx, &store.PlanLane{PlanID: "p1", Lane: "B", Queue: []string{"bd-2"}, Mode: "opencode"}); err != nil {
		t.Fatalf("AddLane opencode after 0016: %v", err)
	}
	if err := st.AddLane(ctx, &store.PlanLane{PlanID: "p1", Lane: "C", Queue: []string{"bd-3"}, Mode: "bogus"}); err == nil {
		t.Fatalf("mode bogus must fail the CHECK")
	}

	var fkOn int
	if err := db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fkOn); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if fkOn != 1 {
		t.Fatalf("foreign_keys after 0016 = %d, want 1", fkOn)
	}
	// plan_handoff's foreign key resolves to the rebuilt plan_lane.
	if _, err := db.ExecContext(ctx, `DELETE FROM plan_lane WHERE plan_id = 'p1' AND lane = 'A'`); err != nil {
		t.Fatalf("delete lane: %v", err)
	}
	if n := count(); n != 0 {
		t.Fatalf("plan_handoff rows after deleting their lane = %d, want 0 (cascade)", n)
	}
}
