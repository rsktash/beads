package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/internal/config"
	"github.com/rsktash/beads/store"
)

// EnvMigrateSourcePassword names the env var (and .env key) for the
// migration source's password. Stays as BEADS_DOLT_PASSWORD per user request.
const EnvMigrateSourcePassword = "BEADS_DOLT_PASSWORD"

// migrateSourcePassword resolves the migration-source password from
// $BEADS_DOLT_PASSWORD, then .env files (cwd then .bd/.env).
func migrateSourcePassword() string {
	if v := os.Getenv(EnvMigrateSourcePassword); v != "" {
		return v
	}
	for _, p := range []string{".env", ".bd/.env"} {
		if v := config.ReadEnvKey(p, EnvMigrateSourcePassword); v != "" {
			return v
		}
	}
	return ""
}

// ensureParseTime appends parseTime=true so the driver returns DATETIME
// columns as time.Time (otherwise they come back as raw bytes).
func ensureParseTime(dsn string) string {
	if strings.Contains(dsn, "parseTime=") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "parseTime=true"
}

// injectMysqlPassword adds password into a `user@tcp(host)/db` MySQL DSN if
// the DSN doesn't already include credentials.
func injectMysqlPassword(dsn, pw string) string {
	if pw == "" {
		return dsn
	}
	at := strings.Index(dsn, "@")
	if at <= 0 {
		return dsn
	}
	cred := dsn[:at]
	if strings.Contains(cred, ":") {
		return dsn
	}
	return cred + ":" + pw + dsn[at:]
}

// newMigrateCmd imports issues, dependencies, labels, and comments from an
// upstream Dolt-backed beads database. Dolt speaks MySQL wire, so:
//
//	bd migrate --from "root@tcp(dolt.yuklar.com:3306)/yuklar"
//
// Behaviour:
//   - reads upstream's `config` table to seed our `issue_prefix` (and id mode)
//   - UNIONs upstream `issues` + `wisps` into our `issues` (wisps rows get
//     ephemeral=1)
//   - drops history (`events`, `wisp_events`, `issue_snapshots`,
//     `compaction_snapshots`) and AI-coordination columns entirely
//   - existing destination rows are skipped; --force replaces them
func newMigrateCmd() *cobra.Command {
	var (
		from  string
		force bool
	)
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Import a beads database from an upstream Dolt server (MySQL wire)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if from == "" {
				return fmt.Errorf("--from is required")
			}
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			from := injectMysqlPassword(from, migrateSourcePassword())
			from = ensureParseTime(from)
			src, err := sql.Open("mysql", from)
			if err != nil {
				return fmt.Errorf("open source: %w", err)
			}
			defer src.Close()
			if err := src.PingContext(cc.ctx); err != nil {
				return fmt.Errorf("ping source: %w", err)
			}

			m := &migrator{src: src, dst: cc.store, force: force}
			return m.Run(cc.ctx)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "MySQL DSN to upstream Dolt sql-server")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing destination rows on conflict")
	return cmd
}

type migrator struct {
	src   *sql.DB
	dst   *store.Store
	force bool
	stats struct {
		issues, deps, labels, comments, skipped int
	}
}

func (m *migrator) Run(ctx context.Context) error {
	if err := m.copyConfig(ctx); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	// Run issues + wisps first so dependency FKs resolve.
	if err := m.copyIssuesFromTable(ctx, "issues", false); err != nil {
		return fmt.Errorf("issues: %w", err)
	}
	if err := m.copyIssuesFromTable(ctx, "wisps", true); err != nil {
		// `wisps` may not exist on older upstreams — tolerate.
		fmt.Printf("note: skipping wisps (%v)\n", err)
	}
	if err := m.copyDependencies(ctx); err != nil {
		return fmt.Errorf("dependencies: %w", err)
	}
	if err := m.copyLabels(ctx); err != nil {
		return fmt.Errorf("labels: %w", err)
	}
	if err := m.copyComments(ctx); err != nil {
		return fmt.Errorf("comments: %w", err)
	}
	fmt.Printf("imported: %d issues, %d deps, %d labels, %d comments (skipped %d)\n",
		m.stats.issues, m.stats.deps, m.stats.labels, m.stats.comments, m.stats.skipped)
	return nil
}

