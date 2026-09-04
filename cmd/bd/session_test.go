package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

const testSessionID = "2f4f7030-4c46-4f20-b27c-c0d7c48f1d3c"

// sessionBase is the fixed instant every session fixture hangs off, so a row's
// stamp and the claim floor never depend on wall-clock ordering.
var sessionBase = time.Date(2026, 9, 4, 2, 11, 0, 0, time.UTC)

func runSessionClose(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	cmd := newSessionCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	cmd.SetOut(bufOut)
	cmd.SetErr(bufErr)
	cmd.SetArgs(append([]string{"close"}, args...))
	err := cmd.Execute()
	return bufOut.String(), bufErr.String(), err
}

// reopenSession opens a second handle on the fixture DSN for the assertions
// that read or stamp rows after a command has run.
func reopenSession(t *testing.T, dsn string) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("reopen %s: %v", dsn, err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// mkSessionComment writes a comment at the given instant and stamps it with the
// session, the way the annotate hook does in production.
func mkSessionComment(t *testing.T, st *store.Store, issueID, author, text string, at time.Time, session string) string {
	t.Helper()
	ctx := context.Background()
	c := &beads.Comment{IssueID: issueID, Author: author, Text: text, CreatedAt: at}
	if err := st.AddComment(ctx, c); err != nil {
		t.Fatalf("add comment %q: %v", text, err)
	}
	if session != "" {
		if err := st.AnnotateComment(ctx, c.ID, session, "msg-"+c.ID, "toolu_"+c.ID, ""); err != nil {
			t.Fatalf("annotate comment %s: %v", c.ID, err)
		}
	}
	return c.ID
}

// mkSessionStatement files a statement at the given instant and stamps it with
// the session.
func mkSessionStatement(t *testing.T, st *store.Store, kind, issueID, filedBy, text, status string, at time.Time, session string) string {
	t.Helper()
	ctx := context.Background()
	s := &beads.Statement{Kind: kind, IssueID: &issueID, Text: text, FiledBy: filedBy, Status: status, Scope: "inherit", CreatedAt: at}
	if err := st.CreateStatement(ctx, s); err != nil {
		t.Fatalf("create %s %q: %v", kind, text, err)
	}
	if session != "" {
		if err := st.AnnotateStatement(ctx, s.ID, session, "msg-"+s.ID, "toolu_"+s.ID, ""); err != nil {
			t.Fatalf("annotate statement %s: %v", s.ID, err)
		}
	}
	return s.ID
}

// setIssueState stamps status, assignee and updated_at straight onto the row:
// CreateIssue always overwrites updated_at with the wall clock, and the claim
// floor comparison needs a fixed instant.
func setIssueState(t *testing.T, st *store.Store, issueID, status, assignee string, updatedAt time.Time) {
	t.Helper()
	res, err := st.DB().Exec(`UPDATE issues SET status = ?, assignee = ?, updated_at = ? WHERE id = ?`, status, assignee, updatedAt, issueID)
	if err != nil {
		t.Fatalf("set issue state %s: %v", issueID, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("set issue state %s: %d rows affected", issueID, n)
	}
}

func countRows(t *testing.T, st *store.Store, table string) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestSessionClose_OwnerDecisionWithoutRuling(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "owner decision without ruling")
	mkSessionComment(t, st, issue.ID, "owner:tester", "ruled: the cursor helper stays pure", sessionBase, testSessionID)
	_ = st.Close()

	out, _, err := runSessionClose(t, []string{"--session", testSessionID})
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	if !strings.Contains(out, "OWNER DECISIONS WITH NO RULING (1)") {
		t.Fatalf("expected one owner decision, got:\n%s", out)
	}
	if !strings.Contains(out, "ruled: the cursor helper stays pure") {
		t.Fatalf("expected the comment text, got:\n%s", out)
	}
	wantCmd := "bd ruling add " + issue.ID + ` "<text>" --topic <slug>`
	if !strings.Contains(out, wantCmd) {
		t.Fatalf("expected prepared command %q, got:\n%s", wantCmd, out)
	}
}

func TestSessionClose_OwnerDecisionWithRulingOmitted(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "owner decision already typed")
	mkSessionComment(t, st, issue.ID, "owner:tester", "ruled: the cursor helper stays pure", sessionBase, testSessionID)
	mkSessionStatement(t, st, "ruling", issue.ID, "owner:tester", "the cursor helper stays pure", "active", sessionBase.Add(time.Minute), testSessionID)
	_ = st.Close()

	out, _, err := runSessionClose(t, []string{"--session", testSessionID})
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	if !strings.Contains(out, "OWNER DECISIONS WITH NO RULING (0)") {
		t.Fatalf("a typed decision must not be listed, got:\n%s", out)
	}
	if strings.Contains(out, "bd ruling add "+issue.ID) {
		t.Fatalf("no prepared ruling command expected, got:\n%s", out)
	}
}

