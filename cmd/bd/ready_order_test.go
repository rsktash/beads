package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"text/tabwriter"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

var readyOrderEpoch = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

func newReadyOrderStore(t *testing.T) *store.Store {
	t.Helper()
	_, st := newTempContractStore(t, "ready")
	return st
}

func readyOrderIssue(t *testing.T, st *store.Store, id, title string, priority int, updatedAt time.Time) *beads.Issue {
	t.Helper()
	i := &beads.Issue{
		ID: id, Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: priority,
	}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create issue %s: %v", id, err)
	}
	if _, err := st.DB().ExecContext(context.Background(),
		"UPDATE issues SET created_at = ?, updated_at = ? WHERE id = ?", updatedAt, updatedAt, id); err != nil {
		t.Fatalf("set issue times for %s: %v", id, err)
	}
	i.CreatedAt = updatedAt
	i.UpdatedAt = updatedAt
	return i
}

func readyOrderPlan(t *testing.T, st *store.Store, id, status string, lanes ...store.PlanLane) {
	t.Helper()
	ctx := context.Background()
	if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: id, Title: id, Status: status}); err != nil {
		t.Fatalf("create plan %s: %v", id, err)
	}
	for i := range lanes {
		lanes[i].PlanID = id
		if err := st.AddLane(ctx, &lanes[i]); err != nil {
			t.Fatalf("add lane %s/%s: %v", id, lanes[i].Lane, err)
		}
	}
}

func readyOrderHandoff(t *testing.T, st *store.Store, planID, lane string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	sessionID := "session-" + planID
	if err := st.ClaimLane(ctx, planID, lane, sessionID); err != nil {
		t.Fatalf("claim lane %s/%s: %v", planID, lane, err)
	}
	if err := st.Handoff(ctx, &store.PlanHandoff{
		ID: "handoff-" + planID, PlanID: planID, Lane: lane, SessionID: sessionID, CreatedAt: at,
	}, 0); err != nil {
		t.Fatalf("handoff %s/%s: %v", planID, lane, err)
	}
}

func runReadyOrder(t *testing.T, jsonOutput bool, args ...string) (string, string, error) {
	t.Helper()
	oldJSON := flagJSON
	flagJSON = jsonOutput
	defer func() { flagJSON = oldJSON }()
	return captureStdoutStderr(func() error {
		cmd := newReadyCmd()
		cmd.SetArgs(args)
		return cmd.Execute()
	})
}

func readyOrderIDs(t *testing.T, out string) []string {
	t.Helper()
	var rows []slimIssue
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("decode ready JSON: %v\n%s", err, out)
	}
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	return ids
}

func requireReadyOrder(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ready ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ready ids = %v, want %v", got, want)
		}
	}
}

func TestReadyOrder_FollowsPlanQueue(t *testing.T) {
	st := newReadyOrderStore(t)
	a1 := readyOrderIssue(t, st, "ready-a1", "A first raw", 0, readyOrderEpoch.Add(4*time.Minute))
	a2 := readyOrderIssue(t, st, "ready-a2", "A queue head", 4, readyOrderEpoch.Add(time.Minute))
	b1 := readyOrderIssue(t, st, "ready-b1", "B first raw", 1, readyOrderEpoch.Add(3*time.Minute))
	b2 := readyOrderIssue(t, st, "ready-b2", "B queue head", 3, readyOrderEpoch.Add(2*time.Minute))
	readyOrderPlan(t, st, "active-two-lanes", "active",
		store.PlanLane{Lane: "B", Queue: []string{b2.ID, b1.ID}},
		store.PlanLane{Lane: "A", Queue: []string{a2.ID, a1.ID}})

	out, errOut, err := runReadyOrder(t, true)
	if err != nil {
		t.Fatalf("ready: %v (%s)", err, errOut)
	}
	requireReadyOrder(t, readyOrderIDs(t, out), a2.ID, a1.ID, b2.ID, b1.ID)
}

