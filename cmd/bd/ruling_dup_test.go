package main

import (
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
)

// TestDup_PrintsExistingToStderr: two rulings on the bead, then a third —
// stderr holds both existing headlines, stdout holds only the new id.
func TestDup_PrintsExistingToStderr(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "dup print")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")

	if _, _, err := runRulingAdd(t, []string{"add", issue.ID, "first ruling headline"}); err != nil {
		t.Fatalf("r1: %v", err)
	}
	if _, _, err := runRulingAdd(t, []string{"add", issue.ID, "second ruling headline"}); err != nil {
		t.Fatalf("r2: %v", err)
	}
	out, errStr, err := runRulingAdd(t, []string{"add", issue.ID, "third ruling headline"})
	if err != nil {
		t.Fatalf("r3: %v", err)
	}
	if !strings.Contains(errStr, "first ruling headline") {
		t.Fatalf("stderr missing first headline, got %q", errStr)
	}
	if !strings.Contains(errStr, "second ruling headline") {
		t.Fatalf("stderr missing second headline, got %q", errStr)
	}
	trimmedOut := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmedOut, "R-") || strings.Contains(trimmedOut, "\n") {
		t.Fatalf("stdout should hold only the new id, got %q", out)
	}
}

// TestDup_RefusesExactRepeat: add the same text twice — the second returns
// an error naming the first id and --supersedes.
func TestDup_RefusesExactRepeat(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "dup repeat")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")

	out1, _, err := runRulingAdd(t, []string{"add", issue.ID, "do the thing"})
	if err != nil {
		t.Fatalf("r1: %v", err)
	}
	r1 := strings.TrimSpace(out1)

	_, _, err = runRulingAdd(t, []string{"add", issue.ID, "do the thing"})
	if err == nil {
		t.Fatalf("second identical ruling should be refused")
	}
	if !strings.Contains(err.Error(), r1) {
		t.Fatalf("error should name %s, got %q", r1, err.Error())
	}
	if !strings.Contains(err.Error(), "--supersedes") {
		t.Fatalf("error should mention --supersedes, got %q", err.Error())
	}
}

// TestDup_RefusalWritesNothing: after the refusal, ListStatements returns
// one row (the original), not two.
func TestDup_RefusalWritesNothing(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "dup writes nothing")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")

	if _, _, err := runRulingAdd(t, []string{"add", issue.ID, "keep it"}); err != nil {
		t.Fatalf("r1: %v", err)
	}
	if _, _, err := runRulingAdd(t, []string{"add", issue.ID, "keep it"}); err == nil {
		t.Fatalf("dup should be refused")
	}
	if n := countStatements(t, dsn); n != 1 {
		t.Fatalf("expected 1 statement after refusal, got %d", n)
	}
}

// TestDup_CaseAndWhitespaceInsensitive: "Do X" then "  do   x  " — refused.
func TestDup_CaseAndWhitespaceInsensitive(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "dup case")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")

	if _, _, err := runRulingAdd(t, []string{"add", issue.ID, "Do X"}); err != nil {
		t.Fatalf("r1: %v", err)
	}
	if _, _, err := runRulingAdd(t, []string{"add", issue.ID, "  do   x  "}); err == nil {
		t.Fatalf("case/whitespace variant should be refused as a duplicate")
	}
}

// TestDup_DiffersAfter120Chars: two texts identical for the first 120
// characters, different after — refused, documenting that the collision
// check compares at headline (120-rune) granularity, never full text.
func TestDup_DiffersAfter120Chars(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "dup 120")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")

	base := strings.Repeat("a", 120)
	text1 := base + " one distinct tail"
	text2 := base + " a completely different tail"

	if _, _, err := runRulingAdd(t, []string{"add", issue.ID, text1}); err != nil {
		t.Fatalf("r1: %v", err)
	}
	if _, _, err := runRulingAdd(t, []string{"add", issue.ID, text2}); err == nil {
		t.Fatalf("texts sharing their first 120 characters should be refused as a duplicate")
	}
}

// TestDup_AllowsWithSupersedes: same text with --supersedes succeeds — the
// check is skipped because superseding is the sanctioned way to restate.
func TestDup_AllowsWithSupersedes(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "dup supersedes")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")

	out1, _, err := runRulingAdd(t, []string{"add", issue.ID, "same text"})
	if err != nil {
		t.Fatalf("r1: %v", err)
	}
	r1 := strings.TrimSpace(out1)

	out2, _, err := runRulingAdd(t, []string{"add", issue.ID, "same text", "--supersedes", r1})
	if err != nil {
		t.Fatalf("supersede with identical text should succeed: %v", err)
	}
	if strings.TrimSpace(out2) == "" {
		t.Fatalf("expected a new id, got %q", out2)
	}
	if n := countStatements(t, dsn); n != 2 {
		t.Fatalf("expected 2 statements, got %d", n)
	}
}

// TestDup_IgnoresInherited: an identical ruling on the parent does not
// block the child's add — the set checked is the bead's own, never the
// inheritance-resolved one.
func TestDup_IgnoresInherited(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	ctx := context.Background()
	epic := &beads.Issue{Title: "epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, epic); err != nil {
		t.Fatalf("epic: %v", err)
	}
	child := &beads.Issue{Title: "child", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, child, nil); err != nil {
		t.Fatalf("child: %v", err)
	}
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")

	if _, _, err := runRulingAdd(t, []string{"add", epic.ID, "shared decision"}); err != nil {
		t.Fatalf("parent ruling: %v", err)
	}
	if _, _, err := runRulingAdd(t, []string{"add", child.ID, "shared decision"}); err != nil {
		t.Fatalf("child ruling identical to an inherited parent ruling should succeed: %v", err)
	}
	if n := countStatements(t, dsn); n != 2 {
		t.Fatalf("expected 2 statements, got %d", n)
	}
}

// TestDup_EmptySetPrintsNone: the first ruling on a bead prints "no
// existing rulings" to stderr.
func TestDup_EmptySetPrintsNone(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "dup empty")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")

	_, errStr, err := runRulingAdd(t, []string{"add", issue.ID, "first ever ruling"})
	if err != nil {
		t.Fatalf("r1: %v", err)
	}
	if !strings.Contains(errStr, "no existing rulings") {
		t.Fatalf("stderr should say no existing rulings, got %q", errStr)
	}
}
