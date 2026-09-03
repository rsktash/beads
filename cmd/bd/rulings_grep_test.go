package main

import (
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// mkRulingStatementWithLaw is mkRulingStatement's sibling that also sets
// Law, for TestGrep_MatchesLaw's fixture (a ruling whose law carries the
// keyword and whose text does not).
func mkRulingStatementWithLaw(t *testing.T, st *store.Store, issueID, law, text string) string {
	t.Helper()
	r := &beads.Statement{Kind: "ruling", IssueID: &issueID, Text: text, Law: law, FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), r); err != nil {
		t.Fatalf("create ruling %q: %v", text, err)
	}
	return r.ID
}

func TestGrep_MatchesText(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = dsn
	issue := mkIssueForRuling(t, st, "grep text fixture")
	rQuota := mkRulingStatement(t, st, issue.ID, "the quota is fixed at 10")
	mkRulingStatement(t, st, issue.ID, "first unrelated ruling")
	mkRulingStatement(t, st, issue.ID, "second unrelated ruling")
	_ = st.Close()

	t.Setenv(EnvTerse, "1")
	out, _, err := runRulingsCmd(t, []string{"--grep", "quota"})
	if err != nil {
		t.Fatalf("rulings --grep quota: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one matching line, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(out, rQuota) {
		t.Fatalf("expected match %q in output, got:\n%s", rQuota, out)
	}
}

func TestGrep_MatchesLaw(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = dsn
	issue := mkIssueForRuling(t, st, "grep law fixture")
	rLaw := mkRulingStatementWithLaw(t, st, issue.ID, "quota enforcement", "text carries no keyword at all")
	_ = st.Close()

	t.Setenv(EnvTerse, "1")
	out, _, err := runRulingsCmd(t, []string{"--grep", "quota"})
	if err != nil {
		t.Fatalf("rulings --grep quota: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one matching line, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(out, rLaw) {
		t.Fatalf("expected match %q in output, got:\n%s", rLaw, out)
	}
}

func TestGrep_CaseInsensitive(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = dsn
	issue := mkIssueForRuling(t, st, "grep case fixture")
	rQuota := mkRulingStatement(t, st, issue.ID, "the Quota is fixed at 10")
	_ = st.Close()

	t.Setenv(EnvTerse, "1")
	out, _, err := runRulingsCmd(t, []string{"--grep", "QUOTA"})
	if err != nil {
		t.Fatalf("rulings --grep QUOTA: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one matching line, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(out, rQuota) {
		t.Fatalf("expected match %q in output, got:\n%s", rQuota, out)
	}
}

func TestGrep_CrossesScopes(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = dsn
	issue := mkIssueForRuling(t, st, "grep cross-scope fixture")
	rIssue := mkRulingStatement(t, st, issue.ID, "issue-scoped quota ruling")
	prID := &beads.Statement{Kind: "ruling", IssueID: nil, Text: "project-scoped quota ruling", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), prID); err != nil {
		t.Fatalf("create project ruling: %v", err)
	}
	_ = st.Close()

	t.Setenv(EnvTerse, "1")
	out, _, err := runRulingsCmd(t, []string{"--grep", "quota"})
	if err != nil {
		t.Fatalf("rulings --grep quota: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 matching lines (both scopes), got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(out, rIssue) || !strings.Contains(out, prID.ID) {
		t.Fatalf("expected both %q and %q in output, got:\n%s", rIssue, prID.ID, out)
	}
}

func TestGrep_RejectsIssueArg(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = dsn
	issue := mkIssueForRuling(t, st, "grep reject fixture")
	mkRulingStatement(t, st, issue.ID, "quota ruling")
	_ = st.Close()

	_, _, err := runRulingsCmd(t, []string{"--grep", "x", issue.ID})
	if err == nil {
		t.Fatalf("expected an error combining --grep with an issue id")
	}
	if !strings.Contains(err.Error(), "drop the issue id") {
		t.Fatalf("expected error to contain %q, got %q", "drop the issue id", err.Error())
	}
}

func TestGrep_NoMatchIsSilentExitZero(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = dsn
	issue := mkIssueForRuling(t, st, "grep no-match fixture")
	mkRulingStatement(t, st, issue.ID, "unrelated ruling")
	_ = st.Close()

	t.Setenv(EnvTerse, "1")
	out, _, err := runRulingsCmd(t, []string{"--grep", "zzz"})
	if err != nil {
		t.Fatalf("rulings --grep zzz: %v", err)
	}
	if out != "" {
		t.Fatalf("expected empty stdout, got:\n%s", out)
	}
}

func TestRulingListAlias_IdenticalOutput(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = dsn
	issue := mkIssueForRuling(t, st, "alias fixture")
	mkRulingStatement(t, st, issue.ID, "first quota ruling")
	mkRulingStatement(t, st, issue.ID, "second unrelated ruling")
	prID := &beads.Statement{Kind: "ruling", IssueID: nil, Text: "project quota ruling", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), prID); err != nil {
		t.Fatalf("create project ruling: %v", err)
	}
	_ = st.Close()

	t.Setenv(EnvTerse, "1")
	args := []string{"--grep", "quota"}
	outRulings, _, err := runRulingsCmd(t, args)
	if err != nil {
		t.Fatalf("rulings --grep quota: %v", err)
	}

	buf, _, err := runRulingAdd(t, append([]string{"list"}, args...))
	if err != nil {
		t.Fatalf("ruling list --grep quota: %v", err)
	}
	if buf != outRulings {
		t.Fatalf("bd ruling list output differs from bd rulings\n rulings:   %q\n ruling list: %q", outRulings, buf)
	}
}
