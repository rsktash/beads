package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	withBodyID       = "bd-with-body"
	emptyBodyID      = "bd-empty-body"
	emptyDesignID    = "bd-empty-design"
	headedDesignID   = "bd-headed-design"
	withBodyDesign   = "The migrated design text."
	emptyBodyDesign  = "The empty-body design text."
	headedDesignText = "Opening design.\n\n## Nested concern\n\nStill part of the original design."
	migratedHeading  = "## Des" + "ign (migrated)"
)

func TestDropDesign_AppendsUnderHeading(t *testing.T) {
	dsn := migratedDesignFixture(t)

	if got := issueDescription(t, dsn, withBodyID); got != "Existing body\n\n"+migratedHeading+"\n\n"+withBodyDesign {
		t.Fatalf("description with an existing body = %q", got)
	}
	if got := issueDescription(t, dsn, emptyBodyID); got != migratedHeading+"\n\n"+emptyBodyDesign {
		t.Fatalf("description with an empty body = %q", got)
	}
}

func TestDropDesign_EmptyDescriptionNoLeadingBlanks(t *testing.T) {
	dsn := migratedDesignFixture(t)
	want := migratedHeading + "\n\n" + emptyBodyDesign
	if got := issueDescription(t, dsn, emptyBodyID); got != want {
		t.Fatalf("empty-body description = %q, want %q", got, want)
	}
}

func TestDropDesign_EmptyDesignUntouched(t *testing.T) {
	dsn := migratedDesignFixture(t)
	if got := issueDescription(t, dsn, emptyDesignID); got != "Untouched body" {
		t.Fatalf("empty-design description = %q, want %q", got, "Untouched body")
	}
}

func TestDropDesign_ColumnGone(t *testing.T) {
	dsn := migratedDesignFixture(t)
	db := openRawSQLite(t, dsn)
	defer db.Close()

	var design string
	err := db.QueryRow(`SELECT design FROM issues LIMIT 1`).Scan(&design)
	if err == nil || !strings.Contains(err.Error(), "no such column: design") {
		t.Fatalf("SELECT design error = %v, want missing-column error", err)
	}
}

func TestDropDesign_SectionSlugReachable(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bd")
	build := exec.Command("go", "build", "-o", bin, "../cmd/bd")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build bd: %v\n%s", err, out)
	}
	dsn := migratedDesignFixture(t)
	cmd := exec.Command(bin, "--db", dsn, "--json", "show", withBodyID, "--section", "design-migrated")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bd show --section design-migrated: %v\n%s", err, out)
	}
	var rows []struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("decode bd show output: %v\n%s", err, out)
	}
	if len(rows) != 1 || strings.TrimSpace(rows[0].Description) != withBodyDesign {
		t.Fatalf("section output = %+v, want %q", rows, withBodyDesign)
	}
}

func TestDropDesign_BodyTailEqualsDesign(t *testing.T) {
	dsn := migratedDesignFixture(t)
	body := issueDescription(t, dsn, headedDesignID)
	prefix, tail, ok := strings.Cut(body, "\n\n"+migratedHeading+"\n\n")
	if !ok {
		t.Fatalf("migrated heading missing from %q", body)
	}
	if prefix != "Existing headed body" {
		t.Fatalf("description prefix = %q, want %q", prefix, "Existing headed body")
	}
	if tail != headedDesignText {
		t.Fatalf("description tail = %q, want original design %q", tail, headedDesignText)
	}
}

func migratedDesignFixture(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "")

	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "design.sqlite")
	db := openRawSQLite(t, dsn)
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL
	)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	migrations, err := loadMigrations(DriverSQLite)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	for _, migration := range migrations {
		if migration.version >= 12 {
			continue
		}
		if _, err := db.ExecContext(ctx, migration.body); err != nil {
			t.Fatalf("apply migration %d: %v", migration.version, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			migration.version, time.Now().UTC()); err != nil {
			t.Fatalf("record migration %d: %v", migration.version, err)
		}
	}
	seed := func(id, description, design string) {
		t.Helper()
		now := time.Now().UTC()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO issues (id, title, description, design, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`, id, id, description, design, now, now); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed(withBodyID, "Existing body", withBodyDesign)
	seed(emptyBodyID, "", emptyBodyDesign)
	seed(emptyDesignID, "Untouched body", "")
	seed(headedDesignID, "Existing headed body", headedDesignText)
	if err := db.Close(); err != nil {
		t.Fatalf("close pre-migration database: %v", err)
	}

	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open database for migration 12: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close migrated database: %v", err)
	}
	return dsn
}

func issueDescription(t *testing.T, dsn, id string) string {
	t.Helper()
	db := openRawSQLite(t, dsn)
	defer db.Close()
	var description string
	if err := db.QueryRow(`SELECT description FROM issues WHERE id = ?`, id).Scan(&description); err != nil {
		t.Fatalf("read description for %s: %v", id, err)
	}
	return description
}

func openRawSQLite(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatalf("ping raw sqlite: %v", err)
	}
	return db
}
