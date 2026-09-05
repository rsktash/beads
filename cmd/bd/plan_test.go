package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newTempPlanStore(t *testing.T) *store.Store {
	t.Helper()
	isolateExpandState(t)
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "plan.db")
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetConfig(ctx, store.CfgIssuePrefix, "bd"); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	old := flagDB
	flagDB = dsn
	t.Cleanup(func() {
		flagDB = old
		_ = st.Close()
	})
	return st
}

// runPlan executes `bd plan …` with a fresh command tree, so flag state never
// leaks between cases.
func runPlan(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newPlanCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func mkPlanIssue(t *testing.T, st *store.Store, title string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	return i
}

// mkPlanQuestion files an active question against a bead and returns its id.
func mkPlanQuestion(t *testing.T, st *store.Store, issueID, text string) string {
	t.Helper()
	id := issueID
	q := &beads.Statement{Kind: "question", IssueID: &id, Text: text, Status: "active", Scope: "inherit", FiledBy: "executor:test"}
	if err := st.CreateStatement(context.Background(), q); err != nil {
		t.Fatalf("create question: %v", err)
	}
	return q.ID
}

// planWithLane builds one plan carrying one lane, and returns the plan id.
func planWithLane(t *testing.T, st *store.Store, lane string, cursor int, queue []string) string {
	t.Helper()
	ctx := context.Background()
	p := &store.ExecutionPlan{ID: "night-run-0904", Title: "Night run: exports + auth"}
	if err := st.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := st.AddLane(ctx, &store.PlanLane{
		PlanID: p.ID, Lane: lane, Queue: queue, Cursor: cursor, Mode: "subagent",
	}); err != nil {
		t.Fatalf("AddLane: %v", err)
	}
	return p.ID
}

// laneLine returns the LANE line for the named lane out of `plan show` output.
func laneLine(t *testing.T, out, lane string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "LANE  "+lane+"  ") {
			return line
		}
	}
	t.Fatalf("no LANE %s line in output:\n%s", lane, out)
	return ""
}