func TestSessionClose_IgnoresNonPrefixComment(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "non-prefix comment")
	mkSessionComment(t, st, issue.ID, "owner:tester", "looks fine", sessionBase, testSessionID)
	_ = st.Close()

	out, _, err := runSessionClose(t, []string{"--session", testSessionID})
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	if !strings.Contains(out, "OWNER DECISIONS WITH NO RULING (0)") {
		t.Fatalf("a comment with no acceptance prefix must not be listed, got:\n%s", out)
	}
	if strings.Contains(out, "looks fine") {
		t.Fatalf("comment text leaked into the report:\n%s", out)
	}
}

func TestSessionClose_IgnoresAgentComment(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "agent comment carries the same prefix")
	_ = st.Close()

	const ownerText = "ruled: the owner decided this one"
	const agentText = "ruled: the agent only echoed it"

	// t.Setenv registers the restore; unsetting after it is how the owner path
	// (BD_ACTOR unset) is exercised without leaking the ambient value.
	t.Setenv("BD_ACTOR", "executor")
	if err := os.Unsetenv("BD_ACTOR"); err != nil {
		t.Fatalf("unset BD_ACTOR: %v", err)
	}
	runCommentAddForSession(t, issue.ID, ownerText)
	t.Setenv("BD_ACTOR", "executor")
	runCommentAddForSession(t, issue.ID, agentText)

	st2 := reopenSession(t, dsn)
	ctx := context.Background()
	comments, err := st2.ListComments(ctx, issue.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	for _, c := range comments {
		if err := st2.AnnotateComment(ctx, c.ID, testSessionID, "msg-"+c.ID, "toolu_"+c.ID, ""); err != nil {
			t.Fatalf("annotate %s: %v", c.ID, err)
		}
	}

	out, _, err := runSessionClose(t, []string{"--session", testSessionID})
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	if strings.Contains(out, agentText) {
		t.Fatalf("an agent comment must not be listed, got:\n%s", out)
	}
	if !strings.Contains(out, ownerText) {
		t.Fatalf("the owner comment must still be listed, got:\n%s", out)
	}
	if !strings.Contains(out, "OWNER DECISIONS WITH NO RULING (1)") {
		t.Fatalf("expected exactly one owner decision, got:\n%s", out)
	}
}

// runCommentAddForSession drives the real `bd comment add` so the author comes
// from the same actor resolution production uses.
func runCommentAddForSession(t *testing.T, issueID, text string) {
	t.Helper()
	cmd := newCommentCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"add", issueID, text})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("comment add %q: %v", text, err)
	}
}

func TestSessionClose_OpenQuestionsTouched(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "open question touched")
	qID := mkSessionStatement(t, st, "question", issue.ID, "executor:tester", "Does the roster fixture need the demo supplier?", "active", sessionBase, testSessionID)
	_ = st.Close()

	out, _, err := runSessionClose(t, []string{"--session", testSessionID})
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	if !strings.Contains(out, "OPEN QUESTIONS TOUCHED (1)") {
		t.Fatalf("expected one open question, got:\n%s", out)
	}
	if !strings.Contains(out, qID) || !strings.Contains(out, "executor") {
		t.Fatalf("expected the question id and author, got:\n%s", out)
	}
	wantCmd := "bd question close " + qID + ` --reason moot --note "<why>"`
	if !strings.Contains(out, wantCmd) {
		t.Fatalf("expected prepared command %q, got:\n%s", wantCmd, out)
	}
}

func TestSessionClose_AnsweredQuestionOmitted(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "answered question omitted")
	qID := mkSessionStatement(t, st, "question", issue.ID, "executor:tester", "Is this still blocking?", "answered", sessionBase, testSessionID)
	_ = st.Close()

	out, _, err := runSessionClose(t, []string{"--session", testSessionID})
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	if !strings.Contains(out, "OPEN QUESTIONS TOUCHED (0)") {
		t.Fatalf("an answered question must not be listed, got:\n%s", out)
	}
	if strings.Contains(out, "bd question close "+qID) {
		t.Fatalf("no prepared question command expected, got:\n%s", out)
	}
}

