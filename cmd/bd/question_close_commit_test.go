package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsktash/beads/store"
)

// newAgentsRepo makes a git repository and moves the working directory into
// it. Its two commits: the root one changes a nested AGENTS.md, the next does
// not.
func newAgentsRepo(t *testing.T) (agentsSHA, otherSHA string) {
	t.Helper()
	root, agentsSHA, otherSHA := newAgentsRepoAt(t)
	t.Chdir(root)
	return agentsSHA, otherSHA
}

func writeRepoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func newAgentsRepoAt(t *testing.T) (root, agentsSHA, otherSHA string) {
	t.Helper()
	root = t.TempDir()
	mustGit(t, root, "init")
	mustGit(t, root, "config", "user.email", "close@example.test")
	mustGit(t, root, "config", "user.name", "Close Test")
	write := func(rel, content string) { writeRepoFile(t, root, rel, content) }
	write("server/AGENTS.md", "# server\n- the answer\n")
	mustGit(t, root, "add", ".")
	mustGit(t, root, "commit", "-m", "rule into AGENTS.md")
	agentsSHA = mustGit(t, root, "rev-parse", "HEAD")
	write("server/main.go", "package main\n")
	mustGit(t, root, "add", ".")
	mustGit(t, root, "commit", "-m", "code only")
	otherSHA = mustGit(t, root, "rev-parse", "HEAD")
	return root, agentsSHA, otherSHA
}

func TestQuestionClose_CommitAnswers(t *testing.T) {
	agentsSHA, _ := newAgentsRepo(t)
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "commit close")
	qID := mkQuestion(t, st, issue.ID, "which way does the server go?")
	_ = st.Close()
	asOwner(t)

	if _, _, err := runQuestionCmd(t, []string{"close", qID, "--commit", agentsSHA[:10]}); err != nil {
		t.Fatalf("close --commit: %v", err)
	}
	got := getStatementQF(t, dsn, qID)
	if got.Status != "answered" {
		t.Fatalf("status = %s, want answered", got.Status)
	}
	if got.AnsweredCommit != agentsSHA {
		t.Fatalf("answered_commit = %q, want the full sha %q", got.AnsweredCommit, agentsSHA)
	}
	if got.AnsweredBy != nil || got.Evidence != "" {
		t.Fatalf("the commit has its own column: answered_by %v evidence %q", got.AnsweredBy, got.Evidence)
	}
	if !beadIsReady(t, dsn, issue.ID) {
		t.Fatalf("bead should be ready once its question is answered")
	}

	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st2.Close()
	var buf bytes.Buffer
	if err := printShowHuman(&buf, &cmdCtx{ctx: ctx, store: st2}, issue.ID, showOpts{}); err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(buf.String(), "answered by commit "+agentsSHA) {
		t.Fatalf("CLOSED QUESTIONS must name the commit, got:\n%s", buf.String())
	}
}

func TestQuestionClose_CommitRefusals(t *testing.T) {
	agentsSHA, otherSHA := newAgentsRepo(t)
	for _, tc := range []struct {
		name  string
		actor string
		args  []string
		want  string
	}{
		{"no AGENTS.md in the commit", "", []string{"--commit", otherSHA}, "changes no AGENTS.md"},
		{"unknown commit", "", []string{"--commit", "deadbeefdeadbeef"}, "is not a commit"},
		{"option-shaped commit", "", []string{"--commit=--all"}, "is not a commit"},
		{"with --reason", "", []string{"--commit", agentsSHA, "--reason", "moot"}, "takes no --reason, --of or --note"},
		{"with --of", "", []string{"--commit", agentsSHA, "--of", "Q-1"}, "takes no --reason, --of or --note"},
		{"with --note", "", []string{"--commit", agentsSHA, "--note", "n"}, "takes no --reason, --of or --note"},
		{"--repo without --commit", "", []string{"--reason", "moot", "--note", "n", "--repo", "."}, "--repo is only for --commit"},
		{"executor", "executor", []string{"--commit", agentsSHA}, "BD_ACTOR=executor"},
		{"neither --commit nor --reason", "", []string{"--note", "n"}, "--reason is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dsn, st := newTempQFStore(t, "bd")
			issue := mkQFIssue(t, st, "commit refusal")
			qID := mkQuestion(t, st, issue.ID, "q")
			_ = st.Close()
			if tc.actor == "" {
				asOwner(t)
			} else {
				t.Setenv("BD_ACTOR", tc.actor)
			}
			_, _, err := runQuestionCmd(t, append([]string{"close", qID}, tc.args...))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
			if got := getStatementQF(t, dsn, qID); got.Status != "active" || got.AnsweredCommit != "" {
				t.Fatalf("a refused close wrote: status %s commit %q", got.Status, got.AnsweredCommit)
			}
		})
	}
}

