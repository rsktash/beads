package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// Feature 22: bd comment retract — a comment is retracted by a mark, never
// deleted; a retracted comment leaves both untyped-history blocks and the
// CONTRACT-line count, and stays readable through --include-retracted and
// --json.

func mkRetractComment(t *testing.T, st *store.Store, issueID, text string) *beads.Comment {
	t.Helper()
	c := &beads.Comment{IssueID: issueID, Author: "alice", Text: text, CreatedAt: time.Now().UTC()}
	if err := st.AddComment(context.Background(), c); err != nil {
		t.Fatalf("AddComment %q: %v", text, err)
	}
	return c
}

func runRetractCmd(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	root := newCommentCmd()
	root.SetArgs(args)
	return captureStdoutStderr(func() error { return root.Execute() })
}

func retractViaCmd(t *testing.T, dsn, commentID, note string) {
	t.Helper()
	_ = dsn
	t.Setenv("BD_ACTOR", "coordinator")
	out, errStr, err := runRetractCmd(t, []string{"retract", commentID, "--note", note})
	if err != nil {
		t.Fatalf("retract %s: %v (out %q errStr %q)", commentID, err, out, errStr)
	}
	if !strings.Contains(out, commentID) {
		t.Fatalf("retract output should name the comment id, got %q", out)
	}
}

// Behaviour 1: retraction is a mark. The row stays, the text is untouched,
// retracted_at is set.
func TestRetract_MarksNotDeletes(t *testing.T) {
	dsn, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "marks not deletes", "body")
	c := mkRetractComment(t, st, issue.ID, "the exports cursor should be an offset")
	_ = st.Close()

	retractViaCmd(t, dsn, c.ID, "superseded by R-66")

	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st2.Close()
	var n int
	var text string
	var retractedAt sql.NullTime
	var by, note string
	if err := st2.DB().QueryRow(`SELECT COUNT(*), text, retracted_at, retracted_by, retract_note FROM comments WHERE id = ?`, c.ID).Scan(&n, &text, &retractedAt, &by, &note); err != nil {
		t.Fatalf("select comment: %v", err)
	}
	if n != 1 {
		t.Fatalf("retracted comment row must still exist, got %d rows", n)
	}
	if text != "the exports cursor should be an offset" {
		t.Fatalf("comment text must be untouched, got %q", text)
	}
	if !retractedAt.Valid {
		t.Fatalf("retracted_at must be set, got NULL")
	}
	if !strings.Contains(by, "coordinator") {
		t.Fatalf("retracted_by should carry the retracting actor, got %q", by)
	}
	if note != "superseded by R-66" {
		t.Fatalf("retract_note mismatch, got %q", note)
	}
}

// Behaviour 3: --note is required, same rule and wording as bd ruling retire.
func TestRetract_RequiresNote(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "requires note", "body")
	c := mkRetractComment(t, st, issue.ID, "noteless")
	_ = st.Close()

	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRetractCmd(t, []string{"retract", c.ID})
	if err == nil {
		t.Fatalf("expected error for missing --note")
	}
	if err.Error() != "--note is required" {
		t.Fatalf("expected %q, got %q", "--note is required", err.Error())
	}
}

// Behaviour 4: retraction is owner/coordinator only — the same refusal
// string as every actor-gated write, and nothing is written.
func TestRetract_RefusesExecutor(t *testing.T) {
	dsn, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "refuses executor", "body")
	c := mkRetractComment(t, st, issue.ID, "executor tries to erase")
	_ = st.Close()

	t.Setenv("BD_ACTOR", "executor")
	_, _, err := runRetractCmd(t, []string{"retract", c.ID, "--note", "should be refused"})
	if err == nil {
		t.Fatalf("expected executor refusal")
	}
	if !strings.Contains(err.Error(), "executors cannot file rulings") {
		t.Fatalf("error should be the executor refusal, got %q", err.Error())
	}

	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	var retractedAt sql.NullTime
	if err := st2.DB().QueryRow(`SELECT retracted_at FROM comments WHERE id = ?`, c.ID).Scan(&retractedAt); err != nil {
		t.Fatalf("select: %v", err)
	}
	if retractedAt.Valid {
		t.Fatalf("refusal must leave the comment un-retracted")
	}
}

