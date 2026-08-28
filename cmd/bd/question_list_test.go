package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rsktash/beads"
)

func TestQuestionList_EmptyProjectPrintsNoMatches(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	_ = st.Close()
	asOwner(t)
	out, _, err := runQuestionCmd(t, []string{"list"})
	if err != nil {
		t.Fatalf("list on an empty project must succeed: %v", err)
	}
	if strings.TrimSpace(out) != "questions: no matches" {
		t.Fatalf("expected the no-matches line alone, got %q", out)
	}
}

func TestQuestionList_ActiveOnlyByDefault(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	a := mkQFIssue(t, st, "bead a")
	b := mkQFIssue(t, st, "bead b")
	open1 := mkQuestion(t, st, a.ID, "still blocking a")
	open2 := mkQuestion(t, st, b.ID, "still blocking b")
	closed := mkQuestion(t, st, a.ID, "went moot")
	_ = st.Close()

	asOwner(t)
	if _, _, err := runQuestionCmd(t, []string{"close", closed, "--reason", "moot", "--note", "n"}); err != nil {
		t.Fatalf("close: %v", err)
	}

	out, _, err := runQuestionCmd(t, []string{"list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Contains(out, "no matches") {
		t.Fatalf("the no-matches line must not appear beside rows, got %q", out)
	}
	for _, want := range []string{open1, open2, a.ID, b.ID, "still blocking a", "still blocking b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list must contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, closed) {
		t.Fatalf("a closed question must not appear in the default listing, got:\n%s", out)
	}
	if lines := strings.Count(strings.TrimSpace(out), "\n") + 1; lines != 2 {
		t.Fatalf("expected 2 rows, got %d:\n%s", lines, out)
	}
}

func TestQuestionList_StatusSelectsClosedOnes(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	a := mkQFIssue(t, st, "bead a")
	live := mkQuestion(t, st, a.ID, "still open")
	moot := mkQuestion(t, st, a.ID, "went moot")
	old := mkQuestion(t, st, a.ID, "was reframed")
	newer := mkQuestion(t, st, a.ID, "the sharper one")
	_ = st.Close()

	asOwner(t)
	if _, _, err := runQuestionCmd(t, []string{"close", moot, "--reason", "moot", "--note", "n"}); err != nil {
		t.Fatalf("close moot: %v", err)
	}
	if _, _, err := runQuestionCmd(t, []string{"close", old, "--reason", "superseded", "--note", "n", "--of", newer}); err != nil {
		t.Fatalf("close superseded: %v", err)
	}

	out, _, err := runQuestionCmd(t, []string{"list", "--status", "retracted"})
	if err != nil {
		t.Fatalf("list retracted: %v", err)
	}
	if !strings.Contains(out, moot) || strings.Contains(out, live) || strings.Contains(out, old) {
		t.Fatalf("--status retracted should show only %s, got:\n%s", moot, out)
	}

	out, _, err = runQuestionCmd(t, []string{"list", "--status", "superseded"})
	if err != nil {
		t.Fatalf("list superseded: %v", err)
	}
	if !strings.Contains(out, old) || strings.Contains(out, moot) {
		t.Fatalf("--status superseded should show only %s, got:\n%s", old, out)
	}
	// The row names the replacing question and its kind.
	if !strings.Contains(out, "question: "+newer) {
		t.Fatalf("the row should name what answered it, got:\n%s", out)
	}

	if _, _, err := runQuestionCmd(t, []string{"list", "--status", "banana"}); err == nil {
		t.Fatalf("an unknown --status must be refused")
	}
}

func TestQuestionList_AnsweredRowNamesTheKind(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	a := mkQFIssue(t, st, "answered rows")
	q1 := mkQuestion(t, st, a.ID, "settled by evidence")
	q2 := mkQuestion(t, st, a.ID, "settled by judgment")
	fID := mkStatementQF(t, st, a.ID, "finding", "the evidence")
	rID := mkStatementQF(t, st, a.ID, "ruling", "the decision")
	_ = st.Close()

	asOwner(t)
	if _, _, err := runQuestionCmd(t, []string{"answer", q1, "--finding", fID}); err != nil {
		t.Fatalf("answer --finding: %v", err)
	}
	if _, _, err := runQuestionCmd(t, []string{"answer", q2, "--ruling", rID}); err != nil {
		t.Fatalf("answer --ruling: %v", err)
	}
	out, _, err := runQuestionCmd(t, []string{"list", "--status", "answered"})
	if err != nil {
		t.Fatalf("list answered: %v", err)
	}
	if !strings.Contains(out, "finding: "+fID) {
		t.Fatalf("row should read finding: %s, got:\n%s", fID, out)
	}
	if !strings.Contains(out, "ruling: "+rID) {
		t.Fatalf("row should read ruling: %s, got:\n%s", rID, out)
	}
}

func TestQuestionList_IssueScopedDoesNotInherit(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	parent := mkQFIssue(t, st, "the epic")
	child := mkQFIssue(t, st, "the child")
	dep := beads.Dependency{IssueID: child.ID, DependsOnID: parent.ID, Type: beads.DepParentChild}
	if err := st.AddDependency(context.Background(), dep); err != nil {
		t.Fatalf("add parent-child: %v", err)
	}
	parentQ := mkQuestion(t, st, parent.ID, "a question on the epic")
	childQ := mkQuestion(t, st, child.ID, "a question on the child")
	bare := mkQFIssue(t, st, "no questions")
	_ = st.Close()

	asOwner(t)
	out, _, err := runQuestionCmd(t, []string{"list", child.ID})
	if err != nil {
		t.Fatalf("list scoped: %v", err)
	}
	if !strings.Contains(out, childQ) {
		t.Fatalf("scoped list must show the bead's own question, got:\n%s", out)
	}
	if strings.Contains(out, parentQ) {
		t.Fatalf("questions do not inherit; the epic's question must not appear, got:\n%s", out)
	}

	// A bead with no questions of its own prints the no-matches line.
	out, _, err = runQuestionCmd(t, []string{"list", bare.ID})
	if err != nil {
		t.Fatalf("list bare: %v", err)
	}
	if strings.TrimSpace(out) != "questions: no matches" {
		t.Fatalf("expected the no-matches line, got %q", out)
	}

	// An unknown issue id is a failure, not a no-matches line.
	out, _, err = runQuestionCmd(t, []string{"list", "bd-nope"})
	if err == nil {
		t.Fatalf("an unknown issue id must fail")
	}
	if strings.Contains(out, "no matches") {
		t.Fatalf("a failure must not print the no-matches line, got %q", out)
	}
}

func TestQuestionList_OpenToExecutors(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	a := mkQFIssue(t, st, "executor reads")
	qID := mkQuestion(t, st, a.ID, "what blocks me?")
	_ = st.Close()

	t.Setenv("BD_ACTOR", "executor")
	out, _, err := runQuestionCmd(t, []string{"list"})
	if err != nil {
		t.Fatalf("list must be open to executors: %v", err)
	}
	if !strings.Contains(out, qID) {
		t.Fatalf("executor should see the blocked frontier, got:\n%s", out)
	}
}

func TestQuestionList_JsonCarriesFullRow(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	a := mkQFIssue(t, st, "json list")
	qID := mkQuestion(t, st, a.ID, "the json question")
	bare := mkQFIssue(t, st, "bare")
	_ = st.Close()
	asOwner(t)
	old := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = old })

	out, _, err := runQuestionCmd(t, []string{"list"})
	if err != nil {
		t.Fatalf("json list: %v", err)
	}
	var got []beads.Statement
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if len(got) != 1 || got[0].ID != qID {
		t.Fatalf("json list mismatch: %+v", got)
	}
	if got[0].Kind != "question" || got[0].Status != "active" || got[0].Text != "the json question" ||
		got[0].IssueID == nil || *got[0].IssueID != a.ID || got[0].FiledBy == "" || got[0].CreatedAt.IsZero() {
		t.Fatalf("json row should carry the full statement, got %+v", got[0])
	}

	// An empty result is an empty array, not null.
	out, _, err = runQuestionCmd(t, []string{"list", bare.ID})
	if err != nil {
		t.Fatalf("json list bare: %v", err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty json list should be [], got %q", out)
	}
}

func TestQuestionList_HelpStatesNoInheritance(t *testing.T) {
	out, _, _ := runQuestionCmd(t, []string{"list", "--help"})
	if !strings.Contains(out, "do not inherit") {
		t.Fatalf("list help must state that questions do not inherit, got:\n%s", out)
	}
}
