package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newTempRulingStore(t *testing.T, prefix string) (string, *store.Store) {
	t.Helper()
	isolateExpandState(t)
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "ruling.db")
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetConfig(ctx, store.CfgIssuePrefix, prefix); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	// Ensure flagDB points at this DSN for openStore inside command
	old := flagDB
	flagDB = dsn
	t.Cleanup(func() {
		flagDB = old
		_ = st.Close()
	})
	return dsn, st
}

func mkIssueForRuling(t *testing.T, st *store.Store, title string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	return i
}

func countStatements(t *testing.T, dsn string) int {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open count: %v", err)
	}
	defer st.Close()
	list, err := st.ListStatements(ctx, store.StatementFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return len(list)
}

// fixtureTopic is the slug the pre-topics fixtures file under. Feature 10 made
// --topic required on ruling, question and finding add; the fixtures below
// predate it and assert nothing about topics, so the argument is added once in
// the shared runners rather than at every call site. One slug for all three
// kinds keeps `--answers` inheritance consistent: a ruling answering a fixture
// question inherits the same slug it would have been given.
const fixtureTopic = "fixture-topic"

// withFixtureTopic appends --topic to an add invocation that does not carry
// one. A test exercising the missing-topic refusal drives the commands through
// newRoot instead (topics_test.go), so it is never reached by this.
func withFixtureTopic(args []string) []string {
	if len(args) == 0 || args[0] != "add" {
		return args
	}
	for _, a := range args {
		if a == "--topic" {
			return args
		}
	}
	return append(append([]string{}, args...), "--topic", fixtureTopic)
}

func runRulingAdd(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	args = withFixtureTopic(args)
	root := newRulingCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(args)
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

func TestResolveActor_UnsetIsOwner(t *testing.T) {
	orig, had := os.LookupEnv("BD_ACTOR")
	if had {
		os.Unsetenv("BD_ACTOR")
		t.Cleanup(func() { os.Setenv("BD_ACTOR", orig) })
	}
	identity, isExec := resolveActor()
	if isExec {
		t.Fatalf("unset BD_ACTOR should not be executor, got %q isExec=%v", identity, isExec)
	}
	if !strings.Contains(strings.ToLower(identity), "owner") {
		t.Fatalf("unset identity should contain owner, got %q", identity)
	}
	if !strings.Contains(identity, assigneeFromEnv()) {
		t.Fatalf("identity should contain user %q, got %q", assigneeFromEnv(), identity)
	}
}

func TestResolveActor_CoordinatorAllowed(t *testing.T) {
	t.Setenv("BD_ACTOR", "coordinator")
	identity, isExec := resolveActor()
	if isExec {
		t.Fatalf("coordinator should not be executor, got %q", identity)
	}
	if !strings.Contains(identity, "coordinator") {
		t.Fatalf("coordinator identity should contain coordinator, got %q", identity)
	}
}

func TestResolveActor_ExecutorRefused(t *testing.T) {
	t.Setenv("BD_ACTOR", "executor")
	identity, isExec := resolveActor()
	if !isExec {
		t.Fatalf("executor should be executor, got %q", identity)
	}
	if !strings.Contains(identity, "executor") {
		t.Fatalf("executor identity should contain executor, got %q", identity)
	}
}

func TestResolveActor_EmptyStringIsExecutor(t *testing.T) {
	t.Setenv("BD_ACTOR", "")
	_, isExec := resolveActor()
	if !isExec {
		t.Fatalf("empty BD_ACTOR explicitly set should be executor")
	}
}

func TestResolveActor_OtherValueIsExecutor(t *testing.T) {
	val := "banana"
	t.Setenv("BD_ACTOR", val)
	identity, isExec := resolveActor()
	if !isExec {
		t.Fatalf("%q should be executor", val)
	}
	if !strings.Contains(identity, val) {
		t.Fatalf("identity %q should contain %q", identity, val)
	}
}

// TestRuling_AddRefusedForEveryActor: rulings are legacy, so ruling add is
// refused whoever runs it — owner, coordinator, executor — on every path,
// --doctrine included, and writes nothing.
func TestRuling_AddRefusedForEveryActor(t *testing.T) {
	for _, actor := range []string{"", "coordinator", "executor"} {
		for _, args := range [][]string{
			{"add", "ISSUE", "ruling text"},
			{"add", "a project ruling"},
			{"add", "the text", "--doctrine", "--concern", "authority", "--law", "A law."},
		} {
			name := actor + "/" + strings.Join(args, " ")
			t.Run(name, func(t *testing.T) {
				dsn, st := newTempRulingStore(t, "bd")
				issue := mkIssueForRuling(t, st, "refused")
				_ = st.Close()
				if actor == "" {
					t.Setenv("BD_ACTOR", "")
					os.Unsetenv("BD_ACTOR")
				} else {
					t.Setenv("BD_ACTOR", actor)
				}
				run := append([]string{}, args...)
				for i, a := range run {
					if a == "ISSUE" {
						run[i] = issue.ID
					}
				}
				out, errStr, err := runRulingAdd(t, run)
				if err == nil {
					t.Fatalf("ruling add must be refused, got out %q errStr %q", out, errStr)
				}
				if err != errRulingsClosed {
					t.Fatalf("refusal = %v, want errRulingsClosed", err)
				}
				for _, want := range []string{"legacy", "AGENTS.md", "--commit"} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("refusal %q must name %q", err.Error(), want)
					}
				}
				if n := countStatements(t, dsn); n != 0 {
					t.Fatalf("a refused ruling add wrote %d statements", n)
				}
			})
		}
	}
}
