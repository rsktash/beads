package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

var sinceTestWindow = time.Date(2026, 9, 2, 12, 34, 0, 0, time.UTC)

func setStatementChangedAt(t *testing.T, st *store.Store, id string, at *time.Time) {
	t.Helper()
	var value any
	if at != nil {
		value = *at
	}
	if _, err := st.DB().ExecContext(context.Background(),
		"UPDATE statements SET changed_at = ? WHERE id = ?", value, id); err != nil {
		t.Fatalf("set changed_at for %s: %v", id, err)
	}
}

func addSinceComment(t *testing.T, st *store.Store, issueID, text string, at time.Time) {
	t.Helper()
	if err := st.AddComment(context.Background(), &beads.Comment{
		IssueID: issueID, Author: "owner", Text: text, CreatedAt: at,
	}); err != nil {
		t.Fatalf("add comment: %v", err)
	}
}

func addSinceHandoff(t *testing.T, st *store.Store, planID, status, lane string, queue []string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: planID, Title: planID, Status: status}); err != nil {
		t.Fatalf("create plan %s: %v", planID, err)
	}
	if err := st.AddLane(ctx, &store.PlanLane{PlanID: planID, Lane: lane, Queue: queue}); err != nil {
		t.Fatalf("add lane %s/%s: %v", planID, lane, err)
	}
	sessionID := "session-" + planID
	if err := st.ClaimLane(ctx, planID, lane, sessionID); err != nil {
		t.Fatalf("claim lane %s/%s: %v", planID, lane, err)
	}
	if err := st.Handoff(ctx, &store.PlanHandoff{
		ID: "handoff-" + planID, PlanID: planID, Lane: lane,
		SessionID: sessionID, CreatedAt: at,
	}, 0); err != nil {
		t.Fatalf("handoff %s/%s: %v", planID, lane, err)
	}
}

func TestSince_DateNarrowsSections(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "date window", "## Files\n- server/src/auth.ts\n")
	dateWindow := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	oldAt, newAt := dateWindow.Add(-time.Hour), dateWindow.Add(time.Hour)

	for _, fixture := range []struct {
		kind string
		text string
		at   time.Time
	}{
		{kind: "ruling", text: "old ruling", at: oldAt},
		{kind: "ruling", text: "new ruling", at: newAt},
		{kind: "question", text: "old question", at: oldAt},
		{kind: "question", text: "new question", at: newAt},
		{kind: "finding", text: "old finding", at: oldAt},
		{kind: "finding", text: "new finding", at: newAt},
	} {
		row := mkAuthStatement(t, st, authStatement{
			kind: fixture.kind, issueID: bead.ID, topic: "date-window",
			text: fixture.text, createdAt: fixture.at,
		})
		setStatementChangedAt(t, st, row.ID, &fixture.at)
	}
	addSinceComment(t, st, bead.ID, "old comment", oldAt)
	addSinceComment(t, st, bead.ID, "new comment", newAt)

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--since", "2026-09-02")
	requireNoErr(t, err, errOut)
	for _, section := range []struct {
		label string
		want  string
		gone  string
	}{
		{label: "RULINGS", want: "new ruling", gone: "old ruling"},
		{label: "QUESTIONS", want: "new question", gone: "old question"},
		{label: "FINDINGS", want: "new finding", gone: "old finding"},
		{label: "COMMENTS", want: "new comment", gone: "old comment"},
	} {
		rows := strings.Join(briefSection(out, section.label), "\n")
		if !strings.Contains(rows, section.want) {
			t.Errorf("%q is missing from %s:\n%s", section.want, section.label, rows)
		}
		if strings.Contains(rows, section.gone) {
			t.Errorf("%q survived the date window in %s:\n%s", section.gone, section.label, rows)
		}
	}
}