func TestPlan_ShowLaneShape(t *testing.T) {
	st := newTempPlanStore(t)
	ctx := context.Background()
	queue := make([]string, 0, 7)
	for i := 0; i < 7; i++ {
		queue = append(queue, mkPlanIssue(t, st, "queued task").ID)
	}
	planID := planWithLane(t, st, "A", 3, queue)
	if err := st.ClaimLane(ctx, planID, "A", "sess-2f4f"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	out, err := runPlan(t, "show", planID)
	if err != nil {
		t.Fatalf("plan show: %v (%s)", err, out)
	}
	wantPlan := "PLAN  night-run-0904  active  Night run: exports + auth"
	if !strings.Contains(out, wantPlan) {
		t.Fatalf("missing plan line %q in:\n%s", wantPlan, out)
	}
	want := "LANE  A  cursor 3/7  holder sess-2f4f  mode subagent  next " + queue[3] + "  ready"
	if got := laneLine(t, out, "A"); got != want {
		t.Fatalf("lane line\n got %q\nwant %q", got, want)
	}
}

func TestPlan_ShowRendersLastHandoffEntry(t *testing.T) {
	st := newTempPlanStore(t)
	ctx := context.Background()
	a, b := mkPlanIssue(t, st, "first"), mkPlanIssue(t, st, "second")
	parkTarget := mkPlanIssue(t, st, "parked one")
	qid := mkPlanQuestion(t, st, parkTarget.ID, "which shape?")
	planID := planWithLane(t, st, "A", 0, []string{a.ID, b.ID})
	if err := st.ClaimLane(ctx, planID, "A", "sess-91b0"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	threadPath := filepath.Join(t.TempDir(), "thread.md")
	if err := os.WriteFile(threadPath, []byte("exports paging landed; the cursor helper is pure\n"), 0o644); err != nil {
		t.Fatalf("write thread: %v", err)
	}
	if out, err := runPlan(t, "handoff", planID,
		"--lane", "A", "--session", "sess-91b0",
		"--done", a.ID+":a23dadd",
		"--next", b.ID,
		"--parked", parkTarget.ID+":"+qid,
		"--thread-file", threadPath); err != nil {
		t.Fatalf("handoff: %v (%s)", err, out)
	}

	out, err := runPlan(t, "show", planID)
	if err != nil {
		t.Fatalf("plan show: %v (%s)", err, out)
	}
	if !strings.Contains(out, "  done "+a.ID+":a23dadd") {
		t.Fatalf("handoff line missing done pair:\n%s", out)
	}
	if !strings.Contains(out, "  parked "+parkTarget.ID+":"+qid) {
		t.Fatalf("handoff line missing parked pair:\n%s", out)
	}
	if !strings.Contains(out, "      thread: exports paging landed; the cursor helper is pure\n") {
		t.Fatalf("thread line missing:\n%s", out)
	}
	if !strings.Contains(out, "handoff ") || !strings.Contains(out, " sess-91b0") {
		t.Fatalf("handoff entry missing session:\n%s", out)
	}
}

func TestPlan_JoinWithoutLaneWaits(t *testing.T) {
	st := newTempPlanStore(t)
	planID := planWithLane(t, st, "A", 0, []string{mkPlanIssue(t, st, "one").ID})

	out, err := runPlan(t, "join", planID, "--session", "sess-pool")
	if err != nil {
		t.Fatalf("join: %v (%s)", err, out)
	}
	if !strings.Contains(out, "waiting") {
		t.Fatalf("join without a lane should report waiting, got %q", out)
	}
	sessions, err := st.ListPlanSessions(context.Background(), planID)
	if err != nil {
		t.Fatalf("ListPlanSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "sess-pool" || sessions[0].Lane != "" {
		t.Fatalf("waiting pool row = %+v", sessions)
	}
}

func TestPlan_JoinHeldLaneStaysWaiting(t *testing.T) {
	st := newTempPlanStore(t)
	ctx := context.Background()
	planID := planWithLane(t, st, "B", 0, []string{mkPlanIssue(t, st, "one").ID})
	if err := st.ClaimLane(ctx, planID, "B", "sess-holder"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	out, err := runPlan(t, "join", planID, "--session", "sess-late", "--lane", "B")
	if err != nil {
		t.Fatalf("joining a held lane must not fail: %v (%s)", err, out)
	}
	if !strings.Contains(out, "waiting") || !strings.Contains(out, "sess-holder") {
		t.Fatalf("join of a held lane should report waiting and the holder, got %q", out)
	}
	lane, err := st.GetLane(ctx, planID, "B")
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if lane.Holder != "sess-holder" {
		t.Fatalf("holder = %q, want sess-holder", lane.Holder)
	}
	sessions, err := st.ListPlanSessions(ctx, planID)
	if err != nil {
		t.Fatalf("ListPlanSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "sess-late" {
		t.Fatalf("the waiting session should still be recorded, got %+v", sessions)
	}
}

// heldLanePlan builds a plan whose lane is claimed by sess-1, ready for a handoff.
func heldLanePlan(t *testing.T, st *store.Store) (string, []string) {
	t.Helper()
	a, b := mkPlanIssue(t, st, "first"), mkPlanIssue(t, st, "second")
	planID := planWithLane(t, st, "A", 0, []string{a.ID, b.ID})
	if err := st.ClaimLane(context.Background(), planID, "A", "sess-1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	return planID, []string{a.ID, b.ID}
}

func TestPlan_ParkRequiresQuestionId(t *testing.T) {
	st := newTempPlanStore(t)
	planID, ids := heldLanePlan(t, st)

	out, err := runPlan(t, "handoff", planID, "--lane", "A", "--session", "sess-1",
		"--next", ids[1], "--parked", ids[0])
	if err == nil {
		t.Fatalf("prose parking must be refused, got %q", out)
	}
	want := "park " + ids[0] + " needs a question id — file one with bd question add"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	lane, gerr := st.GetLane(context.Background(), planID, "A")
	if gerr != nil {
		t.Fatalf("GetLane: %v", gerr)
	}
	if lane.Holder != "sess-1" {
		t.Fatalf("refused handoff must not release the lane, holder = %q", lane.Holder)
	}
}

func TestPlan_ParkRejectsUnknownQuestion(t *testing.T) {
	st := newTempPlanStore(t)
	planID, ids := heldLanePlan(t, st)

	out, err := runPlan(t, "handoff", planID, "--lane", "A", "--session", "sess-1",
		"--next", ids[1], "--parked", ids[0]+":Q-999")
	if err == nil {
		t.Fatalf("an unknown question id must be refused, got %q", out)
	}
	want := "park " + ids[0] + " needs a question id — file one with bd question add"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	hs, lerr := st.ListHandoffs(context.Background(), planID)
	if lerr != nil {
		t.Fatalf("ListHandoffs: %v", lerr)
	}
	if len(hs) != 0 {
		t.Fatalf("refused handoff wrote %d entries", len(hs))
	}
}

func TestPlan_ThreadFileCappedAtFiveLines(t *testing.T) {
	st := newTempPlanStore(t)
	planID, ids := heldLanePlan(t, st)
	dir := t.TempDir()

	long := filepath.Join(dir, "long.md")
	if err := os.WriteFile(long, []byte("l1\nl2\nl3\nl4\nl5\nl6\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := runPlan(t, "handoff", planID, "--lane", "A", "--session", "sess-1",
		"--next", ids[1], "--thread-file", long)
	if err == nil {
		t.Fatalf("a six-line thread must be refused, got %q", out)
	}
	if !strings.Contains(err.Error(), "has 6 lines") {
		t.Fatalf("refusal should name the line count, got %q", err.Error())
	}

	ok := filepath.Join(dir, "ok.md")
	if err := os.WriteFile(ok, []byte("l1\nl2\nl3\nl4\nl5\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if out, err := runPlan(t, "handoff", planID, "--lane", "A", "--session", "sess-1",
		"--next", ids[1], "--thread-file", ok); err != nil {
		t.Fatalf("a five-line thread is at the cap and must pass: %v (%s)", err, out)
	}
}

func TestPlan_NextBlockedShowsBlocked(t *testing.T) {
	st := newTempPlanStore(t)
	blocked := mkPlanIssue(t, st, "needs a decision")
	after := mkPlanIssue(t, st, "later")
	qid := mkPlanQuestion(t, st, blocked.ID, "which shape?")
	planID := planWithLane(t, st, "B", 0, []string{blocked.ID, after.ID})

	out, err := runPlan(t, "show", planID)
	if err != nil {
		t.Fatalf("plan show: %v (%s)", err, out)
	}
	line := laneLine(t, out, "B")
	if !strings.HasSuffix(line, "next "+blocked.ID+"  blocked by "+qid) {
		t.Fatalf("lane line = %q, want it to end with next %s  blocked by %s", line, blocked.ID, qid)
	}
	// A blocked next item is shown, never skipped.
	if strings.Contains(line, after.ID) {
		t.Fatalf("blocked item was skipped over: %q", line)
	}
}

func TestPlan_HandedUnclaimed(t *testing.T) {
	st := newTempPlanStore(t)
	planID, ids := heldLanePlan(t, st)

	if out, err := runPlan(t, "handoff", planID, "--lane", "A", "--session", "sess-1",
		"--done", ids[0]+":a23dadd", "--next", ids[1]); err != nil {
		t.Fatalf("handoff: %v (%s)", err, out)
	}
	out, err := runPlan(t, "show", planID)
	if err != nil {
		t.Fatalf("plan show: %v (%s)", err, out)
	}
	line := laneLine(t, out, "A")
	if !strings.Contains(line, "cursor 1/2  handed, unclaimed  mode subagent  next "+ids[1]) {
		t.Fatalf("lane line = %q, want handed, unclaimed at cursor 1/2", line)
	}
}

func TestPlan_GraphWinsOverQueueOrder(t *testing.T) {
	st := newTempPlanStore(t)
	ctx := context.Background()
	first := mkPlanIssue(t, st, "must land first")
	second := mkPlanIssue(t, st, "queued next but blocked")
	if err := st.AddDependency(ctx, beads.Dependency{IssueID: second.ID, DependsOnID: first.ID, Type: beads.DepBlocks}); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}
	// The queue says go on the blocked item; the graph says wait.
	planID := planWithLane(t, st, "A", 0, []string{second.ID, first.ID})

	out, err := runPlan(t, "show", planID)
	if err != nil {
		t.Fatalf("plan show: %v (%s)", err, out)
	}
	line := laneLine(t, out, "A")
	want := "next " + second.ID + "  blocked (queue order says go; the graph says wait — the graph wins)"
	if !strings.HasSuffix(line, want) {
		t.Fatalf("lane line = %q, want suffix %q", line, want)
	}
}

func TestPlan_OpenToExecutor(t *testing.T) {
	t.Setenv("BD_ACTOR", "executor")
	st := newTempPlanStore(t)
	ctx := context.Background()
	a, b := mkPlanIssue(t, st, "first"), mkPlanIssue(t, st, "second")

	out, err := runPlan(t, "create", "Night run: exports + auth")
	if err != nil {
		t.Fatalf("an executor must be able to create a plan: %v (%s)", err, out)
	}
	planID := strings.TrimSpace(out)
	if !strings.HasPrefix(planID, "night-run-exports-auth-") {
		t.Fatalf("plan id = %q, want a slug of the title plus a suffix", planID)
	}
	if got := len(planID) - len("night-run-exports-auth-"); got != 4 {
		t.Fatalf("id suffix = %d chars, want 4 (%q)", got, planID)
	}

	if out, err := runPlan(t, "lane", "add", planID, "A", "--queue", a.ID+","+b.ID, "--mode", "codex"); err != nil {
		t.Fatalf("lane add: %v (%s)", err, out)
	}
	if out, err := runPlan(t, "claim", planID, "--lane", "A", "--session", "sess-exec"); err != nil {
		t.Fatalf("claim: %v (%s)", err, out)
	}
	if out, err := runPlan(t, "handoff", planID, "--lane", "A", "--session", "sess-exec",
		"--done", a.ID+":a23dadd", "--next", b.ID); err != nil {
		t.Fatalf("an executor must be able to hand off: %v (%s)", err, out)
	}
	lane, err := st.GetLane(ctx, planID, "A")
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if lane.Holder != "" || lane.Cursor != 1 {
		t.Fatalf("lane after handoff = holder %q cursor %d, want released at 1", lane.Holder, lane.Cursor)
	}
}

func TestPlan_ClaimRefusesHeldLane(t *testing.T) {
	st := newTempPlanStore(t)
	planID, _ := heldLanePlan(t, st)

	out, err := runPlan(t, "claim", planID, "--lane", "A", "--session", "sess-2")
	if err == nil {
		t.Fatalf("claiming a held lane must fail, got %q", out)
	}
	if !strings.HasPrefix(err.Error(), "lane A is held by session sess-1 since ") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestPlan_DoneEndsAFinishedPlan(t *testing.T) {
	st := newTempPlanStore(t)
	a := mkPlanIssue(t, st, "only task")
	planID := planWithLane(t, st, "A", 1, []string{a.ID})

	out, err := runPlan(t, "done", planID)
	if err != nil {
		t.Fatalf("plan done: %v (%s)", err, out)
	}
	if want := "plan " + planID + " done"; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
	show, err := runPlan(t, "show", planID)
	if err != nil {
		t.Fatalf("plan show: %v (%s)", err, show)
	}
	if !strings.Contains(show, "PLAN  "+planID+"  done  ") {
		t.Fatalf("plan show does not render the ended state:\n%s", show)
	}
	if strings.Contains(show, "end it with bd plan done") {
		t.Fatal("an ended plan still carries the finishable hint")
	}
}

func TestPlan_DoneRefusesAHeldLaneUntilForced(t *testing.T) {
	st := newTempPlanStore(t)
	planID, _ := heldLanePlan(t, st)

	out, err := runPlan(t, "done", planID)
	if err == nil {
		t.Fatalf("ending a held plan should be refused:\n%s", out)
	}
	msg := err.Error()
	for _, want := range []string{"still running", "held", "--force"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal %q does not name %q", msg, want)
		}
	}
	if p, gerr := st.GetPlan(context.Background(), planID); gerr != nil {
		t.Fatalf("GetPlan: %v", gerr)
	} else if p.Status != "active" {
		t.Fatalf("refused end wrote status %q", p.Status)
	}

	if out, err := runPlan(t, "done", planID, "--force"); err != nil {
		t.Fatalf("forced done: %v (%s)", err, out)
	}
	p, err := st.GetPlan(context.Background(), planID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if p.Status != "done" {
		t.Fatalf("status = %q, want done", p.Status)
	}
}

func TestPlan_AbandonRecordsTheOtherOutcome(t *testing.T) {
	st := newTempPlanStore(t)
	a, b := mkPlanIssue(t, st, "first"), mkPlanIssue(t, st, "second")
	planID := planWithLane(t, st, "A", 1, []string{a.ID, b.ID})

	out, err := runPlan(t, "abandon", planID, "--force")
	if err != nil {
		t.Fatalf("plan abandon: %v (%s)", err, out)
	}
	if want := "plan " + planID + " abandoned"; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
	p, err := st.GetPlan(context.Background(), planID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if p.Status != "abandoned" {
		t.Fatalf("status = %q, want abandoned", p.Status)
	}
}

func TestPlan_ShowHintsAFinishedPlanIsEndable(t *testing.T) {
	st := newTempPlanStore(t)
	a := mkPlanIssue(t, st, "only task")
	planID := planWithLane(t, st, "A", 1, []string{a.ID})

	out, err := runPlan(t, "show", planID)
	if err != nil {
		t.Fatalf("plan show: %v (%s)", err, out)
	}
	want := "every lane is done — end it with bd plan done " + planID
	if !strings.Contains(out, want) {
		t.Fatalf("missing hint %q in:\n%s", want, out)
	}
}

func TestPlan_ShowWithholdsTheHintFromARunningPlan(t *testing.T) {
	st := newTempPlanStore(t)
	a, b := mkPlanIssue(t, st, "first"), mkPlanIssue(t, st, "second")
	planID := planWithLane(t, st, "A", 1, []string{a.ID, b.ID})

	out, err := runPlan(t, "show", planID)
	if err != nil {
		t.Fatalf("plan show: %v (%s)", err, out)
	}
	if strings.Contains(out, "every lane is done") {
		t.Fatalf("a lane short of its end carries the hint:\n%s", out)
	}
}

func TestPlan_DoneUnknownPlanNamesIt(t *testing.T) {
	newTempPlanStore(t)
	_, err := runPlan(t, "done", "no-such-plan")
	if err == nil || !strings.Contains(err.Error(), "plan no-such-plan not found") {
		t.Fatalf("want a named not-found refusal, got %v", err)
	}
}

func TestPlan_EndOpenToExecutor(t *testing.T) {
	t.Setenv("BD_ACTOR", "executor")
	st := newTempPlanStore(t)
	a := mkPlanIssue(t, st, "only task")
	planID := planWithLane(t, st, "A", 1, []string{a.ID})

	// Every other plan verb is open to an executor; ending is the same class.
	if out, err := runPlan(t, "done", planID); err != nil {
		t.Fatalf("an executor must be able to end a plan: %v (%s)", err, out)
	}
}
