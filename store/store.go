// Package store is the persistence layer for beads. It wraps sqlc-generated
// per-engine code (SQLite, Postgres) behind one Go API. Static queries are
// type-checked by sqlc at codegen time; the small amount of dynamic SQL
// (filtered list, partial update) is hand-written against database/sql.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/internal/db/pgdb"
	"github.com/rsktash/beads/internal/db/sqlitedb"
	"github.com/rsktash/beads/internal/idgen"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

type Driver string

const (
	DriverSQLite   Driver = "sqlite"
	DriverPostgres Driver = "postgres"
)

var (
	ErrNotFound = errors.New("not found")
	ErrCycle    = errors.New("dependency would create a cycle")
)

//go:embed schema.sqlite.sql
var schemaSQLite string

//go:embed schema.postgres.sql
var schemaPostgres string

// Store is the canonical handle. Open with Open(dsn).
type Store struct {
	db     *sql.DB
	driver Driver
	sqlite *sqlitedb.Queries // non-nil iff driver == DriverSQLite
	pg     *pgdb.Queries     // non-nil iff driver == DriverPostgres
}

// Open accepts:
//   - sqlite path or sqlite:... DSN
//   - postgres://user:pass@host/db?sslmode=disable
func Open(ctx context.Context, dsn string) (*Store, error) {
	driver, conn, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}
	driverName := "sqlite3"
	if driver == DriverPostgres {
		driverName = "postgres"
	}
	db, err := sql.Open(driverName, conn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", driver, err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping %s: %w", driver, err)
	}
	if driver == DriverSQLite {
		if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL;"); err != nil {
			return nil, fmt.Errorf("sqlite pragmas: %w", err)
		}
	}
	s := &Store{db: db, driver: driver}
	switch driver {
	case DriverSQLite:
		s.sqlite = sqlitedb.New(db)
	case DriverPostgres:
		s.pg = pgdb.New(db)
	}
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func parseDSN(dsn string) (Driver, string, error) {
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return DriverPostgres, dsn, nil
	case strings.HasPrefix(dsn, "sqlite://"):
		return DriverSQLite, strings.TrimPrefix(dsn, "sqlite://"), nil
	case strings.HasPrefix(dsn, "sqlite:"):
		return DriverSQLite, strings.TrimPrefix(dsn, "sqlite:"), nil
	case strings.HasSuffix(dsn, ".db"), strings.HasSuffix(dsn, ".sqlite"), strings.HasSuffix(dsn, ".sqlite3"):
		return DriverSQLite, dsn, nil
	}
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		return "", "", fmt.Errorf("unsupported DSN scheme %q", u.Scheme)
	}
	return "", "", fmt.Errorf("cannot determine driver from DSN %q", dsn)
}

func (s *Store) Driver() Driver { return s.driver }
func (s *Store) Close() error   { return s.db.Close() }
func (s *Store) DB() *sql.DB    { return s.db }

func (s *Store) migrate(ctx context.Context) error {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM issues").Scan(&n); err == nil {
		return nil
	}
	body := schemaSQLite
	if s.driver == DriverPostgres {
		body = schemaPostgres
	}
	_, err := s.db.ExecContext(ctx, body)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// CreateIssue inserts a new issue. If i.ID is empty a hash id is generated and
// retried on collision. Always sets created_at/updated_at if zero.
func (s *Store) CreateIssue(ctx context.Context, i *beads.Issue) error {
	if i.ID == "" {
		for attempt := 0; attempt < 8; attempt++ {
			i.ID = idgen.New()
			err := s.insertIssue(ctx, i)
			if err == nil {
				return nil
			}
			if !isUniqueViolation(err) {
				return err
			}
		}
		return fmt.Errorf("idgen: exhausted retries")
	}
	return s.insertIssue(ctx, i)
}

