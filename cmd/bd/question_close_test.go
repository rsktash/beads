package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// mkQuestion creates an active question on the issue and returns its id.
func mkQuestion(t *testing.T, st *store.Store, issueID, text string) string {
	t.Helper()
	q := &beads.Statement{Kind: "question", IssueID: &issueID, Text: text, FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), q); err != nil {
		t.Fatalf("create question %q: %v", text, err)
	}
	return q.ID
}

// asOwner clears BD_ACTOR so the caller is the owner.
func asOwner(t *testing.T) {
	t.Helper()
	orig, had := os.LookupEnv("BD_ACTOR")
	os.Unsetenv("BD_ACTOR")
	t.Cleanup(func() {
		if had {
			os.Setenv("BD_ACTOR", orig)
		} else {
			os.Unsetenv("BD_ACTOR")
		}
	})
}

// beadIsReady reports whether the bead appears in the ready set.
func beadIsReady(t *testing.T, dsn, issueID string) bool {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open ready: %v", err)
	}
	defer st.Close()
	ready, err := st.Ready(ctx)
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	for _, r := range ready {
		if r.ID == issueID {
			return true
		}
	}
	return false
}

func TestQuestionClose_MootRetractsAndUnblocks(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "moot close")
	qID := mkQuestion(t, st, issue.ID, "does this still matter?")
	_ = st.Close()

	if beadIsReady(t, dsn, issue.ID) {
		t.Fatalf("bead should be blocked while the question is active")
	}

	asOwner(t)
	out, _, err := runQuestionCmd(t, []string{"close", qID, "--reason", "moot", "--note", "the feature was dropped"})
	if err != nil {
		t.Fatalf("close moot should succeed: %v out %q", err, out)
	}
	got := getStatementQF(t, dsn, qID)
	if got.Status != "retracted" {
		t.Fatalf("moot should write retracted, got %s", got.Status)
	}
	if got.AnsweredBy != nil {
		t.Fatalf("moot should leave answered_by nil, got %v", got.AnsweredBy)
	}
	reason, note, ok := decodeClosure(got.Evidence)
	if !ok || reason != "moot" || note != "the feature was dropped" {
		t.Fatalf("closure record wrong: evidence %q -> reason %q note %q ok %v", got.Evidence, reason, note, ok)
	}
	if !beadIsReady(t, dsn, issue.ID) {
		t.Fatalf("bead should be ready after the question was closed as moot")
	}
}

func TestQuestionClose_DuplicateRetractsAndLinks(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "duplicate close")
	survivor := mkQuestion(t, st, issue.ID, "the surviving question")
	dup := mkQuestion(t, st, issue.ID, "the duplicate question")
	_ = st.Close()

	asOwner(t)
	_, _, err := runQuestionCmd(t, []string{"close", dup, "--reason", "duplicate", "--note", "same as the other one", "--of", survivor})
	if err != nil {
		t.Fatalf("close duplicate should succeed: %v", err)
	}
	got := getStatementQF(t, dsn, dup)
	if got.Status != "retracted" {
		t.Fatalf("duplicate should write retracted, got %s", got.Status)
	}
	if got.AnsweredBy == nil || *got.AnsweredBy != survivor {
		t.Fatalf("duplicate should link to the survivor %s, got %v", survivor, got.AnsweredBy)
	}
	// The survivor is untouched and still blocks.
	surv := getStatementQF(t, dsn, survivor)
	if surv.Status != "active" || surv.AnsweredBy != nil || surv.Evidence != "" {
		t.Fatalf("survivor should be untouched, got %+v", surv)
	}
	if beadIsReady(t, dsn, issue.ID) {
		t.Fatalf("bead should stay blocked while the surviving question is active")
	}
}

func TestQuestionClose_SupersededWritesSupersededStatus(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "superseded close")
	old := mkQuestion(t, st, issue.ID, "the vague question")
	replacement := mkQuestion(t, st, issue.ID, "the sharper question")
	_ = st.Close()

	asOwner(t)
	_, _, err := runQuestionCmd(t, []string{"close", old, "--reason", "superseded", "--note", "reframed after reading the code", "--of", replacement})
	if err != nil {
		t.Fatalf("close superseded should succeed: %v", err)
	}
	got := getStatementQF(t, dsn, old)
	if got.Status != "superseded" {
		t.Fatalf("superseded should write superseded, got %s", got.Status)
	}
	if got.AnsweredBy == nil || *got.AnsweredBy != replacement {
		t.Fatalf("superseded should link to %s, got %v", replacement, got.AnsweredBy)
	}
	reason, note, ok := decodeClosure(got.Evidence)
	if !ok || reason != "superseded" || note != "reframed after reading the code" {
		t.Fatalf("closure record wrong: %q", got.Evidence)
	}
	// The reason and the status are the same word here; the line must not double it.
	line := formatClosedQuestionLine(*got, map[string]string{replacement: "question"})
	if strings.Contains(line, "superseded (superseded)") {
		t.Fatalf("the reason must not repeat the status, got %q", line)
	}
	for _, want := range []string{"superseded", "question: " + replacement, "reframed after reading the code"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line must contain %q, got %q", want, line)
		}
	}
}

