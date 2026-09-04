package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func runAnnotate(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	cmd := newAnnotateCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	cmd.SetOut(bufOut)
	cmd.SetErr(bufErr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return bufOut.String(), bufErr.String(), err
}

// readStatementPointer reads the three provenance columns straight off the row.
func readStatementPointer(t *testing.T, dsn, id string) store.ProvenancePointer {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	p, err := st.StatementPointer(ctx, id)
	if err != nil {
		t.Fatalf("statement pointer %s: %v", id, err)
	}
	return p
}

func mkCommentForAnnotate(t *testing.T, st *store.Store, issueID, text string) string {
	t.Helper()
	c := &beads.Comment{IssueID: issueID, Author: "owner:tester", Text: text}
	if err := st.AddComment(context.Background(), c); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	return c.ID
}

func TestAnnotate_WritesPointer(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "annotate writes pointer")
	rID := mkRulingStatement(t, st, issue.ID, "a ruling to annotate")
	_ = st.Close()

	if _, _, err := runAnnotate(t, []string{rID,
		"--session", "2f4f7030-4c46-4f20-b27c-c0d7c48f1d3c",
		"--msg", "6539661a-7f47-4f73-bea2-cdaa25777e16",
		"--tool", "toolu_015HjFDCcRMbXHtkpyqdbt45",
	}); err != nil {
		t.Fatalf("annotate: %v", err)
	}

	got := readStatementPointer(t, dsn, rID)
	want := store.ProvenancePointer{
		SessionID: "2f4f7030-4c46-4f20-b27c-c0d7c48f1d3c",
		MsgID:     "6539661a-7f47-4f73-bea2-cdaa25777e16",
		ToolUseID: "toolu_015HjFDCcRMbXHtkpyqdbt45",
	}
	if got != want {
		t.Fatalf("pointer mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestAnnotate_OpenToExecutor(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "annotate open to executor")
	rID := mkRulingStatement(t, st, issue.ID, "the hook annotates, not a person")
	_ = st.Close()

	t.Setenv("BD_ACTOR", "executor")
	if _, _, err := runAnnotate(t, []string{rID, "--session", "sess-1", "--msg", "msg-1", "--tool", "toolu_1"}); err != nil {
		t.Fatalf("executor must be allowed to annotate, got: %v", err)
	}
	if got := readStatementPointer(t, dsn, rID); got.SessionID != "sess-1" {
		t.Fatalf("expected pointer written by executor, got %+v", got)
	}
}

func TestAnnotate_RequiresAllThree(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "annotate requires all three")
	rID := mkRulingStatement(t, st, issue.ID, "a ruling")
	_ = st.Close()

	_, _, err := runAnnotate(t, []string{rID, "--session", "sess-1", "--msg", "msg-1"})
	if err == nil {
		t.Fatal("expected an error when --tool is omitted")
	}
	for _, want := range []string{"--session", "--msg", "--tool"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should name %s, got %q", want, err.Error())
		}
	}
}

func TestAnnotate_Overwrites(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "annotate overwrites")
	rID := mkRulingStatement(t, st, issue.ID, "a ruling annotated twice")
	_ = st.Close()

	if _, _, err := runAnnotate(t, []string{rID, "--session", "old", "--msg", "old-msg", "--tool", "toolu_old"}); err != nil {
		t.Fatalf("first annotate: %v", err)
	}
	if _, _, err := runAnnotate(t, []string{rID, "--session", "new", "--msg", "new-msg", "--tool", "toolu_new"}); err != nil {
		t.Fatalf("second annotate: %v", err)
	}

	got := readStatementPointer(t, dsn, rID)
	want := store.ProvenancePointer{SessionID: "new", MsgID: "new-msg", ToolUseID: "toolu_new"}
	if got != want {
		t.Fatalf("second pointer should win: got %+v want %+v", got, want)
	}
}