func TestReadyOrder_OffQueueBeadsFollow(t *testing.T) {
	st := newReadyOrderStore(t)
	queued := readyOrderIssue(t, st, "ready-queued", "queued", 4, readyOrderEpoch)
	older := readyOrderIssue(t, st, "ready-off-old", "off queue older", 2, readyOrderEpoch.Add(time.Minute))
	newer := readyOrderIssue(t, st, "ready-off-new", "off queue newer", 2, readyOrderEpoch.Add(2*time.Minute))
	highPriority := readyOrderIssue(t, st, "ready-off-priority", "off queue priority", 1, readyOrderEpoch)
	readyOrderPlan(t, st, "active-off-queue", "active",
		store.PlanLane{Lane: "A", Queue: []string{queued.ID}})

	out, errOut, err := runReadyOrder(t, true)
	if err != nil {
		t.Fatalf("ready: %v (%s)", err, errOut)
	}
	requireReadyOrder(t, readyOrderIDs(t, out), queued.ID, highPriority.ID, newer.ID, older.ID)
}

func TestReadyOrder_LaneColumnRendered(t *testing.T) {
	st := newReadyOrderStore(t)
	queued := readyOrderIssue(t, st, "ready-lane", "queued with lane", 2, readyOrderEpoch)
	offQueue := readyOrderIssue(t, st, "ready-no-lane", "not queued", 1, readyOrderEpoch.Add(time.Minute))
	readyOrderPlan(t, st, "active-lane-column", "active",
		store.PlanLane{Lane: "A", Queue: []string{queued.ID}})

	out, errOut, err := runReadyOrder(t, false)
	if err != nil {
		t.Fatalf("ready: %v (%s)", err, errOut)
	}
	if !strings.Contains(out, "LANE  ID") || !strings.Contains(out, "A1    "+queued.ID) {
		t.Fatalf("active-plan table lacks lane header or A1 row:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, offQueue.ID) && !strings.HasPrefix(line, "      ") {
			t.Fatalf("off-queue row has a lane value: %q", line)
		}
	}
}

func TestReadyOrder_TwoActivePlansUnion(t *testing.T) {
	st := newReadyOrderStore(t)
	alpha := readyOrderIssue(t, st, "ready-u-alpha", "alpha plan, late lane", 3, readyOrderEpoch.Add(time.Minute))
	zeta := readyOrderIssue(t, st, "ready-u-zeta", "zeta plan, early lane", 0, readyOrderEpoch)
	off := readyOrderIssue(t, st, "ready-u-off", "off queue bead", 1, readyOrderEpoch.Add(2*time.Minute))
	// Lane order alone would put zeta (lane A) before alpha (lane Z): the
	// expected order holds only when plan id is compared before lane.
	readyOrderPlan(t, st, "plan-alpha", "active", store.PlanLane{Lane: "Z", Queue: []string{alpha.ID}})
	readyOrderPlan(t, st, "plan-zeta", "active", store.PlanLane{Lane: "A", Queue: []string{zeta.ID}})

	out, errOut, err := runReadyOrder(t, true)
	if err != nil {
		t.Fatalf("ready: %v (%s)", err, errOut)
	}
	requireReadyOrder(t, readyOrderIDs(t, out), alpha.ID, zeta.ID, off.ID)

	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("decode ready JSON: %v\n%s", err, out)
	}
	if rows[0]["plan"] != "plan-alpha" || rows[0]["lane"] != "Z" || rows[0]["index"] != float64(1) {
		t.Fatalf("alpha slot fields = %v, want plan-alpha/Z/1", rows[0])
	}
	if rows[1]["plan"] != "plan-zeta" || rows[1]["lane"] != "A" || rows[1]["index"] != float64(1) {
		t.Fatalf("zeta slot fields = %v, want plan-zeta/A/1", rows[1])
	}
	if _, exists := rows[2]["plan"]; exists {
		t.Fatalf("off-queue row carries a plan field: %v", rows[2])
	}
}

// goldenReadyTable renders cell rows with the ready table's tabwriter
// settings, giving a byte-exact expectation for printReadyPlanTable.
func goldenReadyTable(rows [][]string) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 2, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r, "\t"))
	}
	w.Flush()
	return b.String()
}