func TestSince_RFC3339Accepted(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "RFC3339 window", "## Files\n- server/src/auth.ts\n")
	row := mkAuthStatement(t, st, authStatement{
		kind: "finding", issueID: bead.ID, topic: "rfc3339", text: "inside RFC3339 window",
		createdAt: sinceTestWindow.Add(time.Minute),
	})
	changedAt := sinceTestWindow.Add(time.Minute)
	setStatementChangedAt(t, st, row.ID, &changedAt)

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "findings",
		"--since", sinceTestWindow.Format(time.RFC3339))
	requireNoErr(t, err, errOut)
	if !strings.Contains(out, "FINDINGS") || !strings.Contains(out, "(since 2026-09-02 12:34)") {
		t.Fatalf("RFC3339 window is missing from the section header:\n%s", out)
	}
	if !strings.Contains(out, "inside RFC3339 window") {
		t.Fatalf("recent finding is missing:\n%s", out)
	}
}

func TestSince_GarbageRefused(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "bad window", "## Files\n- server/src/auth.ts\n")

	_, _, err := runAuthority(t, "authority", bead.ID, "--since", "2026-09-02 12:34")
	if err == nil {
		t.Fatal("an unsupported --since form must be refused")
	}
	const want = "--since takes YYYY-MM-DD, an RFC3339 timestamp, or the word handoff"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestSince_HandoffResolvesLaneTimestamp(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "handoff window", "## Files\n- server/src/auth.ts\n")
	handoffAt := sinceTestWindow
	oldAt, newAt := handoffAt.Add(-time.Hour), handoffAt.Add(time.Hour)
	oldRow := mkAuthStatement(t, st, authStatement{
		kind: "ruling", issueID: bead.ID, topic: "handoff-window", text: "before active handoff", createdAt: oldAt,
	})
	newRow := mkAuthStatement(t, st, authStatement{
		kind: "ruling", issueID: bead.ID, topic: "handoff-window", text: "after active handoff", createdAt: newAt,
	})
	setStatementChangedAt(t, st, oldRow.ID, &oldAt)
	setStatementChangedAt(t, st, newRow.ID, &newAt)
	addSinceHandoff(t, st, "inactive-plan", "abandoned", "Z", []string{bead.ID}, newAt.Add(time.Hour))
	addSinceHandoff(t, st, "active-plan", "active", "A", []string{bead.ID}, handoffAt)

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings", "--since", "handoff")
	requireNoErr(t, err, errOut)
	if !strings.Contains(out, "RULINGS") || !strings.Contains(out, "(since 2026-09-02 12:34 — lane A handoff)") {
		t.Fatalf("active lane handoff is missing from the section header:\n%s", out)
	}
	if strings.Contains(out, "before active handoff") || !strings.Contains(out, "after active handoff") {
		t.Fatalf("handoff window used the wrong plan or timestamp:\n%s", out)
	}
}

func TestSince_TwoActivePlansAmbiguous(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "ambiguous plan", "## Files\n- server/src/auth.ts\n")
	for _, id := range []string{"plan-alpha", "plan-beta"} {
		if err := st.CreatePlan(context.Background(), &store.ExecutionPlan{ID: id, Title: id}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	_, _, err := runAuthority(t, "authority", bead.ID, "--since", "handoff")
	if err == nil {
		t.Fatal("two active plans must be ambiguous")
	}
	for _, id := range []string{"plan-alpha", "plan-beta"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("error does not name %s: %v", id, err)
		}
	}
	const want = "--since handoff is ambiguous: plans plan-alpha and plan-beta are both active"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestSince_NoPlanErrors(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "no active plan", "## Files\n- server/src/auth.ts\n")
	addSinceHandoff(t, st, "old-plan", "done", "A", []string{bead.ID}, sinceTestWindow)

	_, _, err := runAuthority(t, "authority", bead.ID, "--since", "handoff")
	if err == nil || !strings.Contains(err.Error(), "--since handoff") {
		t.Fatalf("no active plan error must name the flag, got %v", err)
	}
}

