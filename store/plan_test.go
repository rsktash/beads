package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rsktash/beads/store"
)

func newPlanStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "plan.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SetConfig(ctx, store.CfgIssuePrefix, "bd"); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	return st
}

// planFixture creates one active plan with a single lane holding a 3-item queue.
func planFixture(t *testing.T, st *store.Store, planID, lane string) {
	t.Helper()
	ctx := context.Background()
	if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: planID, Title: "night run"}); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := st.AddLane(ctx, &store.PlanLane{
		PlanID: planID,
		Lane:   lane,
		Queue:  []string{"bd-1", "bd-2", "bd-3"},
		Mode:   "subagent",
	}); err != nil {
		t.Fatalf("AddLane: %v", err)
	}
}

func TestClaim_FirstWins(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	planFixture(t, st, "night-run-0904", "A")

	if err := st.ClaimLane(ctx, "night-run-0904", "A", "sess-first"); err != nil {
		t.Fatalf("first claim should win: %v", err)
	}
	err := st.ClaimLane(ctx, "night-run-0904", "A", "sess-second")
	if err == nil {
		t.Fatal("second claim should be refused")
	}
	var held *store.LaneHeldError
	if !errors.As(err, &held) {
		t.Fatalf("want LaneHeldError, got %T: %v", err, err)
	}
	if held.Holder != "sess-first" {
		t.Fatalf("holder = %q, want sess-first", held.Holder)
	}
	if held.ClaimedAt.IsZero() {
		t.Fatal("held error should carry claimed_at")
	}
	if want := "lane A is held by session sess-first since "; err.Error()[:len(want)] != want {
		t.Fatalf("message = %q, want prefix %q", err.Error(), want)
	}

	lane, err := st.GetLane(ctx, "night-run-0904", "A")
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if lane.Holder != "sess-first" {
		t.Fatalf("holder after losing claim = %q, want sess-first", lane.Holder)
	}
}

func TestClaim_ConcurrentSingleWinner(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	planFixture(t, st, "race", "A")

	const goroutines = 20
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		wins  int
		other []error
	)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			err := st.ClaimLane(ctx, "race", "A", "sess-"+string(rune('a'+n)))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.As(err, new(*store.LaneHeldError)):
			default:
				other = append(other, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if len(other) > 0 {
		t.Fatalf("claims failed for reasons other than a held lane: %v", other)
	}
	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}
	lane, err := st.GetLane(ctx, "race", "A")
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if lane.Holder == "" {
		t.Fatal("lane should be held after the race")
	}
}

func TestHandoff_ReleasesLane(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	planFixture(t, st, "night-run-0904", "A")
	if err := st.ClaimLane(ctx, "night-run-0904", "A", "sess-91b0"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	h := &store.PlanHandoff{
		ID:        "H-1",
		PlanID:    "night-run-0904",
		Lane:      "A",
		SessionID: "sess-91b0",
		CreatedAt: time.Date(2026, 9, 4, 3, 12, 0, 0, time.UTC),
		Done:      []string{"bd-1:a23dadd"},
		NextID:    "bd-2",
		Parked:    []string{"bd-9:Q-12"},
		Thread:    "exports paging landed",
	}
	if err := st.Handoff(ctx, h, 1); err != nil {
		t.Fatalf("Handoff: %v", err)
	}

	lane, err := st.GetLane(ctx, "night-run-0904", "A")
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if lane.Holder != "" || lane.ClaimedAt != nil {
		t.Fatalf("lane not released: holder=%q claimed_at=%v", lane.Holder, lane.ClaimedAt)
	}
	if lane.Cursor != 1 {
		t.Fatalf("cursor = %d, want 1", lane.Cursor)
	}

	last, err := st.LastHandoffPerLane(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("LastHandoffPerLane: %v", err)
	}
	got, ok := last["A"]
	if !ok {
		t.Fatal("no handoff recorded for lane A")
	}
	if len(got.Done) != 1 || got.Done[0] != "bd-1:a23dadd" {
		t.Fatalf("done = %v, want [bd-1:a23dadd]", got.Done)
	}
	if len(got.Parked) != 1 || got.Parked[0] != "bd-9:Q-12" {
		t.Fatalf("parked = %v, want [bd-9:Q-12]", got.Parked)
	}
	if got.Thread != "exports paging landed" {
		t.Fatalf("thread = %q", got.Thread)
	}

	// Released means the next session can take it.
	if err := st.ClaimLane(ctx, "night-run-0904", "A", "sess-next"); err != nil {
		t.Fatalf("claim after handoff: %v", err)
	}
}

func TestHandoff_RefusesNonHolder(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	planFixture(t, st, "night-run-0904", "A")
	if err := st.ClaimLane(ctx, "night-run-0904", "A", "sess-holder"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	err := st.Handoff(ctx, &store.PlanHandoff{
		ID:        "H-9",
		PlanID:    "night-run-0904",
		Lane:      "A",
		SessionID: "sess-stranger",
		Done:      []string{"bd-1:deadbee"},
	}, 1)
	if !errors.Is(err, store.ErrNotHolder) {
		t.Fatalf("want ErrNotHolder, got %v", err)
	}

	// Nothing partial survives: the lane is still held and no entry was written.
	lane, err := st.GetLane(ctx, "night-run-0904", "A")
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if lane.Holder != "sess-holder" {
		t.Fatalf("holder = %q, want sess-holder", lane.Holder)
	}
	if lane.Cursor != 0 {
		t.Fatalf("cursor = %d, want 0", lane.Cursor)
	}
	hs, err := st.ListHandoffs(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("ListHandoffs: %v", err)
	}
	if len(hs) != 0 {
		t.Fatalf("handoffs = %d, want 0", len(hs))
	}
}

func TestHandoff_CascadesOnPlanDelete(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	planFixture(t, st, "night-run-0904", "A")
	if err := st.JoinPlan(ctx, "night-run-0904", "sess-91b0", "A"); err != nil {
		t.Fatalf("JoinPlan: %v", err)
	}
	if err := st.ClaimLane(ctx, "night-run-0904", "A", "sess-91b0"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := st.Handoff(ctx, &store.PlanHandoff{
		ID:        "H-1",
		PlanID:    "night-run-0904",
		Lane:      "A",
		SessionID: "sess-91b0",
		Done:      []string{"bd-1:a23dadd"},
	}, 1); err != nil {
		t.Fatalf("Handoff: %v", err)
	}

	if err := st.DeletePlan(ctx, "night-run-0904"); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}

	if _, err := st.GetPlan(ctx, "night-run-0904"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("plan still readable: %v", err)
	}
	lanes, err := st.ListLanes(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("ListLanes: %v", err)
	}
	if len(lanes) != 0 {
		t.Fatalf("lanes = %d, want 0 after cascade", len(lanes))
	}
	sessions, err := st.ListPlanSessions(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("ListPlanSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions = %d, want 0 after cascade", len(sessions))
	}
	hs, err := st.ListHandoffs(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("ListHandoffs: %v", err)
	}
	if len(hs) != 0 {
		t.Fatalf("handoffs = %d, want 0 after cascade", len(hs))
	}
}