func TestPrintReadyPlanTable_PlanColumnGated(t *testing.T) {
	issue := func(id, title string, priority int) beads.Issue {
		return beads.Issue{ID: id, Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: priority}
	}
	render := func(t *testing.T, issues []beads.Issue, queue map[string]store.QueueSlot) string {
		t.Helper()
		out, _, err := captureStdoutStderr(func() error {
			printReadyPlanTable(issues, queue)
			return nil
		})
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		return out
	}

	t.Run("single plan keeps today's columns", func(t *testing.T) {
		queued := issue("gate-queued", "queued", 1)
		off := issue("gate-off", "off queue", 2)
		got := render(t, []beads.Issue{queued, off}, map[string]store.QueueSlot{
			queued.ID: {Plan: "solo-plan", Lane: "A", Index: 1},
		})
		want := goldenReadyTable([][]string{
			{"LANE", "ID", "P", "STATUS", "TYPE", "ASSIGNEE", "TITLE"},
			{"A1", queued.ID, "p1", "open", "task", "-", "queued"},
			{"", off.ID, "p2", "open", "task", "-", "off queue"},
		})
		if got != want {
			t.Fatalf("single-plan output changed:\ngot:\n%swant:\n%s", got, want)
		}
	})

	t.Run("two plans lead with PLAN", func(t *testing.T) {
		alpha := issue("gate-alpha", "alpha bead", 1)
		off := issue("gate-none", "no plan bead", 2)
		beta := issue("gate-beta", "beta bead", 3)
		got := render(t, []beads.Issue{alpha, off, beta}, map[string]store.QueueSlot{
			alpha.ID: {Plan: "alpha-plan", Lane: "A", Index: 1},
			beta.ID:  {Plan: "beta-plan", Lane: "Z", Index: 2},
		})
		want := goldenReadyTable([][]string{
			{"PLAN", "LANE", "ID", "P", "STATUS", "TYPE", "ASSIGNEE", "TITLE"},
			{"alpha-plan", "A1", alpha.ID, "p1", "open", "task", "-", "alpha bead"},
			{"-", "", off.ID, "p2", "open", "task", "-", "no plan bead"},
			{"beta-plan", "Z2", beta.ID, "p3", "open", "task", "-", "beta bead"},
		})
		if got != want {
			t.Fatalf("two-plan output is wrong:\ngot:\n%swant:\n%s", got, want)
		}
	})
}

func TestReadyOrder_NoPlanPriorityFirst(t *testing.T) {
	st := newReadyOrderStore(t)
	low := readyOrderIssue(t, st, "ready-p4", "low priority", 4, readyOrderEpoch.Add(2*time.Minute))
	high := readyOrderIssue(t, st, "ready-p0", "high priority", 0, readyOrderEpoch)

	out, errOut, err := runReadyOrder(t, true)
	if err != nil {
		t.Fatalf("ready: %v (%s)", err, errOut)
	}
	requireReadyOrder(t, readyOrderIDs(t, out), high.ID, low.ID)
}

func TestReadyOrder_BlockerClearedSinceHandoffFirst(t *testing.T) {
	st := newReadyOrderStore(t)
	answered := readyOrderIssue(t, st, "ready-answered", "question answered", 1, readyOrderEpoch)
	dependency := readyOrderIssue(t, st, "ready-dependency", "dependency closed", 1, readyOrderEpoch.Add(time.Minute))
	ordinary := readyOrderIssue(t, st, "ready-ordinary", "ordinary newer bead", 1, readyOrderEpoch.Add(4*time.Minute))
	if _, err := st.DB().ExecContext(context.Background(),
		"UPDATE issues SET created_at = ? WHERE id = ?", readyOrderEpoch.Add(-time.Minute), ordinary.ID); err != nil {
		t.Fatalf("move ordinary bead to raw query head: %v", err)
	}

	question := &beads.Statement{
		Kind: "question", IssueID: &answered.ID, Text: "resolved?", Status: "answered",
		CreatedAt: readyOrderEpoch.Add(2 * time.Minute),
	}
	if err := st.CreateStatement(context.Background(), question); err != nil {
		t.Fatalf("create answered question: %v", err)
	}
	if _, err := st.DB().ExecContext(context.Background(),
		"UPDATE statements SET changed_at = ? WHERE id = ?", readyOrderEpoch.Add(2*time.Minute), question.ID); err != nil {
		t.Fatalf("set question changed_at: %v", err)
	}
	if _, err := st.DB().ExecContext(context.Background(),
		"UPDATE issues SET updated_at = ? WHERE id = ?", readyOrderEpoch, answered.ID); err != nil {
		t.Fatalf("restore answered issue updated_at: %v", err)
	}

	blocker := readyOrderIssue(t, st, "ready-closed-blocker", "closed blocker", 1, readyOrderEpoch)
	if err := st.AddDependency(context.Background(), beads.Dependency{
		IssueID: dependency.ID, DependsOnID: blocker.ID, Type: beads.DepBlocks,
	}); err != nil {
		t.Fatalf("add blocker dependency: %v", err)
	}
	if _, err := st.DB().ExecContext(context.Background(),
		"UPDATE issues SET status = 'closed', closed_at = ? WHERE id = ?", readyOrderEpoch.Add(3*time.Minute), blocker.ID); err != nil {
		t.Fatalf("close blocker: %v", err)
	}

	readyOrderPlan(t, st, "past-plan", "done", store.PlanLane{Lane: "A"})
	readyOrderHandoff(t, st, "past-plan", "A", readyOrderEpoch.Add(time.Minute))

	out, errOut, err := runReadyOrder(t, true)
	if err != nil {
		t.Fatalf("ready: %v (%s)", err, errOut)
	}
	ids := readyOrderIDs(t, out)
	if len(ids) != 3 || ids[2] != ordinary.ID ||
		!((ids[0] == answered.ID && ids[1] == dependency.ID) || (ids[0] == dependency.ID && ids[1] == answered.ID)) {
		t.Fatalf("recently cleared blockers must precede the ordinary bead: %v", ids)
	}
}