func TestAnnotate_Comment(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "annotate a comment")
	cID := mkCommentForAnnotate(t, st, issue.ID, "an untyped comment")
	_ = st.Close()

	if _, _, err := runAnnotate(t, []string{cID, "--session", "sess-c", "--msg", "msg-c", "--tool", "toolu_c"}); err != nil {
		t.Fatalf("annotate comment: %v", err)
	}

	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st2.Close()
	var sess, msg, tool string
	if err := st2.DB().QueryRow(`SELECT session_id, msg_id, tool_use_id FROM comments WHERE id = ?`, cID).Scan(&sess, &msg, &tool); err != nil {
		t.Fatalf("select comment columns: %v", err)
	}
	if sess != "sess-c" || msg != "msg-c" || tool != "toolu_c" {
		t.Fatalf("comment columns not set: %q %q %q", sess, msg, tool)
	}
}

func TestAnnotate_UnknownIDIsNotFound(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	_ = mkIssueForRuling(t, st, "annotate unknown id")
	_ = st.Close()

	_, _, err := runAnnotate(t, []string{"R-nosuchthing", "--session", "s", "--msg", "m", "--tool", "t"})
	if err == nil {
		t.Fatal("expected an error for an unknown id")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Fatalf("expected ErrNotFound, got %q", err.Error())
	}
}

func TestAnnotate_TranscriptStoredOnStatement(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "annotate transcript statement")
	rID := mkRulingStatement(t, st, issue.ID, "a ruling with a transcript path")
	_ = st.Close()

	if _, _, err := runAnnotate(t, []string{rID,
		"--session", "sess-t", "--msg", "msg-t", "--tool", "toolu_t",
		"--transcript", "/tmp/other-project/sess-t.jsonl",
	}); err != nil {
		t.Fatalf("annotate: %v", err)
	}
	got := readStatementPointer(t, dsn, rID)
	if got.TranscriptPath != "/tmp/other-project/sess-t.jsonl" {
		t.Fatalf("expected the transcript path stored, got %+v", got)
	}
}

func TestAnnotate_TranscriptStoredOnComment(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "annotate transcript comment")
	cID := mkCommentForAnnotate(t, st, issue.ID, "a comment with a transcript path")
	_ = st.Close()

	if _, _, err := runAnnotate(t, []string{cID,
		"--session", "sess-ct", "--msg", "msg-ct", "--tool", "toolu_ct",
		"--transcript", "/tmp/other-project/sess-ct.jsonl",
	}); err != nil {
		t.Fatalf("annotate comment: %v", err)
	}

	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st2.Close()
	p, err := st2.CommentPointer(ctx, cID)
	if err != nil {
		t.Fatalf("comment pointer: %v", err)
	}
	if p.TranscriptPath != "/tmp/other-project/sess-ct.jsonl" {
		t.Fatalf("expected the transcript path stored on the comment, got %+v", p)
	}
}

func TestAnnotate_TranscriptOmittedStoresEmpty(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "annotate transcript omitted")
	rID := mkRulingStatement(t, st, issue.ID, "a rule annotated without a transcript")
	_ = st.Close()

	if _, _, err := runAnnotate(t, []string{rID, "--session", "sess-o", "--msg", "msg-o", "--tool", "toolu_o"}); err != nil {
		t.Fatalf("annotate: %v", err)
	}
	got := readStatementPointer(t, dsn, rID)
	if got.TranscriptPath != "" {
		t.Fatalf("an annotate without --transcript must store '', got %+v", got)
	}
}

func TestAnnotate_TranscriptOmittedClearsStored(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "annotate transcript cleared")
	rID := mkRulingStatement(t, st, issue.ID, "a rule annotated twice, second time without a transcript")
	_ = st.Close()

	if _, _, err := runAnnotate(t, []string{rID,
		"--session", "sess-x", "--msg", "msg-x", "--tool", "toolu_x",
		"--transcript", "/tmp/other-project/sess-x.jsonl",
	}); err != nil {
		t.Fatalf("first annotate: %v", err)
	}
	if _, _, err := runAnnotate(t, []string{rID, "--session", "sess-x", "--msg", "msg-x", "--tool", "toolu_x"}); err != nil {
		t.Fatalf("second annotate: %v", err)
	}
	got := readStatementPointer(t, dsn, rID)
	if got.TranscriptPath != "" {
		t.Fatalf("a second annotate without --transcript must clear the stored path, got %+v", got)
	}
}
