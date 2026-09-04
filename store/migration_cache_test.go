package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateCache_RecreatedSqliteFileMigratesAgain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "")

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tracker.sqlite")
	first, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("remove first database: %v", err)
	}

	second, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer second.Close()

	rows, err := second.DB().QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatalf("read recreated schema_migrations: %v", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			t.Fatalf("scan recreated migration version: %v", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate recreated migration versions: %v", err)
	}

	migrations, err := loadMigrations(DriverSQLite)
	if err != nil {
		t.Fatalf("load embedded sqlite migrations: %v", err)
	}
	if len(applied) != len(migrations) {
		t.Fatalf("recreated database has %d applied migrations, want %d", len(applied), len(migrations))
	}
	for _, migration := range migrations {
		if !applied[migration.version] {
			t.Errorf("recreated database is missing migration %d", migration.version)
		}
	}
}

func TestMigrateCache_SameFileTakesFastPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "")

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tracker.sqlite")
	first, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	cachePath, err := first.migrationCachePath()
	if err != nil {
		t.Fatalf("migration cache path: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	oldTime := time.Unix(123, 0)
	if err := os.Chtimes(cachePath, oldTime, oldTime); err != nil {
		t.Fatalf("set cache modification time: %v", err)
	}
	before, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat cache before second open: %v", err)
	}

	second, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second store: %v", err)
	}
	after, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat cache after second open: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("cache modification time changed on fast path: before %v, after %v", before.ModTime(), after.ModTime())
	}
}

func TestMigrateCache_PostgresKeyUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "")

	dsn := "postgres://tracker@example.test/beads?sslmode=disable"
	s := &Store{driver: DriverPostgres, dsn: dsn}
	cachePath, err := s.migrationCachePath()
	if err != nil {
		t.Fatalf("migration cache path: %v", err)
	}
	wantHash := sha256.Sum256([]byte(dsn))
	want := hex.EncodeToString(wantHash[:])
	if got := filepath.Base(cachePath); got != want {
		t.Fatalf("postgres cache key = %q, want %q", got, want)
	}
}