// Behaviour 2: an unknown id is told apart from an already-retracted one.
func TestRetract_RefusesUnknownId(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "unknown id", "body")
	_ = mkRetractComment(t, st, issue.ID, "innocent bystander")
	_ = st.Close()

	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRetractCmd(t, []string{"retract", "11111111-2222-3333-4444-555555555555", "--note", "no such row"})
	if err == nil {
		t.Fatalf("expected error for unknown comment id")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown id error should say not found, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "already retracted") {
		t.Fatalf("unknown id error must not read as already retracted, got %q", err.Error())
	}
}

// Behaviour 2 + 8: a second retract is refused with its own error text, and
// there is no un-retract.
func TestRetract_RefusesTwice(t *testing.T) {
	dsn, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "refuses twice", "body")
	c := mkRetractComment(t, st, issue.ID, "retracted once")
	_ = st.Close()

	retractViaCmd(t, dsn, c.ID, "first retract")

	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRetractCmd(t, []string{"retract", c.ID, "--note", "second retract"})
	if err == nil {
		t.Fatalf("expected error retracting twice")
	}
	if !strings.Contains(err.Error(), "already retracted") {
		t.Fatalf("second retract should say already retracted, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "not found") {
		t.Fatalf("already-retracted error must not read as not found, got %q", err.Error())
	}
}

// Behaviour 5: absent from the NOTES / UNTYPED HISTORY block of a typed bead.
func TestRetract_HiddenFromShow(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "hidden from show", "body")
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "a ruling", IssueID: strPtr2(issue.ID)})
	retracted := mkRetractComment(t, st, issue.ID, "hidden from show text")
	mkRetractComment(t, st, issue.ID, "kept from show text")
	if err := st.RetractComment(context.Background(), retracted.ID, "coordinator:tester", "gone"); err != nil {
		t.Fatalf("RetractComment: %v", err)
	}

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	if !strings.Contains(out, "NOTES / UNTYPED HISTORY") {
		t.Fatalf("typed bead should render the NOTES block, got:\n%s", out)
	}
	if strings.Contains(out, "hidden from show text") {
		t.Fatalf("retracted comment must be absent from bd show, got:\n%s", out)
	}
	if !strings.Contains(out, "kept from show text") {
		t.Fatalf("kept comment should still render, got:\n%s", out)
	}
	if strings.Contains(out, "RETRACTED") {
		t.Fatalf("default show must not render a retract mark, got:\n%s", out)
	}
}

// Behaviour 5, legacy branch: a bead with zero statements takes the UNTYPED
// HISTORY banner path, and the retracted comment is hidden there too.
func TestRetract_HiddenFromLegacyHistoryBlock(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "legacy hidden", "legacy body")
	retracted := mkRetractComment(t, st, issue.ID, "legacy hidden text")
	mkRetractComment(t, st, issue.ID, "legacy kept text")
	if err := st.RetractComment(context.Background(), retracted.ID, "coordinator:tester", "gone"); err != nil {
		t.Fatalf("RetractComment: %v", err)
	}

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	if !strings.Contains(out, "UNTYPED HISTORY — provenance unknown") {
		t.Fatalf("statement-less bead should render the UNTYPED HISTORY banner, got:\n%s", out)
	}
	if strings.Contains(out, "legacy hidden text") {
		t.Fatalf("retracted comment must be absent from the legacy history block, got:\n%s", out)
	}
	if !strings.Contains(out, "legacy kept text") {
		t.Fatalf("kept comment should still render in the legacy block, got:\n%s", out)
	}
}

// Behaviour 5: the CONTRACT line count drops the retracted comment.
func TestRetract_NotCountedOnContractLine(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "count check", "body")
	c1 := mkRetractComment(t, st, issue.ID, "counted comment")
	mkRetractComment(t, st, issue.ID, "also counted comment")

	before := renderContractOutput(t, st, issue.ID, showOpts{})
	firstBefore := strings.SplitN(before, "\n", 2)[0]
	if !strings.Contains(firstBefore, "comments 2") {
		t.Fatalf("expected comments 2 before retract, got %q", firstBefore)
	}

	if err := st.RetractComment(context.Background(), c1.ID, "coordinator:tester", "drop from count"); err != nil {
		t.Fatalf("RetractComment: %v", err)
	}

	after := renderContractOutput(t, st, issue.ID, showOpts{})
	firstAfter := strings.SplitN(after, "\n", 2)[0]
	if !strings.Contains(firstAfter, "comments 1") {
		t.Fatalf("expected comments 1 after retract, got %q", firstAfter)
	}
	if strings.Contains(firstAfter, "comments 2") {
		t.Fatalf("retracted comment must leave the CONTRACT-line count, got %q", firstAfter)
	}
}

