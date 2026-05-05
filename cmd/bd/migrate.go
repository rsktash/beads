package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// newMigrateCmd imports issues, dependencies, labels, comments, and events
// from an upstream Dolt-backed beads repository.
//
// The connection is plain MySQL — Dolt's sql-server speaks the MySQL wire
// protocol — so the workflow is:
//
//	cd /path/to/upstream/.beads/embeddeddolt
//	dolt sql-server -P 3306 -u root --no-auto-commit
//	bd migrate --from "root@tcp(127.0.0.1:3306)/beads"
//
// Existing destination rows are skipped; pass --force to replace them.
func newMigrateCmd() *cobra.Command {
	var (
		from  string
		force bool
	)
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Import a beads database from an upstream Dolt server (MySQL wire)",
		Long: `Connects to a running 'dolt sql-server' (which speaks MySQL) and copies all
issues, dependencies, labels, comments, and events into the active store.

The source database must use the upstream beads schema (gastownhall/beads).

Run 'dolt sql-server' on the upstream .beads/embeddeddolt/ first, then point
this command at it via --from with a MySQL DSN.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if from == "" {
				return fmt.Errorf("--from is required")
			}
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

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
	cmd.Flags().StringVar(&from, "from", "", "MySQL DSN to upstream Dolt sql-server (e.g. root@tcp(127.0.0.1:3306)/beads)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing rows on conflict")
	return cmd
}

type migrator struct {
	src   *sql.DB
	dst   *store.Store
	force bool
	stats struct {
		issues, deps, labels, comments, events, skipped int
	}
}

func (m *migrator) Run(ctx context.Context) error {
	if err := m.copyIssues(ctx); err != nil {
		return fmt.Errorf("issues: %w", err)
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
	if err := m.copyEvents(ctx); err != nil {
		return fmt.Errorf("events: %w", err)
	}
	fmt.Printf("imported: %d issues, %d deps, %d labels, %d comments, %d events (skipped %d)\n",
		m.stats.issues, m.stats.deps, m.stats.labels, m.stats.comments, m.stats.events, m.stats.skipped)
	return nil
}

// upstreamIssueCols enumerates only the columns we actually persist; the
// upstream schema has more (await/hook/agent/compaction) which we drop.
const upstreamIssueCols = `id, COALESCE(content_hash,''), title, COALESCE(description,''),
COALESCE(design,''), COALESCE(acceptance_criteria,''), COALESCE(notes,''),
COALESCE(status,'open'), COALESCE(priority,2), COALESCE(issue_type,'task'),
COALESCE(assignee,''), COALESCE(estimated_minutes,0),
created_at, COALESCE(created_by,''), COALESCE(owner,''),
updated_at, closed_at, COALESCE(closed_by_session,''),
COALESCE(external_ref,''), COALESCE(spec_id,''), COALESCE(CAST(metadata AS CHAR),'{}'),
COALESCE(source_repo,''), COALESCE(source_system,''), COALESCE(close_reason,''),
COALESCE(sender,''), COALESCE(ephemeral,0), COALESCE(pinned,0), COALESCE(is_template,0),
COALESCE(wisp_type,''), COALESCE(mol_type,''), COALESCE(role_type,''),
COALESCE(event_kind,''), COALESCE(actor,''), COALESCE(target,''), COALESCE(payload,''),
started_at, due_at, defer_until`

func (m *migrator) copyIssues(ctx context.Context) error {
	rows, err := m.src.QueryContext(ctx, "SELECT "+upstreamIssueCols+" FROM issues")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			i                                      beads.Issue
			statusS, typeS                         string
			ephemeral, pinned, isTemplate          int
			closedAt, startedAt, dueAt, deferUntil sql.NullTime
		)
		if err := rows.Scan(
			&i.ID, &i.ContentHash, &i.Title, &i.Description, &i.Design,
			&i.AcceptanceCriteria, &i.Notes,
			&statusS, &i.Priority, &typeS, &i.Assignee, &i.EstimatedMinutes,
			&i.CreatedAt, &i.CreatedBy, &i.Owner,
			&i.UpdatedAt, &closedAt, &i.ClosedBySession,
			&i.ExternalRef, &i.SpecID, &i.Metadata,
			&i.SourceRepo, &i.SourceSystem, &i.CloseReason,
			&i.Sender, &ephemeral, &pinned, &isTemplate,
			&i.WispType, &i.MolType, &i.RoleType,
			&i.EventKind, &i.Actor, &i.Target, &i.Payload,
			&startedAt, &dueAt, &deferUntil,
		); err != nil {
			return err
		}
		i.Status = beads.Status(statusS)
		if !i.Status.Valid() {
			i.Status = beads.StatusOpen
		}
		i.Type = beads.IssueType(typeS)
		if !i.Type.Valid() {
			i.Type = beads.TypeTask
		}
		i.Ephemeral = ephemeral != 0
		i.Pinned = pinned != 0
		i.IsTemplate = isTemplate != 0
		if closedAt.Valid {
			t := closedAt.Time
			i.ClosedAt = &t
		}
		if startedAt.Valid {
			t := startedAt.Time
			i.StartedAt = &t
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
		if !d.Type.Valid() {
			d.Type = beads.DepBlocks
		}
		if err := m.dst.AddDependency(ctx, d); err != nil {
			if strings.Contains(err.Error(), "cycle") {
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

func (m *migrator) copyEvents(ctx context.Context) error {
	rows, err := m.src.QueryContext(ctx, `SELECT id, issue_id, event_type, actor,
		COALESCE(old_value,''), COALESCE(new_value,''), COALESCE(comment,''),
		created_at FROM events`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var e beads.Event
		if err := rows.Scan(&e.ID, &e.IssueID, &e.EventType, &e.Actor,
			&e.OldValue, &e.NewValue, &e.Comment, &e.CreatedAt); err != nil {
			return err
		}
		if err := m.dst.AddEvent(ctx, &e); err != nil {
			return fmt.Errorf("event %s: %w", e.ID, err)
		}
		m.stats.events++
	}
	return rows.Err()
}
