package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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

func TestActivePlanQueues_UnionsEveryActivePlan(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	for _, plan := range []struct{ id, lane string }{
		{"alpha-0905", "A"},
		{"beta-0905", "Z"},
	} {
		if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: plan.id, Title: plan.id}); err != nil {
			t.Fatalf("CreatePlan %s: %v", plan.id, err)
		}
		if err := st.AddLane(ctx, &store.PlanLane{
			PlanID: plan.id,
			Lane:   plan.lane,
			Queue:  []string{plan.id + "-1", plan.id + "-2"},
		}); err != nil {
			t.Fatalf("AddLane %s/%s: %v", plan.id, plan.lane, err)
		}
	}
	if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: "done-0904", Title: "done-0904", Status: "done"}); err != nil {
		t.Fatalf("CreatePlan done-0904: %v", err)
	}
	if err := st.AddLane(ctx, &store.PlanLane{PlanID: "done-0904", Lane: "A", Queue: []string{"done-0904-1"}}); err != nil {
		t.Fatalf("AddLane done-0904/A: %v", err)
	}

	ids, slots, err := st.ActivePlanQueues(ctx)
	if err != nil {
		t.Fatalf("ActivePlanQueues: %v", err)
	}
	if len(ids) != 2 || ids[0] != "alpha-0905" || ids[1] != "beta-0905" {
		t.Fatalf("plan ids = %v, want [alpha-0905 beta-0905]", ids)
	}
	for _, want := range []struct {
		bead string
		slot store.QueueSlot
	}{
		{"alpha-0905-1", store.QueueSlot{Plan: "alpha-0905", Lane: "A", Index: 1}},
		{"alpha-0905-2", store.QueueSlot{Plan: "alpha-0905", Lane: "A", Index: 2}},
		{"beta-0905-1", store.QueueSlot{Plan: "beta-0905", Lane: "Z", Index: 1}},
		{"beta-0905-2", store.QueueSlot{Plan: "beta-0905", Lane: "Z", Index: 2}},
	} {
		got, ok := slots[want.bead]
		if !ok {
			t.Fatalf("bead %s missing from the union map", want.bead)
		}
		if got != want.slot {
			t.Fatalf("slot for %s = %+v, want %+v", want.bead, got, want.slot)
		}
	}
	if _, ok := slots["done-0904-1"]; ok {
		t.Fatal("inactive plan bead leaked into the union map")
	}
}

func TestActivePlanQueues_DuplicateBeadKeepsFirstPlanSlot(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	for _, plan := range []struct{ id, lane string }{
		{"zeta-plan", "A"},
		{"alpha-plan", "Z"},
	} {
		if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: plan.id, Title: plan.id}); err != nil {
			t.Fatalf("CreatePlan %s: %v", plan.id, err)
		}
		if err := st.AddLane(ctx, &store.PlanLane{
			PlanID: plan.id,
			Lane:   plan.lane,
			Queue:  []string{"bd-shared"},
		}); err != nil {
			t.Fatalf("AddLane %s/%s: %v", plan.id, plan.lane, err)
		}
	}

	ids, slots, err := st.ActivePlanQueues(ctx)
	if err != nil {
		t.Fatalf("ActivePlanQueues: %v", err)
	}
	if len(ids) != 2 || ids[0] != "alpha-plan" || ids[1] != "zeta-plan" {
		t.Fatalf("plan ids = %v, want [alpha-plan zeta-plan]", ids)
	}
	want := store.QueueSlot{Plan: "alpha-plan", Lane: "Z", Index: 1}
	if got := slots["bd-shared"]; got != want {
		t.Fatalf("slot for bd-shared = %+v, want %+v (first plan in id order)", got, want)
	}
	if len(slots) != 1 {
		t.Fatalf("union map holds %d entries, want 1: %v", len(slots), slots)
	}
}

