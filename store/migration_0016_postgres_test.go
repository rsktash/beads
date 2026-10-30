package store_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestMigration0016_PostgresFindsCheckByDefinition: 0008 left the mode CHECK
// unnamed, so 0016 must find it by definition in bd's own schema, drop it by
// its real name, and fail loudly when there is none. The Postgres harness
// needs a live server (BD_TEST_POSTGRES_DSN), so this reads the file.
func TestMigration0016_PostgresFindsCheckByDefinition(t *testing.T) {
	b, err := os.ReadFile("migrations/0016_lane_mode_opencode.postgres.sql")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(b)
	if regexp.MustCompile(`(?i)DROP\s+CONSTRAINT\s+(IF\s+EXISTS\s+)?"?plan_lane`).MatchString(body) {
		t.Fatalf("0016 must not drop the CHECK by an assumed name:\n%s", body)
	}
	for _, want := range []string{
		"pg_constraint", "pg_get_constraintdef", "contype = 'c'",
		"current_schema()", "DROP CONSTRAINT %I", "RAISE EXCEPTION",
		"ADD CONSTRAINT plan_lane_mode_check",
		"'inline','subagent','codex','opencode'",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("0016 postgres must contain %q:\n%s", want, body)
		}
	}
	if strings.Contains(strings.ToLower(body), "public") {
		t.Fatalf("0016 must respect bd's search_path, not name a schema:\n%s", body)
	}
}
