package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// envPassword resolves a password for the MySQL DSN at migrate time, looking
// at (in order): the DSN itself (already contains `:pw@`), $BEADS_DOLT_PASSWORD,
// then a .env file in cwd or ./.bd/.
func envPassword() string {
	if v := os.Getenv("BEADS_DOLT_PASSWORD"); v != "" {
		return v
	}
	for _, p := range []string{".env", filepath.Join(".bd", ".env")} {
		if v := readEnvKey(p, "BEADS_DOLT_PASSWORD"); v != "" {
			return v
		}
	}
	return ""
}

// readEnvKey reads a single KEY from a dotenv-style file. Tolerates `export `,
// optional surrounding quotes, and `# comments`. Returns "" if missing.
func readEnvKey(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	prefix := key + "="
	exportPrefix := "export " + prefix
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, exportPrefix) {
			line = strings.TrimPrefix(line, "export ")
		}
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		v := strings.TrimPrefix(line, prefix)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		return v
	}
	return ""
}

// injectMysqlPassword adds password into a `user@tcp(host)/db` MySQL DSN if
// the DSN doesn't already include credentials. Leaves DSNs alone if they
// already have a colon before the @ (i.e. user:pw@... form).
func injectMysqlPassword(dsn, pw string) string {
	if pw == "" {
		return dsn
	}
	at := strings.Index(dsn, "@")
	if at <= 0 {
		return dsn
	}
	credential := dsn[:at]
	if strings.Contains(credential, ":") {
		return dsn // already has password
	}
	return credential + ":" + pw + dsn[at:]
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

			from := injectMysqlPassword(from, envPassword())
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

// upstreamIssueCols enumerates only the columns we persist. Upstream has
// more (await/hook/agent/compaction/no_history/work_type) and they're
// dropped here per project scope.
const upstreamIssueCols = `id, COALESCE(content_hash,''), title, COALESCE(description,''),
COALESCE(design,''), COALESCE(acceptance_criteria,''), COALESCE(notes,''),
COALESCE(status,'open'), COALESCE(priority,2), COALESCE(issue_type,'task'),
COALESCE(assignee,''), COALESCE(estimated_minutes,0),
created_at, COALESCE(created_by,''), COALESCE(owner,''),
updated_at, started_at, closed_at, COALESCE(closed_by_session,''),
COALESCE(external_ref,''), COALESCE(spec_id,''), COALESCE(CAST(metadata AS CHAR),'{}'),
COALESCE(source_repo,''), COALESCE(source_system,''), COALESCE(close_reason,''),
COALESCE(sender,''), COALESCE(ephemeral,0), COALESCE(pinned,0), COALESCE(is_template,0),
COALESCE(wisp_type,''), COALESCE(mol_type,''), COALESCE(role_type,''),
COALESCE(event_kind,''), COALESCE(actor,''), COALESCE(target,''), COALESCE(payload,''),
due_at, defer_until`

func (m *migrator) copyIssuesFromTable(ctx context.Context, table string, forceEphemeral bool) error {
	q := "SELECT " + upstreamIssueCols + " FROM " + table
	rows, err := m.src.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			i                                              beads.Issue
			statusS, typeS                                 string
			ephemeral, pinned, isTemplate                  int
			startedAt, closedAt, dueAt, deferUntil         sql.NullTime
		)
		if err := rows.Scan(
			&i.ID, &i.ContentHash, &i.Title, &i.Description, &i.Design,
			&i.AcceptanceCriteria, &i.Notes,
			&statusS, &i.Priority, &typeS, &i.Assignee, &i.EstimatedMinutes,
			&i.CreatedAt, &i.CreatedBy, &i.Owner,
			&i.UpdatedAt, &startedAt, &closedAt, &i.ClosedBySession,
			&i.ExternalRef, &i.SpecID, &i.Metadata,
			&i.SourceRepo, &i.SourceSystem, &i.CloseReason,
			&i.Sender, &ephemeral, &pinned, &isTemplate,
			&i.WispType, &i.MolType, &i.RoleType,
			&i.EventKind, &i.Actor, &i.Target, &i.Payload,
			&dueAt, &deferUntil,
		); err != nil {
			return err
		}
		i.Status = beads.Status(statusS)
		i.Type = beads.IssueType(typeS)
		i.Ephemeral = ephemeral != 0 || forceEphemeral
		i.Pinned = pinned != 0
		i.IsTemplate = isTemplate != 0
		if startedAt.Valid {
			t := startedAt.Time
			i.StartedAt = &t
		}
		if closedAt.Valid {
			t := closedAt.Time
			i.ClosedAt = &t
		}
		if dueAt.Valid {
			t := dueAt.Time
			i.DueAt = &t
		}
		if deferUntil.Valid {
			t := deferUntil.Time
			i.DeferUntil = &t
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
		d.Type = beads.DependencyType(t)
		if err := m.dst.AddDependency(ctx, d); err != nil {
			if errors.Is(err, store.ErrCycle) {
				m.stats.skipped++
				continue
			}
			return fmt.Errorf("dep %s -> %s: %w", d.IssueID, d.DependsOnID, err)
		}
		m.stats.deps++
	}
	return rows.Err()
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
