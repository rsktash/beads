package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/internal/config"
	"github.com/rsktash/beads/store"
)

type citationCLIFixture struct {
	root string
	dsn  string
	st   *store.Store
}

func newCitationCLIFixture(t *testing.T, withGit bool) citationCLIFixture {
	t.Helper()
	isolateExpandState(t)
	root := t.TempDir()
	beadDir := filepath.Join(root, ".bd")
	if err := os.MkdirAll(beadDir, 0o755); err != nil {
		t.Fatalf("mkdir .bd: %v", err)
	}
	if withGit {
		mustGit(t, root, "init")
		mustGit(t, root, "config", "user.email", "citation@example.test")
		mustGit(t, root, "config", "user.name", "Citation Test")
		path := filepath.Join(root, "tracked.txt")
		if err := os.WriteFile(path, []byte("initial\n"), 0o644); err != nil {
			t.Fatalf("write tracked file: %v", err)
		}
		mustGit(t, root, "add", "tracked.txt")
		mustGit(t, root, "commit", "-m", "initial")
	}
	dsn := filepath.Join(beadDir, "citation.db")
	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.SetConfig(context.Background(), store.CfgIssuePrefix, "bd"); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	oldDB := flagDB
	flagDB = dsn
	t.Setenv(config.EnvDir, beadDir)
	t.Setenv("BD_ACTOR", "coordinator")
	t.Cleanup(func() {
		flagDB = oldDB
		_ = st.Close()
	})
	return citationCLIFixture{root: root, dsn: dsn, st: st}
}

func mustGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func runCitationSource(t *testing.T, id string) (string, error) {
	t.Helper()
	cmd := newSourceCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{id})
	err := cmd.Execute()
	return out.String(), err
}

func TestCite_BriefRendersStates(t *testing.T) {
	fx := newCitationCLIFixture(t, false)
	issue := mkIssueForRuling(t, fx.st, "brief states")
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(fx.root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	write("src/live.go", "func live() {}\n")
	write("src/moved.go", "\n\nfunc moved() {}\n")
	for _, text := range []string{
		"src/live.go::live:1",
		"src/moved.go::moved:1",
		"src/gone.go::stale:5",
	} {
		st := &beads.Statement{Kind: "ruling", IssueID: &issue.ID, Text: text, FiledBy: "owner:tester"}
		if err := fx.st.CreateStatement(context.Background(), st); err != nil {
			t.Fatalf("create ruling: %v", err)
		}
	}

	out, _, err := runAuthority(t, "authority", issue.ID, "--kind", "rulings")
	if err != nil {
		t.Fatalf("authority: %v", err)
	}
	for _, want := range []string{
		"cite src/live.go::live:1  live",
		"cite src/moved.go::moved:1  moved -> :3",
		"cite src/gone.go::stale:5  stale (bd source R-3 recovers it)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("brief missing %q:\n%s", want, out)
		}
	}
}

func TestCite_SourceRecoversViaGitShow(t *testing.T) {
	fx := newCitationCLIFixture(t, true)
	path := filepath.Join(fx.root, "src", "gone.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("package src\n\nfunc recovered() {}\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	mustGit(t, fx.root, "add", "src/gone.go")
	mustGit(t, fx.root, "commit", "-m", "add cited source")
	sha := mustGit(t, fx.root, "rev-parse", "HEAD")
	st := &beads.Statement{Kind: "ruling", Text: "src/gone.go::recovered:3", FiledBy: "owner:tester"}
	if err := fx.st.CreateStatement(context.Background(), st); err != nil {
		t.Fatalf("create ruling: %v", err)
	}
	if err := fx.st.SetStatementHeadSHA(context.Background(), st.ID, sha); err != nil {
		t.Fatalf("set head SHA: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove working-tree source: %v", err)
	}

	out, err := runCitationSource(t, st.ID)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if !strings.Contains(out, "source: none recorded") || !strings.Contains(out, "func recovered() {}") {
		t.Fatalf("source did not print transcript status then recovered source:\n%s", out)
	}
}

func TestCite_SourceWithoutShaExitsZero(t *testing.T) {
	fx := newCitationCLIFixture(t, false)
	st := &beads.Statement{Kind: "ruling", Text: "src/gone.go::missing:8", FiledBy: "owner:tester"}
	if err := fx.st.CreateStatement(context.Background(), st); err != nil {
		t.Fatalf("create ruling: %v", err)
	}
	out, err := runCitationSource(t, st.ID)
	if err != nil {
		t.Fatalf("source returned error: %v", err)
	}
	want := "no HEAD recorded for " + st.ID
	if !strings.Contains(out, want) {
		t.Fatalf("source output missing %q:\n%s", want, out)
	}
}