// copyConfig reads upstream's `config` rows for keys we care about (prefix,
// id mode) and writes them to our `config`. Anything we don't recognise is
// ignored.
func (m *migrator) copyConfig(ctx context.Context) error {
	rows, err := m.src.QueryContext(ctx, "SELECT `key`, value FROM config")
	if err != nil {
		return fmt.Errorf("read upstream config: %w", err)
	}
	defer rows.Close()
	wanted := map[string]bool{
		store.CfgIssuePrefix:      true,
		store.CfgIssueIDMode:      true,
		store.CfgStatusCustom:     true,
		store.CfgTypesCustom:      true,
		store.CfgMaxCollisionProb: true,
		store.CfgMinHashLength:    true,
		store.CfgMaxHashLength:    true,
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		if !wanted[k] {
			continue
		}
		// upstream stores prefix sometimes as "yuklar-" — normalise.
		if k == store.CfgIssuePrefix {
			v = strings.TrimSuffix(v, "-")
		}
		if err := m.dst.SetConfig(ctx, k, v); err != nil {
			return fmt.Errorf("set %s=%s: %w", k, v, err)
		}
	}
	return rows.Err()
}

// copyIssuesFromTable does `SELECT *` and reads each row by column name.
// This tolerates upstream schema drift — e.g. older Dolt instances that
// don't have role_type, no_history, await_* etc. — by treating missing
// columns as their default value.
func (m *migrator) copyIssuesFromTable(ctx context.Context, table string, forceEphemeral bool) error {
	rows, err := m.src.QueryContext(ctx, "SELECT * FROM "+table)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		row := make(map[string]any, len(cols))
		for i, name := range cols {
			row[name] = raw[i]
		}

		i := beads.Issue{
			ID:                 asString(row["id"]),
			ContentHash:        asString(row["content_hash"]),
			Title:              asString(row["title"]),
			Description:        asString(row["description"]),
			Design:             asString(row["design"]),
			AcceptanceCriteria: asString(row["acceptance_criteria"]),
			Notes:              asString(row["notes"]),
			Status:             beads.Status(strDefault(row["status"], "open")),
			Priority:           asInt(row["priority"], 2),
			Type:               beads.IssueType(strDefault(row["issue_type"], "task")),
			Assignee:           asString(row["assignee"]),
			EstimatedMinutes:   asInt(row["estimated_minutes"], 0),
			CreatedAt:          asTime(row["created_at"]),
			CreatedBy:          asString(row["created_by"]),
			Owner:              asString(row["owner"]),
			UpdatedAt:          asTime(row["updated_at"]),
			ClosedBySession:    asString(row["closed_by_session"]),
			ExternalRef:        asString(row["external_ref"]),
			SpecID:             asString(row["spec_id"]),
			Metadata:           strDefault(row["metadata"], "{}"),
			SourceRepo:         asString(row["source_repo"]),
			SourceSystem:       asString(row["source_system"]),
			CloseReason:        asString(row["close_reason"]),
			Sender:             asString(row["sender"]),
			Ephemeral:          asInt(row["ephemeral"], 0) != 0 || forceEphemeral,
			Pinned:             asInt(row["pinned"], 0) != 0,
			IsTemplate:         asInt(row["is_template"], 0) != 0,
			WispType:           asString(row["wisp_type"]),
			MolType:            asString(row["mol_type"]),
			RoleType:           asString(row["role_type"]),
			EventKind:          asString(row["event_kind"]),
			Actor:              asString(row["actor"]),
			Target:             asString(row["target"]),
			Payload:            asString(row["payload"]),
			StartedAt:          asTimePtr(row["started_at"]),
			ClosedAt:           asTimePtr(row["closed_at"]),
			DueAt:              asTimePtr(row["due_at"]),
			DeferUntil:         asTimePtr(row["defer_until"]),
		}

		if existing, _ := m.dst.GetIssue(ctx, i.ID); existing != nil {
			if !m.force {
				m.stats.skipped++
				continue
			}
			if err := m.dst.DeleteIssue(ctx, i.ID); err != nil {
				return fmt.Errorf("force-delete %s: %w", i.ID, err)
			}
		}
		if err := m.dst.CreateIssue(ctx, &i); err != nil {
			return fmt.Errorf("issue %s: %w", i.ID, err)
		}
		m.stats.issues++
	}
	return rows.Err()
}

// asString returns the column value as a string. Treats nil + non-strings
// gracefully (mysql JSON columns come back as []byte).
func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}