func TestSince_NoHandoffOnLaneErrors(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "lane without handoff", "## Files\n- server/src/auth.ts\n")
	ctx := context.Background()
	if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: "active-plan", Title: "active plan"}); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if err := st.AddLane(ctx, &store.PlanLane{PlanID: "active-plan", Lane: "B", Queue: []string{bead.ID}}); err != nil {
		t.Fatalf("add lane: %v", err)
	}

	_, _, err := runAuthority(t, "authority", bead.ID, "--since", "handoff")
	if err == nil || !strings.Contains(err.Error(), "--since handoff") || !strings.Contains(err.Error(), "lane B") {
		t.Fatalf("missing lane handoff error must name the flag and lane, got %v", err)
	}
}

func TestSince_NullChangedAtFallsBackToCreatedAt(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "NULL changed_at", "## Files\n- server/src/auth.ts\n")
	oldRow := mkAuthStatement(t, st, authStatement{
		kind: "ruling", issueID: bead.ID, topic: "null-changed", text: "old NULL changed_at", createdAt: sinceTestWindow.Add(-time.Hour),
	})
	newRow := mkAuthStatement(t, st, authStatement{
		kind: "ruling", issueID: bead.ID, topic: "null-changed", text: "new NULL changed_at", createdAt: sinceTestWindow.Add(time.Hour),
	})
	setStatementChangedAt(t, st, oldRow.ID, nil)
	setStatementChangedAt(t, st, newRow.ID, nil)

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings",
		"--since", sinceTestWindow.Format(time.RFC3339))
	requireNoErr(t, err, errOut)
	if strings.Contains(out, "old NULL changed_at") || !strings.Contains(out, "new NULL changed_at") {
		t.Fatalf("NULL changed_at did not fall back to created_at:\n%s", out)
	}
}

func TestSince_EmptySectionSaysNothingSince(t *testing.T) {
	st := newTempAuthorityStore(t)
	oldBead := mkAuthIssue(t, st, "old findings", "## Files\n- server/src/auth.ts\n")
	oldAt := sinceTestWindow.Add(-time.Hour)
	oldRow := mkAuthStatement(t, st, authStatement{
		kind: "finding", issueID: oldBead.ID, topic: "old-findings", text: "outside the window", createdAt: oldAt,
	})
	setStatementChangedAt(t, st, oldRow.ID, &oldAt)

	out, errOut, err := runAuthority(t, "authority", oldBead.ID, "--kind", "findings",
		"--since", sinceTestWindow.Format(time.RFC3339))
	requireNoErr(t, err, errOut)
	section := briefSection(out, "FINDINGS")
	if len(section) != 2 || section[0] != "(since 2026-09-02 12:34)" || section[1] != "(nothing since the window)" {
		t.Fatalf("empty windowed section = %q, want header and empty marker:\n%s", section, out)
	}

	emptyBead := mkAuthIssue(t, st, "empty findings", "## Files\n- server/src/auth.ts\n")
	out, errOut, err = runAuthority(t, "authority", emptyBead.ID, "--kind", "findings")
	requireNoErr(t, err, errOut)
	if strings.Contains(out, "FINDINGS") {
		t.Fatalf("empty unwindowed section must remain absent:\n%s", out)
	}
}

func TestSince_RecentChangedAtOldCreatedAtRenders(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "recently changed ruling", "## Files\n- server/src/auth.ts\n")
	row := mkAuthStatement(t, st, authStatement{
		kind: "ruling", issueID: bead.ID, topic: "changed-window",
		text: "old ruling changed inside the window", createdAt: sinceTestWindow.Add(-24 * time.Hour),
	})
	changedAt := sinceTestWindow.Add(time.Hour)
	setStatementChangedAt(t, st, row.ID, &changedAt)

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings",
		"--since", sinceTestWindow.Format(time.RFC3339))
	requireNoErr(t, err, errOut)
	if !strings.Contains(out, "old ruling changed inside the window") {
		t.Fatalf("recent changed_at did not keep an old statement:\n%s", out)
	}
}
