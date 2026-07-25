package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStripPassword(t *testing.T) {
	cases := []struct {
		in, want string
		had      bool
	}{
		{"postgres://user:pw@host/db", "postgres://user@host/db", true},
		{"postgres://user@host/db", "postgres://user@host/db", false},
		{"postgresql://u:p@h:5432/d?sslmode=disable", "postgresql://u@h:5432/d?sslmode=disable", true},
		{"root:secret@tcp(127.0.0.1:3306)/yuklar", "root@tcp(127.0.0.1:3306)/yuklar", true},
		{"root@tcp(127.0.0.1:3306)/yuklar", "root@tcp(127.0.0.1:3306)/yuklar", false},
		{"sqlite:/tmp/x.db", "sqlite:/tmp/x.db", false},
		{"/tmp/x.db", "/tmp/x.db", false},
	}
	for _, c := range cases {
		got, had := stripPassword(c.in)
		if got != c.want || had != c.had {
			t.Errorf("stripPassword(%q) = (%q, %v); want (%q, %v)", c.in, got, had, c.want, c.had)
		}
	}
}

func TestInjectPassword(t *testing.T) {
	cases := []struct {
		dsn, pw, want string
	}{
		{"postgres://u@h/d", "pw", "postgres://u:pw@h/d"},
		{"postgres://u:already@h/d", "pw", "postgres://u:already@h/d"},
		{"postgresql://u@h:5432/d?x=1", "pw", "postgresql://u:pw@h:5432/d?x=1"},
		{"root@tcp(h:3306)/db", "secret", "root:secret@tcp(h:3306)/db"},
		{"root:already@tcp(h:3306)/db", "secret", "root:already@tcp(h:3306)/db"},
		{"sqlite:/tmp/x.db", "pw", "sqlite:/tmp/x.db"},
		{"postgres://u@h/d", "", "postgres://u@h/d"},
	}
	for _, c := range cases {
		got := injectPassword(c.dsn, c.pw)
		if got != c.want {
			t.Errorf("injectPassword(%q, %q) = %q; want %q", c.dsn, c.pw, got, c.want)
		}
	}
}