func TestReadyOrder_NoHandoffSkipsTieBreak(t *testing.T) {
	st := newReadyOrderStore(t)
	answered := readyOrderIssue(t, st, "ready-no-handoff-answer", "answered", 1, readyOrderEpoch)
	newer := readyOrderIssue(t, st, "ready-no-handoff-new", "newer", 1, readyOrderEpoch.Add(time.Minute))
	question := &beads.Statement{
		Kind: "question", IssueID: &answered.ID, Text: "resolved without handoff?", Status: "answered",
	}
	if err := st.CreateStatement(context.Background(), question); err != nil {
		t.Fatalf("create answered question: %v", err)
	}
	if _, err := st.DB().ExecContext(context.Background(),
		"UPDATE issues SET updated_at = ? WHERE id = ?", readyOrderEpoch, answered.ID); err != nil {
		t.Fatalf("restore answered issue updated_at: %v", err)
	}

	out, errOut, err := runReadyOrder(t, true)
	if err != nil {
		t.Fatalf("ready: %v (%s)", err, errOut)
	}
	requireReadyOrder(t, readyOrderIDs(t, out), newer.ID, answered.ID)
}

func TestReadyOrder_UnreadyBeadNeverSurfaces(t *testing.T) {
	st := newReadyOrderStore(t)
	blocked := readyOrderIssue(t, st, "ready-blocked", "blocked but queued", 1, readyOrderEpoch)
	ready := readyOrderIssue(t, st, "ready-allowed", "ready behind it", 1, readyOrderEpoch.Add(time.Minute))
	blocker := &beads.Issue{
		ID: "ready-live-blocker", Title: "live blocker", Type: beads.TypeTask,
		Status: beads.StatusInProgress, Priority: 1,
	}
	if err := st.CreateIssue(context.Background(), blocker); err != nil {
		t.Fatalf("create live blocker: %v", err)
	}
	if err := st.AddDependency(context.Background(), beads.Dependency{
		IssueID: blocked.ID, DependsOnID: blocker.ID, Type: beads.DepBlocks,
	}); err != nil {
		t.Fatalf("add blocking dependency: %v", err)
	}
	readyOrderPlan(t, st, "active-graph-wins", "active",
		store.PlanLane{Lane: "A", Queue: []string{blocked.ID, ready.ID}})

	out, errOut, err := runReadyOrder(t, true)
	if err != nil {
		t.Fatalf("ready: %v (%s)", err, errOut)
	}
	requireReadyOrder(t, readyOrderIDs(t, out), ready.ID)
}

func TestReadyOrder_LimitAppliesAfterOrdering(t *testing.T) {
	st := newReadyOrderStore(t)
	rawHead := readyOrderIssue(t, st, "ready-raw-head", "raw query head", 0, readyOrderEpoch)
	queueHead := readyOrderIssue(t, st, "ready-queue-head", "queue head", 4, readyOrderEpoch.Add(time.Minute))
	readyOrderPlan(t, st, "active-limit", "active",
		store.PlanLane{Lane: "A", Queue: []string{queueHead.ID, rawHead.ID}})

	out, errOut, err := runReadyOrder(t, true, "--limit", "1")
	if err != nil {
		t.Fatalf("ready --limit 1: %v (%s)", err, errOut)
	}
	requireReadyOrder(t, readyOrderIDs(t, out), queueHead.ID)
}