func TestQuestionClose_ExecutorRefusedAndNothingWritten(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "executor refused")
	qID := mkQuestion(t, st, issue.ID, "may I unblock myself?")
	_ = st.Close()
	before := getStatementQF(t, dsn, qID)

	t.Setenv("BD_ACTOR", "executor")
	_, _, err := runQuestionCmd(t, []string{"close", qID, "--reason", "moot", "--note", "I decided it does not matter"})
	if err == nil {
		t.Fatalf("executor must be refused")
	}
	want := "BD_ACTOR=executor: executors cannot file rulings; executors may file findings or questions instead"
	if err.Error() != want {
		t.Fatalf("refusal must match bd ruling add verbatim\n want %q\n got  %q", want, err.Error())
	}
	after := getStatementQF(t, dsn, qID)
	if after.Status != before.Status || after.Evidence != before.Evidence || after.AnsweredBy != nil {
		t.Fatalf("refused close must write nothing, before %+v after %+v", before, after)
	}
	if beadIsReady(t, dsn, issue.ID) {
		t.Fatalf("bead must stay blocked after a refused close")
	}
}

func TestQuestionClose_CoordinatorAllowed(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "coordinator close")
	qID := mkQuestion(t, st, issue.ID, "q")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	if _, _, err := runQuestionCmd(t, []string{"close", qID, "--reason", "moot", "--note", "no longer applies"}); err != nil {
		t.Fatalf("coordinator should be allowed: %v", err)
	}
	if getStatementQF(t, dsn, qID).Status != "retracted" {
		t.Fatalf("coordinator close should have applied")
	}
}

func TestQuestionClose_NoteRequired(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "note required")
	qID := mkQuestion(t, st, issue.ID, "q")
	_ = st.Close()
	asOwner(t)

	// Missing --note entirely.
	if _, _, err := runQuestionCmd(t, []string{"close", qID, "--reason", "moot"}); err == nil {
		t.Fatalf("missing --note must be refused")
	}
	// Whitespace-only --note.
	_, _, err := runQuestionCmd(t, []string{"close", qID, "--reason", "moot", "--note", "   "})
	if err == nil {
		t.Fatalf("blank --note must be refused")
	}
	if !strings.Contains(err.Error(), "note") {
		t.Fatalf("error should name --note, got %q", err.Error())
	}
	if getStatementQF(t, dsn, qID).Status != "active" {
		t.Fatalf("question must be unchanged after a refused close")
	}
}

func TestQuestionClose_OfRulesPerReason(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "of rules")
	a := mkQuestion(t, st, issue.ID, "a")
	b := mkQuestion(t, st, issue.ID, "b")
	_ = st.Close()
	asOwner(t)

	// moot rejects --of
	_, _, err := runQuestionCmd(t, []string{"close", a, "--reason", "moot", "--note", "n", "--of", b})
	if err == nil || !strings.Contains(err.Error(), "--of") {
		t.Fatalf("moot with --of must be refused naming --of, got %v", err)
	}
	// duplicate requires --of
	_, _, err = runQuestionCmd(t, []string{"close", a, "--reason", "duplicate", "--note", "n"})
	if err == nil || !strings.Contains(err.Error(), "--of") {
		t.Fatalf("duplicate without --of must be refused naming --of, got %v", err)
	}
	// superseded requires --of
	_, _, err = runQuestionCmd(t, []string{"close", a, "--reason", "superseded", "--note", "n"})
	if err == nil || !strings.Contains(err.Error(), "--of") {
		t.Fatalf("superseded without --of must be refused naming --of, got %v", err)
	}
	if getStatementQF(t, dsn, a).Status != "active" {
		t.Fatalf("question must be unchanged after refused closes")
	}
}