func TestSessionClose_ClaimedNotClosed(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	work := mkIssueForRuling(t, st, "claimed but not closed")
	mkSessionComment(t, st, work.ID, "owner:tester", "[status] starting the walk", sessionBase, testSessionID)
	setIssueState(t, st, work.ID, "in_progress", "rustam / opus-5", sessionBase.Add(30*time.Minute))
	_ = st.Close()

	out, _, err := runSessionClose(t, []string{"--session", testSessionID})
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	if !strings.Contains(out, "CLAIMED BUT NOT CLOSED (1)") {
		t.Fatalf("expected one claim, got:\n%s", out)
	}
	if !strings.Contains(out, work.ID+"  [in_progress]  assignee rustam / opus-5") {
		t.Fatalf("expected the claim row, got:\n%s", out)
	}
	wantCmd := "bd close " + work.ID + ` --reason "<what landed>"`
	if !strings.Contains(out, wantCmd) {
		t.Fatalf("expected prepared command %q, got:\n%s", wantCmd, out)
	}
}

func TestSessionClose_ClosedBeadOmitted(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	work := mkIssueForRuling(t, st, "closed bead omitted")
	mkSessionComment(t, st, work.ID, "owner:tester", "[status] starting the walk", sessionBase, testSessionID)
	setIssueState(t, st, work.ID, "closed", "rustam / opus-5", sessionBase.Add(30*time.Minute))
	_ = st.Close()

	out, _, err := runSessionClose(t, []string{"--session", testSessionID})
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	if !strings.Contains(out, "CLAIMED BUT NOT CLOSED (0)") {
		t.Fatalf("a closed bead must not be listed, got:\n%s", out)
	}
	if strings.Contains(out, "bd close "+work.ID) {
		t.Fatalf("no prepared close command expected, got:\n%s", out)
	}
}

func TestSessionClose_EmptySectionsPrintNone(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	_ = mkIssueForRuling(t, st, "a bead this session never touched")
	_ = st.Close()

	out, _, err := runSessionClose(t, []string{"--session", testSessionID})
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	for _, want := range []string{
		"SESSION " + testSessionID + "  closing",
		"OWNER DECISIONS WITH NO RULING (0)",
		"OPEN QUESTIONS TOUCHED (0)",
		"CLAIMED BUT NOT CLOSED (0)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "  (none)"); n != 3 {
		t.Fatalf("expected 3 (none) lines, got %d:\n%s", n, out)
	}
}

func TestSessionClose_WritesNothing(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "session close writes nothing")
	mkSessionComment(t, st, issue.ID, "owner:tester", "ruled: this one stays untyped", sessionBase, testSessionID)
	mkSessionStatement(t, st, "question", issue.ID, "executor:tester", "Still blocked on the fixture?", "active", sessionBase, testSessionID)
	setIssueState(t, st, issue.ID, "in_progress", "rustam / opus-5", sessionBase.Add(30*time.Minute))
	_ = st.Close()

	before := reopenSession(t, dsn)
	counts := map[string]int{}
	for _, tbl := range []string{"statements", "comments", "issues"} {
		counts[tbl] = countRows(t, before, tbl)
	}

	out, _, err := runSessionClose(t, []string{"--session", testSessionID})
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	if !strings.Contains(out, "(1)") {
		t.Fatalf("fixture should have produced findings, got:\n%s", out)
	}

	for _, tbl := range []string{"statements", "comments", "issues"} {
		if got := countRows(t, before, tbl); got != counts[tbl] {
			t.Fatalf("%s row count changed: %d → %d", tbl, counts[tbl], got)
		}
	}
}

func TestSessionClose_NoSessionIdErrors(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	_ = mkIssueForRuling(t, st, "no session id")
	_ = st.Close()

	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("BD_SESSION_ID", "")
	_, _, err := runSessionClose(t, nil)
	if err == nil {
		t.Fatal("expected an error with no session id resolvable")
	}
	if !strings.Contains(err.Error(), "no session id") {
		t.Fatalf("error should say no session id, got %q", err.Error())
	}
}

func TestSessionClose_ResolvesSessionFromEnv(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "session id from the environment")
	mkSessionComment(t, st, issue.ID, "owner:tester", "approved the keyset cursor", sessionBase, testSessionID)
	_ = st.Close()

	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("BD_SESSION_ID", testSessionID)
	out, _, err := runSessionClose(t, nil)
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	if !strings.Contains(out, "approved the keyset cursor") {
		t.Fatalf("expected the decision found via $BD_SESSION_ID, got:\n%s", out)
	}

	t.Setenv("CLAUDE_SESSION_ID", "some-other-session")
	out, _, err = runSessionClose(t, nil)
	if err != nil {
		t.Fatalf("session close: %v", err)
	}
	if strings.Contains(out, "approved the keyset cursor") {
		t.Fatalf("$CLAUDE_SESSION_ID must win over $BD_SESSION_ID, got:\n%s", out)
	}
}