func strDefault(v any, fallback string) string {
	s := asString(v)
	if s == "" {
		return fallback
	}
	return s
}

func asInt(v any, fallback int) int {
	switch x := v.(type) {
	case nil:
		return fallback
	case int64:
		return int(x)
	case int32:
		return int(x)
	case int:
		return x
	case []byte:
		if n, err := strconv.Atoi(string(x)); err == nil {
			return n
		}
	case string:
		if n, err := strconv.Atoi(x); err == nil {
			return n
		}
	}
	return fallback
}

func asTime(v any) time.Time {
	t := asTimePtr(v)
	if t == nil {
		return time.Time{}
	}
	return *t
}

func asTimePtr(v any) *time.Time {
	switch x := v.(type) {
	case nil:
		return nil
	case time.Time:
		if x.IsZero() {
			return nil
		}
		t := x
		return &t
	case []byte:
		s := string(x)
		if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
			return &t
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return &t
		}
	case string:
		if t, err := time.Parse("2006-01-02 15:04:05", x); err == nil {
			return &t
		}
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return &t
		}
	}
	return nil
}

func (m *migrator) copyDependencies(ctx context.Context) error {
	rows, err := m.src.QueryContext(ctx, `SELECT issue_id, depends_on_id, COALESCE(type,'blocks'),
		created_at, COALESCE(created_by,''), COALESCE(CAST(metadata AS CHAR),'{}'),
		COALESCE(thread_id,'') FROM dependencies`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var d beads.Dependency
		var t string
		if err := rows.Scan(&d.IssueID, &d.DependsOnID, &t,
			&d.CreatedAt, &d.CreatedBy, &d.Metadata, &d.ThreadID); err != nil {
			return err
		}
		// Some upstream rows store depends_on_id as "<type>:<id>" (e.g.
		// "blocks:teejar-ztv"). Strip a single "<word>:" prefix when present.
		d.IssueID = stripTypePrefix(d.IssueID)
		d.DependsOnID = stripTypePrefix(d.DependsOnID)
		d.Type = beads.DependencyType(t)
		if err := m.dst.AddDependency(ctx, d); err != nil {
			if errors.Is(err, store.ErrCycle) {
				m.stats.skipped++
				continue
			}
			// Orphan dep (FK violation) — issue referenced doesn't exist on
			// our side. Don't fail the whole migration; just skip.
			if strings.Contains(err.Error(), "not found") ||
				strings.Contains(err.Error(), "foreign key") ||
				strings.Contains(err.Error(), "violates") {
				fmt.Fprintf(os.Stderr, "skip orphan dep %s -> %s: %v\n", d.IssueID, d.DependsOnID, err)
				m.stats.skipped++
				continue
			}
			return fmt.Errorf("dep %s -> %s: %w", d.IssueID, d.DependsOnID, err)
		}
		m.stats.deps++
	}
	return rows.Err()
}

// stripTypePrefix removes a leading "<word>:" prefix if present, leaving
// just the issue id. Upstream sometimes stores depends_on_id as
// "blocks:bd-XXXX"; our schema keeps the type in its own column.
func stripTypePrefix(s string) string {
	colon := strings.Index(s, ":")
	if colon <= 0 {
		return s
	}
	prefix := s[:colon]
	for _, r := range prefix {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return s
		}
	}
	return s[colon+1:]
}

func (m *migrator) copyLabels(ctx context.Context) error {
	rows, err := m.src.QueryContext(ctx, "SELECT issue_id, label FROM labels")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var issueID, label string
		if err := rows.Scan(&issueID, &label); err != nil {
			return err
		}
		if err := m.dst.AddLabel(ctx, issueID, label); err != nil {
			return fmt.Errorf("label %s/%s: %w", issueID, label, err)
		}
		m.stats.labels++
	}
	return rows.Err()
}

func (m *migrator) copyComments(ctx context.Context) error {
	rows, err := m.src.QueryContext(ctx, `SELECT id, issue_id, author, text, created_at FROM comments`)
	if err != nil {
		return nil // table optional
	}
	defer rows.Close()
	for rows.Next() {
		var c beads.Comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Author, &c.Text, &c.CreatedAt); err != nil {
			return err
		}
		if err := m.dst.AddComment(ctx, &c); err != nil {
			return fmt.Errorf("comment %s: %w", c.ID, err)
		}
		m.stats.comments++
	}
	return rows.Err()
}
