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

// newAgentsRepo makes a git repository in the working directory with two
// commits: one that changes a nested AGENTS.md and one that does not.
func newAgentsRepo(t *testing.T) (agentsSHA, otherSHA string) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	mustGit(t, root, "init")
	mustGit(t, root, "config", "user.email", "close@example.test")
	mustGit(t, root, "config", "user.name", "Close Test")
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("server/AGENTS.md", "# server\n- the answer\n")
	mustGit(t, root, "add", ".")
	mustGit(t, root, "commit", "-m", "rule into AGENTS.md")
	agentsSHA = mustGit(t, root, "rev-parse", "HEAD")
	write("server/main.go", "package main\n")
	mustGit(t, root, "add", ".")
	mustGit(t, root, "commit", "-m", "code only")
	otherSHA = mustGit(t, root, "rev-parse", "HEAD")
	return agentsSHA, otherSHA
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
		{"with --reason", "", []string{"--commit", agentsSHA, "--reason", "moot", "--note", "n"}, "takes no --reason or --of"},
		{"with --of", "", []string{"--commit", agentsSHA, "--of", "Q-1"}, "takes no --reason or --of"},
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