func (s *Store) insertIssue(ctx context.Context, i *beads.Issue) error {
	now := time.Now().UTC()
	if i.CreatedAt.IsZero() {
		i.CreatedAt = now
	}
	i.UpdatedAt = now
	if i.Status == "" {
		i.Status = beads.StatusOpen
	}
	if i.Type == "" {
		i.Type = beads.TypeTask
	}
	if i.Metadata == "" {
		i.Metadata = "{}"
	}
	switch s.driver {
	case DriverSQLite:
		return s.sqlite.CreateIssue(ctx, sqlitedb.CreateIssueParams{
			ID: i.ID, ContentHash: i.ContentHash, Title: i.Title,
			Description: i.Description, Design: i.Design,
			AcceptanceCriteria: i.AcceptanceCriteria, Notes: i.Notes,
			Status: string(i.Status), Priority: int64(i.Priority),
			IssueType: string(i.Type), Assignee: i.Assignee,
			EstimatedMinutes: int64(i.EstimatedMinutes),
			CreatedAt:        i.CreatedAt, CreatedBy: i.CreatedBy, Owner: i.Owner,
			UpdatedAt: i.UpdatedAt, ClosedAt: nullTime(i.ClosedAt),
			ClosedBySession: i.ClosedBySession,
			ExternalRef:     i.ExternalRef, SpecID: i.SpecID, Metadata: i.Metadata,
			SourceRepo: i.SourceRepo, SourceSystem: i.SourceSystem, CloseReason: i.CloseReason,
			Sender:    i.Sender,
			Ephemeral: boolToInt64(i.Ephemeral), Pinned: boolToInt64(i.Pinned),
			IsTemplate: boolToInt64(i.IsTemplate),
			WispType:   i.WispType, MolType: i.MolType, RoleType: i.RoleType,
			EventKind: i.EventKind, Actor: i.Actor, Target: i.Target, Payload: i.Payload,
			StartedAt: nullTime(i.StartedAt), DueAt: nullTime(i.DueAt),
			DeferUntil: nullTime(i.DeferUntil),
		})
	case DriverPostgres:
		return s.pg.CreateIssue(ctx, pgdb.CreateIssueParams{
			ID: i.ID, ContentHash: i.ContentHash, Title: i.Title,
			Description: i.Description, Design: i.Design,
			AcceptanceCriteria: i.AcceptanceCriteria, Notes: i.Notes,
			Status: string(i.Status), Priority: int32(i.Priority),
			IssueType: string(i.Type), Assignee: i.Assignee,
			EstimatedMinutes: int32(i.EstimatedMinutes),
			CreatedAt:        i.CreatedAt, CreatedBy: i.CreatedBy, Owner: i.Owner,
			UpdatedAt: i.UpdatedAt, ClosedAt: nullTime(i.ClosedAt),
			ClosedBySession: i.ClosedBySession,
			ExternalRef:     i.ExternalRef, SpecID: i.SpecID, Metadata: i.Metadata,
			SourceRepo: i.SourceRepo, SourceSystem: i.SourceSystem, CloseReason: i.CloseReason,
			Sender:    i.Sender,
			Ephemeral: boolToInt32(i.Ephemeral), Pinned: boolToInt32(i.Pinned),
			IsTemplate: boolToInt32(i.IsTemplate),
			WispType:   i.WispType, MolType: i.MolType, RoleType: i.RoleType,
			EventKind: i.EventKind, Actor: i.Actor, Target: i.Target, Payload: i.Payload,
			StartedAt: nullTime(i.StartedAt), DueAt: nullTime(i.DueAt),
			DeferUntil: nullTime(i.DeferUntil),
		})
	}
	return fmt.Errorf("unknown driver")
}

