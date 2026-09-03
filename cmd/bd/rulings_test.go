package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// runRulingsCmd executes `bd rulings <args>` against a bytes.Buffer, which
// is never a TTY — tests that need the human/verbose render must force it
// with t.Setenv(EnvTerse, "0").
func runRulingsCmd(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	root := newRulingsCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(args)
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

// mkRulingStatement writes a ruling directly via the store, bypassing actor
// gating (these tests exercise render shape, not `bd ruling add`'s gate).
func mkRulingStatement(t *testing.T, st *store.Store, issueID, text string) string {
	t.Helper()
	r := &beads.Statement{Kind: "ruling", IssueID: &issueID, Text: text, FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), r); err != nil {
		t.Fatalf("create ruling %q: %v", text, err)
	}
	return r.ID
}

func TestRulingsList_TerseOneLinePerRuling(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = dsn
	issue := mkIssueForRuling(t, st, "terse fixture")
	r1 := mkRulingStatement(t, st, issue.ID, "first ruling text")
	r2 := mkRulingStatement(t, st, issue.ID, "second ruling text")
	_ = st.Close()

	t.Setenv(EnvTerse, "1")
	out, _, err := runRulingsCmd(t, []string{issue.ID})
	if err != nil {
		t.Fatalf("rulings %s: %v", issue.ID, err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected one line per ruling (2), got %d:\n%s", len(lines), out)
	}
	// Order is the resolver's, not asserted here — each expected row must
	// appear somewhere in the output, once.
	for _, want := range []struct{ id, text string }{{r1, "first ruling text"}, {r2, "second ruling text"}} {
		wantLine := want.id + "  [" + issue.ID + "]  " + want.text
		if !strings.Contains(out, wantLine) {
			t.Fatalf("expected line %q, got:\n%s", wantLine, out)
		}
	}
	// No decorative headers, banners, or blank padding.
	for _, banned := range []string{"RULING", "ACTIVE", "MUST OBEY", "\n\n"} {
		if strings.Contains(out, banned) {
			t.Fatalf("terse output must carry no decorative framing (%q present), got:\n%s", banned, out)
		}
	}
}

func TestRulingsList_TerseProjectWide(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = dsn
	issue := mkIssueForRuling(t, st, "terse project-wide fixture")
	r1 := mkRulingStatement(t, st, issue.ID, "issue ruling")
	prID := &beads.Statement{Kind: "ruling", IssueID: nil, Text: "project ruling", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), prID); err != nil {
		t.Fatalf("create project ruling: %v", err)
	}
	_ = st.Close()

	t.Setenv(EnvTerse, "1")
	out, _, err := runRulingsCmd(t, nil)
	if err != nil {
		t.Fatalf("rulings: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), out)
	}
	joined := out
	if !strings.Contains(joined, r1) || !strings.Contains(joined, "["+issue.ID+"]") {
		t.Fatalf("missing issue-scoped ruling row, got:\n%s", joined)
	}
	if !strings.Contains(joined, prID.ID) || !strings.Contains(joined, "[project]") {
		t.Fatalf("missing project-scoped ruling row, got:\n%s", joined)
	}
}

func TestRulingsList_VerboseUnchangedFromCurrentFormat(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = dsn
	issue := mkIssueForRuling(t, st, "verbose fixture")
	r1 := mkRulingStatement(t, st, issue.ID, "first ruling text")
	r2 := mkRulingStatement(t, st, issue.ID, "second ruling text")
	_ = st.Close()

	// Force verbose (TTY) rendering regardless of the test harness's
	// buffer-backed, always-non-TTY stdout.
	t.Setenv(EnvTerse, "0")
	out, _, err := runRulingsCmd(t, []string{issue.ID})
	if err != nil {
		t.Fatalf("rulings %s: %v", issue.ID, err)
	}

	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	got1, err := st2.GetStatement(ctx, r1)
	if err != nil {
		t.Fatalf("get r1: %v", err)
	}
	got2, err := st2.GetStatement(ctx, r2)
	if err != nil {
		t.Fatalf("get r2: %v", err)
	}
	// Human render shape from cmd/bd/rulings.go: "<id>  <date>  <author>
	// <text>\n" for each ruling on its own issue (no scope bracket, since it
	// matches the queried issue). Order is the resolver's, not asserted
	// here. FiledBy "owner:tester" -> actorWord "owner" (beads-gc6.2 added
	// the author field to the headline).
	line1 := got1.ID + "  " + got1.CreatedAt.Format("2006-01-02") + "  owner  " + got1.Text
	line2 := got2.ID + "  " + got2.CreatedAt.Format("2006-01-02") + "  owner  " + got2.Text
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("expected exactly 2 lines, got:\n%q", out)
	}
	for _, want := range []string{line1, line2} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected line %q, got:\n%s", want, out)
		}
	}
}

func TestRulingsList_VerboseProjectWideUnchanged(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = dsn
	prID := &beads.Statement{Kind: "ruling", IssueID: nil, Text: "project ruling", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), prID); err != nil {
		t.Fatalf("create project ruling: %v", err)
	}
	_ = st.Close()

	t.Setenv(EnvTerse, "0")
	out, _, err := runRulingsCmd(t, nil)
	if err != nil {
		t.Fatalf("rulings: %v", err)
	}
	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	got, err := st2.GetStatement(ctx, prID.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	want := got.ID + "  " + got.CreatedAt.Format("2006-01-02") + "  owner  [project]  " + got.Text + "\n"
	if out != want {
		t.Fatalf("verbose project-wide render should match the pre-existing format\n want %q\n got  %q", want, out)
	}
}