// chdir moves into dir for the duration of the test. These tests mutate process-wide
// state (cwd and env) so they must not run in parallel with each other.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// writeEnv writes a .env holding the store password at path.
func writeEnv(t *testing.T, path, password string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(EnvDBPassword+"="+password+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// The regression this whole change exists for: bd found .bd by walking up but looked for
// .env only relative to cwd, so from anywhere that was not the project root it resolved a
// DSN and then could not find its password.
func TestLookupDBPasswordFindsProjectRootEnvFromSubdirectory(t *testing.T) {
	t.Setenv(EnvDBPassword, "")
	root := t.TempDir()
	beadsDir := filepath.Join(root, dirName)
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeEnv(t, filepath.Join(root, envFileName), "from-project-root")

	sub := filepath.Join(root, "packages", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	if got := lookupDBPassword(beadsDir); got != "from-project-root" {
		t.Fatalf("password from <root>/.env: got %q, want %q", got, "from-project-root")
	}
}

func TestLookupDBPasswordPrefersBeadsDirEnvOverProjectRoot(t *testing.T) {
	t.Setenv(EnvDBPassword, "")
	root := t.TempDir()
	beadsDir := filepath.Join(root, dirName)
	writeEnv(t, filepath.Join(beadsDir, envFileName), "from-bd-dir")
	writeEnv(t, filepath.Join(root, envFileName), "from-project-root")

	// From a subdirectory holding no .env of its own, so the cwd-relative candidate — which
	// is checked first and is unchanged by this work — misses and the two project-level
	// locations can be compared. .bd/.env is the more specific one and must win.
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	if got := lookupDBPassword(beadsDir); got != "from-bd-dir" {
		t.Fatalf("got %q, want %q", got, "from-bd-dir")
	}
}

// Pins the pre-existing precedence rather than changing it: a .env in the current directory
// is consulted before either project-level location. Someone relying on that today keeps it.
func TestLookupDBPasswordCwdEnvStillWinsWhereItAlwaysDid(t *testing.T) {
	t.Setenv(EnvDBPassword, "")
	root := t.TempDir()
	beadsDir := filepath.Join(root, dirName)
	writeEnv(t, filepath.Join(beadsDir, envFileName), "from-bd-dir")
	writeEnv(t, filepath.Join(root, envFileName), "from-cwd")
	chdir(t, root)

	if got := lookupDBPassword(beadsDir); got != "from-cwd" {
		t.Fatalf("got %q, want %q", got, "from-cwd")
	}
}

func TestLookupDBPasswordEnvVarWinsOverFiles(t *testing.T) {
	root := t.TempDir()
	beadsDir := filepath.Join(root, dirName)
	writeEnv(t, filepath.Join(beadsDir, envFileName), "from-file")
	t.Setenv(EnvDBPassword, "from-env")
	chdir(t, root)

	if got := lookupDBPassword(beadsDir); got != "from-env" {
		t.Fatalf("got %q, want %q", got, "from-env")
	}
}

// An ordinary checkout must keep resolving by the upward walk exactly as before.
func TestFindBeadsDirWalksUpInAnOrdinaryCheckout(t *testing.T) {
	t.Setenv(EnvDir, "")
	// macOS reports TempDir under /var but resolves cwd to /private/var, so compare
	// symlink-resolved paths on both sides or this passes nowhere but Linux.
	root := mustEvalSymlinks(t, t.TempDir())
	beadsDir := filepath.Join(root, dirName)
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	got, found, err := findBeadsDir()
	if err != nil || !found {
		t.Fatalf("findBeadsDir: got=%q found=%v err=%v", got, found, err)
	}
	if got != beadsDir {
		t.Fatalf("got %q, want %q", got, beadsDir)
	}
}

// The case the upward walk cannot reach: a linked worktree created OUTSIDE the repository.
// Its .git is a file pointing at <main>/.git/worktrees/<name>, and .bd is not on its parent
// chain at any depth.
func TestFindBeadsDirResolvesMainWorktreeForDetachedWorktree(t *testing.T) {
	t.Setenv(EnvDir, "")
	tmp := mustEvalSymlinks(t, t.TempDir())
	mainRoot := filepath.Join(tmp, "project")
	beadsDir := filepath.Join(mainRoot, dirName)
	gitDir := filepath.Join(mainRoot, gitName)
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "worktrees", "feature"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Deliberately a sibling of the project, not a child — this is what `git worktree add`
	// to a global location produces, and what the old walk could never resolve.
	wt := filepath.Join(tmp, "elsewhere", "feature")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	linked := "gitdir: " + filepath.Join(gitDir, "worktrees", "feature") + "\n"
	if err := os.WriteFile(filepath.Join(wt, gitName), []byte(linked), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, wt)

	got, found, err := findBeadsDir()
	if err != nil || !found {
		t.Fatalf("findBeadsDir from detached worktree: got=%q found=%v err=%v", got, found, err)
	}
	if got != beadsDir {
		t.Fatalf("got %q, want %q", got, beadsDir)
	}
}

func TestGitMainWorktreeRootReturnsEmptyOutsideAnyRepo(t *testing.T) {
	dir := t.TempDir()
	if got := gitMainWorktreeRoot(dir); got != "" {
		t.Fatalf("expected no repo root outside a repository, got %q", got)
	}
}

func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("evalsymlinks %s: %v", path, err)
	}
	return resolved
}

// A .bd with no config — a scratch dir, tooling artefact, half-finished init — must not
// shadow the real project above it. Before this, bd resolved the bare one, read no DSN,
// fell through to the sqlite default and silently created an empty bd.db there; every
// later command then ran against the wrong database while looking like it worked.
func TestFindBeadsDirSkipsABareBeadsDirShadowingTheRealProject(t *testing.T) {
	t.Setenv(EnvDir, "")
	root := mustEvalSymlinks(t, t.TempDir())
	realBeads := filepath.Join(root, dirName)
	if err := os.MkdirAll(realBeads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realBeads, configName), []byte("db=postgres://u@h/db\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A nested worktree-ish directory that picked up a .bd/.scratch but no config.
	nested := filepath.Join(root, ".worktrees", "feature")
	if err := os.MkdirAll(filepath.Join(nested, dirName, ".scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, nested)

	got, found, err := findBeadsDir()
	if err != nil || !found {
		t.Fatalf("findBeadsDir: got=%q found=%v err=%v", got, found, err)
	}
	if got != realBeads {
		t.Fatalf("bare .bd shadowed the real project: got %q, want %q", got, realBeads)
	}
}

// But a bare .bd is still what `bd init` in a fresh directory relies on.
func TestFindBeadsDirStillHonoursABareBeadsDirWhenNothingElseExists(t *testing.T) {
	t.Setenv(EnvDir, "")
	root := mustEvalSymlinks(t, t.TempDir())
	bare := filepath.Join(root, dirName)
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	got, found, err := findBeadsDir()
	if err != nil || !found {
		t.Fatalf("findBeadsDir: got=%q found=%v err=%v", got, found, err)
	}
	if got != bare {
		t.Fatalf("got %q, want %q", got, bare)
	}
}