func TestLastHandoffForBead_FindsBeadInSecondPlan(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	for _, id := range []string{"alpha-0905", "beta-0905"} {
		if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: id, Title: id}); err != nil {
			t.Fatalf("CreatePlan %s: %v", id, err)
		}
	}
	if err := st.AddLane(ctx, &store.PlanLane{PlanID: "alpha-0905", Lane: "A", Queue: []string{"bd-alpha-1"}}); err != nil {
		t.Fatalf("AddLane alpha-0905/A: %v", err)
	}
	if err := st.AddLane(ctx, &store.PlanLane{PlanID: "beta-0905", Lane: "B", Queue: []string{"bd-beta-1"}}); err != nil {
		t.Fatalf("AddLane beta-0905/B: %v", err)
	}
	handoffAt := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
	if err := st.ClaimLane(ctx, "beta-0905", "B", "sess-beta"); err != nil {
		t.Fatalf("ClaimLane beta-0905/B: %v", err)
	}
	if err := st.Handoff(ctx, &store.PlanHandoff{
		ID: "H-beta", PlanID: "beta-0905", Lane: "B",
		SessionID: "sess-beta", CreatedAt: handoffAt,
	}, 0); err != nil {
		t.Fatalf("Handoff beta-0905/B: %v", err)
	}

	at, lane, ok, err := st.LastHandoffForBead(ctx, "bd-beta-1")
	if err != nil {
		t.Fatalf("LastHandoffForBead: %v", err)
	}
	if !ok || lane != "B" || !at.Equal(handoffAt) {
		t.Fatalf("at=%v lane=%q ok=%v, want %v B true", at, lane, ok, handoffAt)
	}

	at, lane, ok, err = st.LastHandoffForBead(ctx, "bd-alpha-1")
	if err != nil {
		t.Fatalf("LastHandoffForBead alpha: %v", err)
	}
	if ok || lane != "A" || !at.IsZero() {
		t.Fatalf("alpha bead: at=%v lane=%q ok=%v, want zero, A, false (lane named, no handoff)", at, lane, ok)
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

// finishLane runs a lane's cursor to the end of its queue.
func finishLane(t *testing.T, st *store.Store, planID, lane string) {
	t.Helper()
	ctx := context.Background()
	l, err := st.GetLane(ctx, planID, lane)
	if err != nil {
		t.Fatalf("GetLane %s/%s: %v", planID, lane, err)
	}
	if err := st.ClaimLane(ctx, planID, lane, "sess-finish"); err != nil {
		t.Fatalf("ClaimLane %s/%s: %v", planID, lane, err)
	}
	h := &store.PlanHandoff{
		ID:        "ph-" + planID + "-" + lane,
		PlanID:    planID,
		Lane:      lane,
		SessionID: "sess-finish",
	}
	if err := st.Handoff(ctx, h, len(l.Queue)); err != nil {
		t.Fatalf("Handoff %s/%s: %v", planID, lane, err)
	}
}

func TestEndPlan_FinishedPlanLeavesTheReadyUnion(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	planFixture(t, st, "night-run-0904", "A")
	finishLane(t, st, "night-run-0904", "A")

	if err := st.EndPlan(ctx, "night-run-0904", "done", false); err != nil {
		t.Fatalf("EndPlan: %v", err)
	}
	p, err := st.GetPlan(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if p.Status != "done" {
		t.Fatalf("status = %q, want done", p.Status)
	}
	ids, slots, err := st.ActivePlanQueues(ctx)
	if err != nil {
		t.Fatalf("ActivePlanQueues: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("plan ids = %v, want none", ids)
	}
	if _, ok := slots["bd-1"]; ok {
		t.Fatal("ended plan still orders ready")
	}
}

func TestEndPlan_RefusesHeldAndUnfinishedLanesTogether(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	planFixture(t, st, "night-run-0904", "A")
	if err := st.AddLane(ctx, &store.PlanLane{
		PlanID: "night-run-0904",
		Lane:   "B",
		Queue:  []string{"bd-4"},
	}); err != nil {
		t.Fatalf("AddLane B: %v", err)
	}
	if err := st.ClaimLane(ctx, "night-run-0904", "A", "sess-holder"); err != nil {
		t.Fatalf("ClaimLane: %v", err)
	}

	err := st.EndPlan(ctx, "night-run-0904", "done", false)
	var unfinished *store.PlanUnfinishedError
	if !errors.As(err, &unfinished) {
		t.Fatalf("want PlanUnfinishedError, got %T: %v", err, err)
	}
	// Both reasons in one refusal: A is held, A and B are both short of the end.
	if len(unfinished.Held) != 1 || !strings.Contains(unfinished.Held[0], "sess-holder") {
		t.Fatalf("held = %v, want A held by sess-holder", unfinished.Held)
	}
	if len(unfinished.Open) != 2 {
		t.Fatalf("open = %v, want both lanes", unfinished.Open)
	}
	p, err := st.GetPlan(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if p.Status != "active" {
		t.Fatalf("refused end still wrote status %q", p.Status)
	}
}

func TestEndPlan_ForceEndsAHeldPlan(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	planFixture(t, st, "night-run-0904", "A")
	if err := st.ClaimLane(ctx, "night-run-0904", "A", "sess-holder"); err != nil {
		t.Fatalf("ClaimLane: %v", err)
	}

	if err := st.EndPlan(ctx, "night-run-0904", "abandoned", true); err != nil {
		t.Fatalf("forced EndPlan: %v", err)
	}
	p, err := st.GetPlan(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if p.Status != "abandoned" {
		t.Fatalf("status = %q, want abandoned", p.Status)
	}
}

func TestEndPlan_RefusesRewritingASettledOutcome(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	planFixture(t, st, "night-run-0904", "A")
	finishLane(t, st, "night-run-0904", "A")
	if err := st.EndPlan(ctx, "night-run-0904", "done", false); err != nil {
		t.Fatalf("first end: %v", err)
	}

	err := st.EndPlan(ctx, "night-run-0904", "abandoned", true)
	if err == nil {
		t.Fatal("re-ending a settled plan should be refused, force included")
	}
	p, err := st.GetPlan(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if p.Status != "done" {
		t.Fatalf("status = %q, want the first outcome to stand", p.Status)
	}
}

func TestEndPlan_RefusesAStatusOutsideTheVocabulary(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	planFixture(t, st, "night-run-0904", "A")

	for _, status := range []string{"closed", "active", ""} {
		if err := st.EndPlan(ctx, "night-run-0904", status, true); err == nil {
			t.Fatalf("status %q should be refused", status)
		}
	}
}

func TestEndPlan_UnknownPlanIsNotFound(t *testing.T) {
	st := newPlanStore(t)
	err := st.EndPlan(context.Background(), "no-such-plan", "done", true)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %T: %v", err, err)
	}
}

func TestFinishable_LaneLessPlanIsNotFinishable(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	// The case that rules derivation out: an empty queue counts as a done
	// lane, so a plan still being built must never read as finished.
	if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: "fresh-0905", Title: "fresh"}); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	p, err := st.GetPlan(ctx, "fresh-0905")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if store.Finishable(*p, nil) {
		t.Fatal("a plan with no lanes yet must not read as finishable")
	}
	if err := st.AddLane(ctx, &store.PlanLane{PlanID: "fresh-0905", Lane: "A", Queue: []string{"bd-1"}}); err != nil {
		t.Fatalf("AddLane: %v", err)
	}
	lanes, err := st.ListLanes(ctx, "fresh-0905")
	if err != nil {
		t.Fatalf("ListLanes: %v", err)
	}
	if store.Finishable(*p, lanes) {
		t.Fatal("a lane short of its end must not read as finishable")
	}
}

func TestFinishable_EveryLaneDoneOnAnActivePlan(t *testing.T) {
	ctx := context.Background()
	st := newPlanStore(t)
	planFixture(t, st, "night-run-0904", "A")
	finishLane(t, st, "night-run-0904", "A")

	p, err := st.GetPlan(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	lanes, err := st.ListLanes(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("ListLanes: %v", err)
	}
	if !store.Finishable(*p, lanes) {
		t.Fatal("an active plan with every lane done should read as finishable")
	}
	if err := st.EndPlan(ctx, "night-run-0904", "done", false); err != nil {
		t.Fatalf("EndPlan: %v", err)
	}
	ended, err := st.GetPlan(ctx, "night-run-0904")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if store.Finishable(*ended, lanes) {
		t.Fatal("an ended plan is no longer finishable")
	}
}