// Behaviour 6: --include-retracted shows the marked row with its original
// text; the default list hides it.
func TestRetract_VisibleWithFlag(t *testing.T) {
	dsn, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "visible with flag", "body")
	c := mkRetractComment(t, st, issue.ID, "flag visible original text")
	mkRetractComment(t, st, issue.ID, "flag kept text")
	_ = st.Close()

	retractViaCmd(t, dsn, c.ID, "superseded by R-66")

	bare, _, err := runRetractCmd(t, []string{"list", issue.ID})
	if err != nil {
		t.Fatalf("default list: %v", err)
	}
	if strings.Contains(bare, "flag visible original text") || strings.Contains(bare, "RETRACTED") {
		t.Fatalf("default list must hide the retracted comment, got:\n%s", bare)
	}
	if !strings.Contains(bare, "flag kept text") {
		t.Fatalf("default list should show kept comments, got:\n%s", bare)
	}

	t.Setenv("BD_ACTOR", "coordinator")
	withFlag, _, err := runRetractCmd(t, []string{"list", issue.ID, "--include-retracted"})
	if err != nil {
		t.Fatalf("list --include-retracted: %v", err)
	}
	if !strings.Contains(withFlag, "RETRACTED") {
		t.Fatalf("--include-retracted should render the mark, got:\n%s", withFlag)
	}
	if !strings.Contains(withFlag, "superseded by R-66") {
		t.Fatalf("--include-retracted should render the note, got:\n%s", withFlag)
	}
	if !strings.Contains(withFlag, "(original) flag visible original text") {
		t.Fatalf("--include-retracted should render the original text, got:\n%s", withFlag)
	}
}

// Behaviour 6: --json always returns retracted rows with their three fields,
// because a machine reader must see the whole record.
func TestRetract_VisibleInJSON(t *testing.T) {
	dsn, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "visible in json", "body")
	retracted := mkRetractComment(t, st, issue.ID, "json retracted text")
	kept := mkRetractComment(t, st, issue.ID, "json kept text")
	_ = st.Close()

	retractViaCmd(t, dsn, retracted.ID, "superseded by R-66")

	old := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = old })

	out, _, err := runRetractCmd(t, []string{"list", issue.ID})
	if err != nil {
		t.Fatalf("json list: %v", err)
	}
	for _, key := range []string{"retracted_at", "retracted_by", "retract_note"} {
		if !strings.Contains(out, key) {
			t.Fatalf("json output should carry %s on the retracted row, got:\n%s", key, out)
		}
	}
	var got []beads.Comment
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if len(got) != 2 {
		t.Fatalf("expected both comments in json, got %d", len(got))
	}
	for _, c := range got {
		if c.ID == retracted.ID {
			if c.RetractedAt == nil {
				t.Fatalf("retracted row json must carry retracted_at, got %+v", c)
			}
			if c.RetractedBy == "" {
				t.Fatalf("retracted row json must carry retracted_by, got %+v", c)
			}
			if !strings.Contains(c.RetractedBy, "coordinator") {
				t.Fatalf("retracted_by should carry the retracting actor, got %q", c.RetractedBy)
			}
			if c.RetractNote != "superseded by R-66" {
				t.Fatalf("retract_note mismatch, got %q", c.RetractNote)
			}
		}
		if c.ID == kept.ID && c.RetractedAt != nil {
			t.Fatalf("non-retracted row must have no retracted_at, got %+v", c)
		}
	}
}

// Behaviour 7: retracting bumps the bead's updated_at.
func TestRetract_BumpsIssueUpdatedAt(t *testing.T) {
	dsn, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "bumps updated_at", "body")
	c := mkRetractComment(t, st, issue.ID, "bump me")
	ctx := context.Background()
	before, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	_ = st.Close()

	time.Sleep(20 * time.Millisecond)
	retractViaCmd(t, dsn, c.ID, "bump")

	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st2.Close()
	after, err := st2.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("retract must bump the bead's updated_at, before %v after %v", before.UpdatedAt, after.UpdatedAt)
	}
}
