package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// mkExpiryQuestion files an active question with an exact created_at and an
// exact changed_at: nil clears the stamp CreateStatement writes, the shape a
// question idle since filing carries.
func mkExpiryQuestion(t *testing.T, st *store.Store, issueID, text string, createdAt time.Time, changedAt *time.Time) string {
	t.Helper()
	q := &beads.Statement{
		Kind: "question", IssueID: &issueID, Text: text,
		FiledBy: "owner:tester", Status: "active", Scope: "inherit",
		CreatedAt: createdAt,
	}
	if err := st.CreateStatement(context.Background(), q); err != nil {
		t.Fatalf("create question %q: %v", text, err)
	}
	setStatementChangedAt(t, st, q.ID, changedAt)
	return q.ID
}

// expiryRowAndCommand finds the row for qID and returns it with the line
// printed under it, pinning the two-line stale row.
func expiryRowAndCommand(t *testing.T, out, qID string) (row, under string) {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, qID+"  ") {
			if i+1 >= len(lines) {
				t.Fatalf("row for %s has no line under it:\n%s", qID, out)
			}
			return l, lines[i+1]
		}
	}
	t.Fatalf("no row for %s in:\n%s", qID, out)
	return "", ""
}

func TestExpiry_MarksStaleQuestion(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	a := mkQFIssue(t, st, "stale holder")
	qID := mkExpiryQuestion(t, st, a.ID, "the stale fork", time.Now().UTC().Add(-30*24*time.Hour), nil)
	_ = st.Close()

	asOwner(t)
	out, _, err := runQuestionCmd(t, []string{"list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	row, under := expiryRowAndCommand(t, out, qID)
	if !strings.Contains(row, "possibly moot (30d)") {
		t.Fatalf("the stale row must carry the marker, got %q", row)
	}
	wantCmd := "bd question close " + qID + " --reason moot --note \"<why>\""
	if strings.TrimSpace(under) != wantCmd {
		t.Fatalf("the line under the row must be the prepared command %q, got %q", wantCmd, under)
	}
}

func TestExpiry_FreshQuestionUnmarked(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	a := mkQFIssue(t, st, "fresh holder")
	qID := mkExpiryQuestion(t, st, a.ID, "the fresh fork", time.Now().UTC().Add(-time.Hour), ptrTime(time.Now().UTC()))
	_ = st.Close()

	asOwner(t)
	out, _, err := runQuestionCmd(t, []string{"list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	row, under := expiryRowAndCommand(t, out, qID)
	if strings.Contains(row, "possibly moot") {
		t.Fatalf("a fresh question must carry no marker, got %q", row)
	}
	if strings.Contains(under, "bd question close") {
		t.Fatalf("a fresh question must carry no prepared command, got %q", under)
	}
}

func TestExpiry_UsesChangedAtWhenSet(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	a := mkQFIssue(t, st, "changed holder")
	qID := mkExpiryQuestion(t, st, a.ID, "old filing, recent change",
		time.Now().UTC().Add(-30*24*time.Hour), ptrTime(time.Now().UTC()))
	_ = st.Close()

	asOwner(t)
	out, _, err := runQuestionCmd(t, []string{"list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	row, under := expiryRowAndCommand(t, out, qID)
	if strings.Contains(row, "possibly moot") {
		t.Fatalf("a recent changed_at must unmark the row, got %q", row)
	}
	if strings.Contains(under, "bd question close") {
		t.Fatalf("a recent changed_at must carry no prepared command, got %q", under)
	}
}

func TestExpiry_StaleDaysZeroDisables(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	a := mkQFIssue(t, st, "zero window")
	qID := mkExpiryQuestion(t, st, a.ID, "stale but unmarked", time.Now().UTC().Add(-30*24*time.Hour), nil)
	_ = st.Close()

	asOwner(t)
	out, _, err := runQuestionCmd(t, []string{"list", "--stale-days", "0"})
	if err != nil {
		t.Fatalf("list --stale-days 0: %v", err)
	}
	row, under := expiryRowAndCommand(t, out, qID)
	if strings.Contains(row, "possibly moot") {
		t.Fatalf("--stale-days 0 must disable the marker, got %q", row)
	}
	if strings.Contains(under, "bd question close") {
		t.Fatalf("--stale-days 0 must carry no prepared command, got %q", under)
	}
}

func TestExpiry_AnsweredQuestionUnmarked(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	a := mkQFIssue(t, st, "answered holder")
	qID := mkExpiryQuestion(t, st, a.ID, "settled while old", time.Now().UTC().Add(-30*24*time.Hour), nil)
	fID := mkStatementQF(t, st, a.ID, "finding", "the evidence that settled it")
	_ = st.Close()

	asOwner(t)
	if _, _, err := runQuestionCmd(t, []string{"answer", qID, "--finding", fID}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	// Clear the changed_at the answer stamped, so only the status can keep
	// the marker off the row.
	st2, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	setStatementChangedAt(t, st2, qID, nil)
	_ = st2.Close()

	out, _, err := runQuestionCmd(t, []string{"list", "--status", "answered"})
	if err != nil {
		t.Fatalf("list answered: %v", err)
	}
	row, under := expiryRowAndCommand(t, out, qID)
	if strings.Contains(row, "possibly moot") {
		t.Fatalf("only active questions carry the marker, got %q", row)
	}
	if strings.Contains(under, "bd question close") {
		t.Fatalf("an answered question carries no prepared command, got %q", under)
	}
}

func TestExpiry_SameMarkerInShowAndAuthority(t *testing.T) {
	st := newTempAuthorityStore(t)
	issue := mkAuthIssue(t, st, "marker parity", "")
	qID := mkExpiryQuestion(t, st, issue.ID, "which marker reads true?",
		time.Now().UTC().Add(-30*24*time.Hour), nil)

	showOut := renderContractOutput(t, st, issue.ID, showOpts{})
	authOut, errOut, err := runAuthority(t, "authority", issue.ID)
	requireNoErr(t, err, errOut)
	listOut, _, err := runQuestionCmd(t, []string{"list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	marker := "possibly moot (30d)"
	command := "bd question close " + qID + " --reason moot --note \"<why>\""
	for name, out := range map[string]string{"bd show": showOut, "bd authority": authOut, "bd question list": listOut} {
		if !strings.Contains(out, marker) {
			t.Fatalf("%s is missing the marker %q:\n%s", name, marker, out)
		}
		if !strings.Contains(out, command) {
			t.Fatalf("%s is missing the prepared command %q:\n%s", name, command, out)
		}
	}
}

func TestExpiry_StillBlocksReady(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	a := mkQFIssue(t, st, "blocked while stale")
	_ = mkExpiryQuestion(t, st, a.ID, "the stale fork", time.Now().UTC().Add(-30*24*time.Hour), nil)
	_ = st.Close()

	asOwner(t)
	out, _, err := captureStdoutStderr(func() error {
		cmd := newReadyCmd()
		cmd.SetArgs([]string{})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	if strings.Contains(out, a.ID) {
		t.Fatalf("a stale question still blocks its bead from bd ready, got:\n%s", out)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