func (s *Store) GetIssue(ctx context.Context, id string) (*beads.Issue, error) {
	switch s.driver {
	case DriverSQLite:
		row, err := s.sqlite.GetIssue(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		return fromSqliteIssue(row), nil
	case DriverPostgres:
		row, err := s.pg.GetIssue(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		return fromPgIssue(row), nil
	}
	return nil, fmt.Errorf("unknown driver")
}

type ListFilter struct {
	Status   *beads.Status
	Type     *beads.IssueType
	Priority *int
	Assignee string
	Limit    int
}

// ListIssues is dynamic (variable WHERE clauses) so it's hand-written.
// All static queries above use sqlc-generated code.
func (s *Store) ListIssues(ctx context.Context, f ListFilter) ([]beads.Issue, error) {
	var args []any
	var where []string
	add := func(clause string, v any) { where = append(where, clause); args = append(args, v) }
	if f.Status != nil {
		add("status=?", string(*f.Status))
	}
	if f.Type != nil {
		add("issue_type=?", string(*f.Type))
	}
	if f.Priority != nil {
		add("priority=?", *f.Priority)
	}
	if f.Assignee != "" {
		add("assignee=?", f.Assignee)
	}
	q := "SELECT * FROM issues"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY priority ASC, created_at ASC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	q = s.rebind(q)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []beads.Issue
	for rows.Next() {
		issue, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *issue)
	}
	return out, rows.Err()
}

type IssueUpdate struct {
	Title              *string
	Description        *string
	Design             *string
	AcceptanceCriteria *string
	Notes              *string
	Type               *beads.IssueType
	Status             *beads.Status
	Priority           *int
	Assignee           *string
	Owner              *string
	EstimatedMinutes   *int
	Metadata           *string
	CloseReason        *string
	DueAt              *time.Time
	DeferUntil         *time.Time
	StartedAt          *time.Time
	Ephemeral          *bool
	Pinned             *bool
}

func (s *Store) UpdateIssue(ctx context.Context, id string, u IssueUpdate) (*beads.Issue, error) {
	var sets []string
	var args []any
	add := func(col string, v any) { sets = append(sets, col+"=?"); args = append(args, v) }
	if u.Title != nil {
		add("title", *u.Title)
	}
	if u.Description != nil {
		add("description", *u.Description)
	}
	if u.Design != nil {
		add("design", *u.Design)
	}
	if u.AcceptanceCriteria != nil {
		add("acceptance_criteria", *u.AcceptanceCriteria)
	}
	if u.Notes != nil {
		add("notes", *u.Notes)
	}
	if u.Type != nil {
		add("issue_type", string(*u.Type))
	}
	if u.Status != nil {
		add("status", string(*u.Status))
		if *u.Status == beads.StatusClosed {
			add("closed_at", time.Now().UTC())
		}
		if *u.Status == beads.StatusInProgress {
			add("started_at", time.Now().UTC())
		}
	}
	if u.Priority != nil {
		add("priority", *u.Priority)
	}
	if u.Assignee != nil {
		add("assignee", *u.Assignee)
	}
	if u.Owner != nil {
		add("owner", *u.Owner)
	}
	if u.EstimatedMinutes != nil {
		add("estimated_minutes", *u.EstimatedMinutes)
	}
	if u.Metadata != nil {
		add("metadata", *u.Metadata)
	}
	if u.CloseReason != nil {
		add("close_reason", *u.CloseReason)
	}
	if u.DueAt != nil {
		add("due_at", *u.DueAt)
	}
	if u.DeferUntil != nil {
		add("defer_until", *u.DeferUntil)
	}
	if u.StartedAt != nil {
		add("started_at", *u.StartedAt)
	}
	if u.Ephemeral != nil {
		add("ephemeral", boolToInt64(*u.Ephemeral))
	}
	if u.Pinned != nil {
		add("pinned", boolToInt64(*u.Pinned))
	}
	if len(sets) == 0 {
		return s.GetIssue(ctx, id)
	}
	add("updated_at", time.Now().UTC())
	args = append(args, id)
	q := s.rebind("UPDATE issues SET " + strings.Join(sets, ", ") + " WHERE id=?")
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetIssue(ctx, id)
}

func (s *Store) DeleteIssue(ctx context.Context, id string) error {
	var n int64
	var err error
	switch s.driver {
	case DriverSQLite:
		n, err = s.sqlite.DeleteIssue(ctx, id)
	case DriverPostgres:
		n, err = s.pg.DeleteIssue(ctx, id)
	}
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddDependency creates a directed edge issue -> depends_on. For type=blocks
// it rejects cycles (direct + transitive).
func (s *Store) AddDependency(ctx context.Context, dep beads.Dependency) error {
	if dep.IssueID == dep.DependsOnID {
		return fmt.Errorf("self-dependency not allowed")
	}
	if _, err := s.GetIssue(ctx, dep.IssueID); err != nil {
		return fmt.Errorf("issue %s: %w", dep.IssueID, err)
	}
	if _, err := s.GetIssue(ctx, dep.DependsOnID); err != nil {
		return fmt.Errorf("depends_on %s: %w", dep.DependsOnID, err)
	}
	if dep.Type == beads.DepBlocks {
		// cycle iff issue is already (transitively) blocked by depends_on.
		// i.e. depends_on -*-> issue means issue currently *waits on* depends_on
		// transitively; adding issue->blocks->depends_on closes the loop.
		reach, err := s.forwardBlocks(ctx, dep.DependsOnID)
		if err != nil {
			return err
		}
		if _, hit := reach[dep.IssueID]; hit {
			return ErrCycle
		}
	}
	if dep.CreatedAt.IsZero() {
		dep.CreatedAt = time.Now().UTC()
	}
	if dep.Metadata == "" {
		dep.Metadata = "{}"
	}
	var err error
	switch s.driver {
	case DriverSQLite:
		err = s.sqlite.AddDependency(ctx, sqlitedb.AddDependencyParams{
			IssueID: dep.IssueID, DependsOnID: dep.DependsOnID,
			Type: string(dep.Type), CreatedAt: dep.CreatedAt,
			CreatedBy: dep.CreatedBy, Metadata: dep.Metadata, ThreadID: dep.ThreadID,
		})
	case DriverPostgres:
		err = s.pg.AddDependency(ctx, pgdb.AddDependencyParams{
			IssueID: dep.IssueID, DependsOnID: dep.DependsOnID,
			Type: string(dep.Type), CreatedAt: dep.CreatedAt,
			CreatedBy: dep.CreatedBy, Metadata: dep.Metadata, ThreadID: dep.ThreadID,
		})
	}
	if err != nil && isUniqueViolation(err) {
		return nil
	}
	return err
}

func (s *Store) RemoveDependency(ctx context.Context, issueID, dependsOnID string) error {
	var n int64
	var err error
	switch s.driver {
	case DriverSQLite:
		n, err = s.sqlite.RemoveDependency(ctx, sqlitedb.RemoveDependencyParams{
			IssueID: issueID, DependsOnID: dependsOnID,
		})
	case DriverPostgres:
		n, err = s.pg.RemoveDependency(ctx, pgdb.RemoveDependencyParams{
			IssueID: issueID, DependsOnID: dependsOnID,
		})
	}
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListDependencies(ctx context.Context, id string) ([]beads.Dependency, error) {
	switch s.driver {
	case DriverSQLite:
		rows, err := s.sqlite.ListDependenciesTouching(ctx, id)
		if err != nil {
			return nil, err
		}
		out := make([]beads.Dependency, 0, len(rows))
		for _, r := range rows {
			out = append(out, beads.Dependency{
				IssueID: r.IssueID, DependsOnID: r.DependsOnID,
				Type: beads.DependencyType(r.Type), CreatedAt: r.CreatedAt,
				CreatedBy: r.CreatedBy, Metadata: r.Metadata, ThreadID: r.ThreadID,
			})
		}
		return out, nil
	case DriverPostgres:
		rows, err := s.pg.ListDependenciesTouching(ctx, id)
		if err != nil {
			return nil, err
		}
		out := make([]beads.Dependency, 0, len(rows))
		for _, r := range rows {
			out = append(out, beads.Dependency{
				IssueID: r.IssueID, DependsOnID: r.DependsOnID,
				Type: beads.DependencyType(r.Type), CreatedAt: r.CreatedAt,
				CreatedBy: r.CreatedBy, Metadata: r.Metadata, ThreadID: r.ThreadID,
			})
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown driver")
}

// Ready returns issues that are open, non-ephemeral, non-template, not
// deferred, and have no `blocks` dependency from a non-{closed,pinned} issue.
func (s *Store) Ready(ctx context.Context) ([]beads.Issue, error) {
	now := sql.NullTime{Time: time.Now().UTC(), Valid: true}
	switch s.driver {
	case DriverSQLite:
		rows, err := s.sqlite.ReadyAt(ctx, now)
		if err != nil {
			return nil, err
		}
		out := make([]beads.Issue, 0, len(rows))
		for _, r := range rows {
			out = append(out, *fromSqliteIssue(r))
		}
		return out, nil
	case DriverPostgres:
		rows, err := s.pg.ReadyAt(ctx, now)
		if err != nil {
			return nil, err
		}
		out := make([]beads.Issue, 0, len(rows))
		for _, r := range rows {
			out = append(out, *fromPgIssue(r))
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown driver")
}

// AddLabel attaches a label to an issue. Idempotent.
func (s *Store) AddLabel(ctx context.Context, issueID, label string) error {
	var err error
	switch s.driver {
	case DriverSQLite:
		err = s.sqlite.AddLabel(ctx, sqlitedb.AddLabelParams{IssueID: issueID, Label: label})
	case DriverPostgres:
		err = s.pg.AddLabel(ctx, pgdb.AddLabelParams{IssueID: issueID, Label: label})
	}
	if err != nil && isUniqueViolation(err) {
		return nil
	}
	return err
}

func (s *Store) RemoveLabel(ctx context.Context, issueID, label string) error {
	var n int64
	var err error
	switch s.driver {
	case DriverSQLite:
		n, err = s.sqlite.RemoveLabel(ctx, sqlitedb.RemoveLabelParams{IssueID: issueID, Label: label})
	case DriverPostgres:
		n, err = s.pg.RemoveLabel(ctx, pgdb.RemoveLabelParams{IssueID: issueID, Label: label})
	}
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListLabels(ctx context.Context, issueID string) ([]string, error) {
	switch s.driver {
	case DriverSQLite:
		return s.sqlite.ListLabels(ctx, issueID)
	case DriverPostgres:
		return s.pg.ListLabels(ctx, issueID)
	}
	return nil, fmt.Errorf("unknown driver")
}

func (s *Store) AddComment(ctx context.Context, c *beads.Comment) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	switch s.driver {
	case DriverSQLite:
		return s.sqlite.AddComment(ctx, sqlitedb.AddCommentParams{
			ID: c.ID, IssueID: c.IssueID, Author: c.Author, Text: c.Text, CreatedAt: c.CreatedAt,
		})
	case DriverPostgres:
		return s.pg.AddComment(ctx, pgdb.AddCommentParams{
			ID: c.ID, IssueID: c.IssueID, Author: c.Author, Text: c.Text, CreatedAt: c.CreatedAt,
		})
	}
	return fmt.Errorf("unknown driver")
}

func (s *Store) ListComments(ctx context.Context, issueID string) ([]beads.Comment, error) {
	switch s.driver {
	case DriverSQLite:
		rows, err := s.sqlite.ListComments(ctx, issueID)
		if err != nil {
			return nil, err
		}
		out := make([]beads.Comment, 0, len(rows))
		for _, r := range rows {
			out = append(out, beads.Comment{ID: r.ID, IssueID: r.IssueID, Author: r.Author, Text: r.Text, CreatedAt: r.CreatedAt})
		}
		return out, nil
	case DriverPostgres:
		rows, err := s.pg.ListComments(ctx, issueID)
		if err != nil {
			return nil, err
		}
		out := make([]beads.Comment, 0, len(rows))
		for _, r := range rows {
			out = append(out, beads.Comment{ID: r.ID, IssueID: r.IssueID, Author: r.Author, Text: r.Text, CreatedAt: r.CreatedAt})
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown driver")
}

func (s *Store) AddEvent(ctx context.Context, e *beads.Event) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	switch s.driver {
	case DriverSQLite:
		return s.sqlite.AddEvent(ctx, sqlitedb.AddEventParams{
			ID: e.ID, IssueID: e.IssueID, EventType: e.EventType, Actor: e.Actor,
			OldValue: e.OldValue, NewValue: e.NewValue, Comment: e.Comment, CreatedAt: e.CreatedAt,
		})
	case DriverPostgres:
		return s.pg.AddEvent(ctx, pgdb.AddEventParams{
			ID: e.ID, IssueID: e.IssueID, EventType: e.EventType, Actor: e.Actor,
			OldValue: e.OldValue, NewValue: e.NewValue, Comment: e.Comment, CreatedAt: e.CreatedAt,
		})
	}
	return fmt.Errorf("unknown driver")
}

func (s *Store) ListEvents(ctx context.Context, issueID string) ([]beads.Event, error) {
	switch s.driver {
	case DriverSQLite:
		rows, err := s.sqlite.ListEvents(ctx, issueID)
		if err != nil {
			return nil, err
		}
		out := make([]beads.Event, 0, len(rows))
		for _, r := range rows {
			out = append(out, beads.Event{
				ID: r.ID, IssueID: r.IssueID, EventType: r.EventType, Actor: r.Actor,
				OldValue: r.OldValue, NewValue: r.NewValue, Comment: r.Comment, CreatedAt: r.CreatedAt,
			})
		}
		return out, nil
	case DriverPostgres:
		rows, err := s.pg.ListEvents(ctx, issueID)
		if err != nil {
			return nil, err
		}
		out := make([]beads.Event, 0, len(rows))
		for _, r := range rows {
			out = append(out, beads.Event{
				ID: r.ID, IssueID: r.IssueID, EventType: r.EventType, Actor: r.Actor,
				OldValue: r.OldValue, NewValue: r.NewValue, Comment: r.Comment, CreatedAt: r.CreatedAt,
			})
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown driver")
}

// ChildCount returns how many parent-child dependencies declare this issue as
// the parent (depends_on). Used by the id generator for hierarchical ids.
func (s *Store) ChildCount(ctx context.Context, parent string) (int, error) {
	switch s.driver {
	case DriverSQLite:
		n, err := s.sqlite.ChildCount(ctx, parent)
		return int(n), err
	case DriverPostgres:
		n, err := s.pg.ChildCount(ctx, parent)
		return int(n), err
	}
	return 0, fmt.Errorf("unknown driver")
}

func (s *Store) forwardBlocks(ctx context.Context, id string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		var nexts []string
		var err error
		switch s.driver {
		case DriverSQLite:
			nexts, err = s.sqlite.BlocksReachableFrom(ctx, sqlitedb.BlocksReachableFromParams{
				IssueID: cur, Type: string(beads.DepBlocks),
			})
		case DriverPostgres:
			nexts, err = s.pg.BlocksReachableFrom(ctx, pgdb.BlocksReachableFromParams{
				IssueID: cur, Type: string(beads.DepBlocks),
			})
		}
		if err != nil {
			return nil, err
		}
		for _, n := range nexts {
			if _, seen := out[n]; seen {
				continue
			}
			out[n] = struct{}{}
			queue = append(queue, n)
		}
	}
	return out, nil
}

// rebind translates `?` placeholders in the dynamic queries to engine flavour.
func (s *Store) rebind(q string) string {
	if s.driver != DriverPostgres {
		return q
	}
	var out strings.Builder
	out.Grow(len(q))
	n := 0
	for _, r := range q {
		if r == '?' {
			n++
			fmt.Fprintf(&out, "$%d", n)
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// idgen helpers
var _ = idgen.New // keep import even if unused in tests

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func boolToInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "SQLSTATE 23505")
}