func TestQuestionClose_OfMustBeAnExistingQuestion(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "of validation")
	q := mkQuestion(t, st, issue.ID, "q")
	f := &beads.Statement{Kind: "finding", IssueID: &issue.ID, Text: "a finding", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), f); err != nil {
		t.Fatalf("create finding: %v", err)
	}
	_ = st.Close()
	asOwner(t)

	if _, _, err := runQuestionCmd(t, []string{"close", q, "--reason", "duplicate", "--note", "n", "--of", "Q-9999"}); err == nil {
		t.Fatalf("--of naming a missing statement must be refused")
	}
	_, _, err := runQuestionCmd(t, []string{"close", q, "--reason", "duplicate", "--note", "n", "--of", f.ID})
	if err == nil || !strings.Contains(err.Error(), "not a question") {
		t.Fatalf("--of naming a finding must be refused, got %v", err)
	}
	if _, _, err := runQuestionCmd(t, []string{"close", q, "--reason", "duplicate", "--note", "n", "--of", q}); err == nil {
		t.Fatalf("--of naming the question itself must be refused")
	}
	if getStatementQF(t, dsn, q).Status != "active" {
		t.Fatalf("question must be unchanged after refused closes")
	}
}

func TestQuestionClose_BadTargets(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "bad targets")
	f := &beads.Statement{Kind: "finding", IssueID: &issue.ID, Text: "a finding", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), f); err != nil {
		t.Fatalf("create finding: %v", err)
	}
	q := mkQuestion(t, st, issue.ID, "q")
	_ = st.Close()
	asOwner(t)

	// Missing target.
	if _, _, err := runQuestionCmd(t, []string{"close", "Q-9999", "--reason", "moot", "--note", "n"}); err == nil {
		t.Fatalf("missing target must be refused")
	}
	// Not a question.
	_, _, err := runQuestionCmd(t, []string{"close", f.ID, "--reason", "moot", "--note", "n"})
	if err == nil || !strings.Contains(err.Error(), "not a question") {
		t.Fatalf("non-question target must be refused, got %v", err)
	}
	if getStatementQF(t, dsn, f.ID).Status != "active" {
		t.Fatalf("finding must be untouched")
	}
	// Invalid reason.
	_, _, err = runQuestionCmd(t, []string{"close", q, "--reason", "banana", "--note", "n"})
	if err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("invalid reason must be refused naming --reason, got %v", err)
	}
	// Already closed.
	if _, _, err := runQuestionCmd(t, []string{"close", q, "--reason", "moot", "--note", "first"}); err != nil {
		t.Fatalf("first close: %v", err)
	}
	_, _, err = runQuestionCmd(t, []string{"close", q, "--reason", "moot", "--note", "second"})
	if err == nil {
		t.Fatalf("closing an already-closed question must be refused")
	}
	if !strings.Contains(err.Error(), "retracted") {
		t.Fatalf("error should name the current status, got %q", err.Error())
	}
	got := getStatementQF(t, dsn, q)
	if _, note, _ := decodeClosure(got.Evidence); note != "first" {
		t.Fatalf("the second close must not overwrite the first record, got %q", got.Evidence)
	}
}

func TestQuestionClose_StillRendersInShow(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "renders after close")
	qID := mkQuestion(t, st, issue.ID, "the question that went moot")
	_ = st.Close()
	asOwner(t)
	if _, _, err := runQuestionCmd(t, []string{"close", qID, "--reason", "moot", "--note", "scope was cut"}); err != nil {
		t.Fatalf("close: %v", err)
	}

	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st2.Close()
	cc := &cmdCtx{ctx: ctx, store: st2}
	var buf bytes.Buffer
	if err := printShowHuman(&buf, cc, issue.ID, showOpts{}); err != nil {
		t.Fatalf("show: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "CLOSED QUESTIONS — NO LONGER BLOCKING") {
		t.Fatalf("show must carry the closed-questions heading, got:\n%s", out)
	}
	if strings.Contains(out, "OPEN QUESTIONS — EXECUTION BLOCKERS") {
		t.Fatalf("a closed question must not render as an open blocker, got:\n%s", out)
	}
	for _, want := range []string{qID, "the question that went moot", "retracted", "moot", "scope was cut"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show must contain %q, got:\n%s", want, out)
		}
	}
}

func TestQuestionClose_JsonPrintsUpdated(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "json close")
	qID := mkQuestion(t, st, issue.ID, "q")
	_ = st.Close()
	asOwner(t)
	old := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = old })
	out, _, err := runQuestionCmd(t, []string{"close", qID, "--reason", "moot", "--note", "n"})
	if err != nil {
		t.Fatalf("json close: %v", err)
	}
	var got beads.Statement
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if got.ID != qID || got.Status != "retracted" {
		t.Fatalf("json close mismatch: %+v", got)
	}
	_ = dsn
}

func TestQuestionClose_HelpNamesReasonsAndLink(t *testing.T) {
	root := newQuestionCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"close", "--help"})
	_ = root.Execute()
	out := buf.String()
	for _, want := range []string{"moot", "duplicate", "superseded", "--note", "answered_by"} {
		if !strings.Contains(out, want) {
			t.Fatalf("close help should mention %q, got:\n%s", want, out)
		}
	}
}