// TestQuestionClose_CommitInNamedRepo: bd runs from an outer folder while the
// AGENTS.md commit lives in a nested repository; --repo points git there.
func TestQuestionClose_CommitInNamedRepo(t *testing.T) {
	repo, agentsSHA, _ := newAgentsRepoAt(t)
	t.Chdir(t.TempDir())
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "nested repo")
	qID := mkQuestion(t, st, issue.ID, "q")
	_ = st.Close()
	asOwner(t)

	_, _, err := runQuestionCmd(t, []string{"close", qID, "--commit", agentsSHA})
	if err == nil || !strings.Contains(err.Error(), "not a commit in the current repository") {
		t.Fatalf("without --repo the sha is looked up in the working directory, got %v", err)
	}
	if _, _, err := runQuestionCmd(t, []string{"close", qID, "--commit", agentsSHA, "--repo", repo}); err != nil {
		t.Fatalf("close --commit --repo: %v", err)
	}
	if got := getStatementQF(t, dsn, qID); got.Status != "answered" || got.AnsweredCommit != agentsSHA {
		t.Fatalf("got status %s commit %q, want answered %s", got.Status, got.AnsweredCommit, agentsSHA)
	}
}

// TestQuestionClose_MergeCommitJudgedByFirstParent: a merge that brought an
// AGENTS.md change in closes the question; a merge that brought only code
// does not.
func TestQuestionClose_MergeCommitJudgedByFirstParent(t *testing.T) {
	repo, _, _ := newAgentsRepoAt(t)
	main := mustGit(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	merge := func(branch, rel string) string {
		mustGit(t, repo, "checkout", "-q", "-b", branch)
		writeRepoFile(t, repo, rel, branch+"\n")
		mustGit(t, repo, "add", ".")
		mustGit(t, repo, "commit", "-q", "-m", branch)
		mustGit(t, repo, "checkout", "-q", main)
		writeRepoFile(t, repo, "main-"+branch+".txt", "main side\n")
		mustGit(t, repo, "add", ".")
		mustGit(t, repo, "commit", "-q", "-m", "main moves on")
		mustGit(t, repo, "merge", "-q", "--no-ff", "--no-edit", branch)
		return mustGit(t, repo, "rev-parse", "HEAD")
	}
	agentsMerge := merge("rule", "client/AGENTS.md")
	codeMerge := merge("code", "client/main.go")

	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "merge close")
	q1 := mkQuestion(t, st, issue.ID, "q1")
	q2 := mkQuestion(t, st, issue.ID, "q2")
	_ = st.Close()
	asOwner(t)

	if _, _, err := runQuestionCmd(t, []string{"close", q1, "--commit", agentsMerge, "--repo", repo}); err != nil {
		t.Fatalf("a merge bringing an AGENTS.md change must close: %v", err)
	}
	if got := getStatementQF(t, dsn, q1); got.AnsweredCommit != agentsMerge {
		t.Fatalf("answered_commit = %q, want %q", got.AnsweredCommit, agentsMerge)
	}
	_, _, err := runQuestionCmd(t, []string{"close", q2, "--commit", codeMerge, "--repo", repo})
	if err == nil || !strings.Contains(err.Error(), "changes no AGENTS.md") {
		t.Fatalf("a merge bringing only code must be refused, got %v", err)
	}
}
