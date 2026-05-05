// Package storage is the persistence layer for beads. It supports SQLite and
// Postgres behind a common API. The schema is intentionally small: a single
// `issues` table plus a `dependencies` edge table.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/rustamsmax/beads/internal/idgen"
	"github.com/rustamsmax/beads/internal/types"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

type Driver string

const (
	DriverSQLite   Driver = "sqlite"
	DriverPostgres Driver = "postgres"
)

var ErrNotFound = errors.New("not found")
var ErrCycle = errors.New("dependency would create a cycle")

// Store is the canonical handle. Open with Open(dsn).
type Store struct {
	db     *sqlx.DB
	driver Driver
}

// Open accepts:
//   - sqlite:///absolute/path.db   (also: sqlite:relative.db, or just a path ending in .db/.sqlite)
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
	db, err := sqlx.ConnectContext(ctx, driverName, conn)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", driver, err)
	}
	if driver == DriverSQLite {
		if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL;"); err != nil {
			return nil, fmt.Errorf("sqlite pragmas: %w", err)
		}
	}
	s := &Store{db: db, driver: driver}
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

// migrate creates the schema if it doesn't exist. Idempotent.
func (s *Store) migrate(ctx context.Context) error {
	stmts := schemaSQLite
	if s.driver == DriverPostgres {
		stmts = schemaPostgres
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

var schemaSQLite = []string{
	`CREATE TABLE IF NOT EXISTS issues (
		id          TEXT PRIMARY KEY,
		title       TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		type        TEXT NOT NULL,
		status      TEXT NOT NULL,
		priority    INTEGER NOT NULL DEFAULT 2,
		assignee    TEXT NOT NULL DEFAULT '',
		labels      TEXT NOT NULL DEFAULT '',
		parent_id   TEXT NOT NULL DEFAULT '',
		created_at  TIMESTAMP NOT NULL,
		updated_at  TIMESTAMP NOT NULL,
		closed_at   TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS issues_status_idx   ON issues(status)`,
	`CREATE INDEX IF NOT EXISTS issues_priority_idx ON issues(priority)`,
	`CREATE TABLE IF NOT EXISTS dependencies (
		from_id    TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
		to_id      TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
		type       TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		PRIMARY KEY (from_id, to_id, type)
	)`,
	`CREATE INDEX IF NOT EXISTS deps_to_idx   ON dependencies(to_id)`,
	`CREATE INDEX IF NOT EXISTS deps_from_idx ON dependencies(from_id)`,
}

var schemaPostgres = []string{
	`CREATE TABLE IF NOT EXISTS issues (
		id          TEXT PRIMARY KEY,
		title       TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		type        TEXT NOT NULL,
		status      TEXT NOT NULL,
		priority    INTEGER NOT NULL DEFAULT 2,
		assignee    TEXT NOT NULL DEFAULT '',
		labels      TEXT NOT NULL DEFAULT '',
		parent_id   TEXT NOT NULL DEFAULT '',
		created_at  TIMESTAMPTZ NOT NULL,
		updated_at  TIMESTAMPTZ NOT NULL,
		closed_at   TIMESTAMPTZ
	)`,
	`CREATE INDEX IF NOT EXISTS issues_status_idx   ON issues(status)`,
	`CREATE INDEX IF NOT EXISTS issues_priority_idx ON issues(priority)`,
	`CREATE TABLE IF NOT EXISTS dependencies (
		from_id    TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
		to_id      TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
		type       TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL,
		PRIMARY KEY (from_id, to_id, type)
	)`,
	`CREATE INDEX IF NOT EXISTS deps_to_idx   ON dependencies(to_id)`,
	`CREATE INDEX IF NOT EXISTS deps_from_idx ON dependencies(from_id)`,
}

func (s *Store) rebind(q string) string { return s.db.Rebind(q) }

// CreateIssue inserts a new issue, generating a unique hash-id if i.ID is empty.
func (s *Store) CreateIssue(ctx context.Context, i *types.Issue) error {
	if i.ID == "" {
		// Retry on the off-chance of a 16-bit collision.
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

func (s *Store) insertIssue(ctx context.Context, i *types.Issue) error {
	now := time.Now().UTC()
	if i.CreatedAt.IsZero() {
		i.CreatedAt = now
	}
	i.UpdatedAt = now
	labels, _ := i.Labels.Value()
	q := s.rebind(`INSERT INTO issues
		(id, title, description, type, status, priority, assignee, labels, parent_id, created_at, updated_at, closed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`)
	_, err := s.db.ExecContext(ctx, q,
		i.ID, i.Title, i.Description, i.Type, i.Status, i.Priority,
		i.Assignee, labels, i.ParentID, i.CreatedAt, i.UpdatedAt, i.ClosedAt)
	return err
}

func (s *Store) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	q := s.rebind(`SELECT id,title,description,type,status,priority,assignee,labels,parent_id,created_at,updated_at,closed_at FROM issues WHERE id=?`)
	var out types.Issue
	if err := s.db.GetContext(ctx, &out, q, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

type ListFilter struct {
	Status   *types.Status
	Type     *types.IssueType
	Priority *int
	Assignee string
	Limit    int
}

func (s *Store) ListIssues(ctx context.Context, f ListFilter) ([]types.Issue, error) {
	var (
		args  []any
		where []string
	)
	if f.Status != nil {
		where = append(where, "status=?")
		args = append(args, *f.Status)
	}
	if f.Type != nil {
		where = append(where, "type=?")
		args = append(args, *f.Type)
	}
	if f.Priority != nil {
		where = append(where, "priority=?")
		args = append(args, *f.Priority)
	}
	if f.Assignee != "" {
		where = append(where, "assignee=?")
		args = append(args, f.Assignee)
	}
	q := `SELECT id,title,description,type,status,priority,assignee,labels,parent_id,created_at,updated_at,closed_at FROM issues`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY priority ASC, created_at ASC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	q = s.rebind(q)
	var out []types.Issue
	if err := s.db.SelectContext(ctx, &out, q, args...); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateIssue applies a partial update. Only non-nil fields are written.
type IssueUpdate struct {
	Title       *string
	Description *string
	Type        *types.IssueType
	Status      *types.Status
	Priority    *int
	Assignee    *string
	Labels      *types.Labels
}

func (s *Store) UpdateIssue(ctx context.Context, id string, u IssueUpdate) (*types.Issue, error) {
	var sets []string
	var args []any
	if u.Title != nil {
		sets = append(sets, "title=?")
		args = append(args, *u.Title)
	}
	if u.Description != nil {
		sets = append(sets, "description=?")
		args = append(args, *u.Description)
	}
	if u.Type != nil {
		sets = append(sets, "type=?")
		args = append(args, *u.Type)
	}
	if u.Status != nil {
		sets = append(sets, "status=?")
		args = append(args, *u.Status)
		if *u.Status == types.StatusClosed {
			sets = append(sets, "closed_at=?")
			args = append(args, time.Now().UTC())
		}
	}
	if u.Priority != nil {
		sets = append(sets, "priority=?")
		args = append(args, *u.Priority)
	}
	if u.Assignee != nil {
		sets = append(sets, "assignee=?")
		args = append(args, *u.Assignee)
	}
	if u.Labels != nil {
		v, _ := u.Labels.Value()
		sets = append(sets, "labels=?")
		args = append(args, v)
	}
	if len(sets) == 0 {
		return s.GetIssue(ctx, id)
	}
	sets = append(sets, "updated_at=?")
	args = append(args, time.Now().UTC())
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
	q := s.rebind("DELETE FROM issues WHERE id=?")
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddDependency creates a directed edge from->to of the given type.
// For type=blocks it also rejects cycles.
func (s *Store) AddDependency(ctx context.Context, from, to string, dt types.DependencyType) error {
	if from == to {
		return fmt.Errorf("self-dependency not allowed")
	}
	if _, err := s.GetIssue(ctx, from); err != nil {
		return fmt.Errorf("from %s: %w", from, err)
	}
	if _, err := s.GetIssue(ctx, to); err != nil {
		return fmt.Errorf("to %s: %w", to, err)
	}
	if dt == types.DepBlocks {
		// Adding from->blocks->to is a cycle iff `from` is already reachable
		// from `to` through blocks edges (then to ->* from + the new edge
		// from -> to closes the loop).
		reach, err := s.forwardBlocks(ctx, to)
		if err != nil {
			return err
		}
		if _, hit := reach[from]; hit {
			return ErrCycle
		}
	}
	q := s.rebind(`INSERT INTO dependencies (from_id,to_id,type,created_at) VALUES (?,?,?,?)`)
	_, err := s.db.ExecContext(ctx, q, from, to, dt, time.Now().UTC())
	if err != nil && isUniqueViolation(err) {
		return nil // idempotent
	}
	return err
}

func (s *Store) RemoveDependency(ctx context.Context, from, to string, dt types.DependencyType) error {
	q := s.rebind(`DELETE FROM dependencies WHERE from_id=? AND to_id=? AND type=?`)
	res, err := s.db.ExecContext(ctx, q, from, to, dt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListDependencies(ctx context.Context, id string) (out []types.Dependency, err error) {
	q := s.rebind(`SELECT from_id,to_id,type,created_at FROM dependencies WHERE from_id=? OR to_id=? ORDER BY created_at`)
	err = s.db.SelectContext(ctx, &out, q, id, id)
	return
}

// Ready returns all open issues that have no open `blocks` dependency pointing
// at them. (i.e. nothing blocks them from being worked on right now.)
func (s *Store) Ready(ctx context.Context) ([]types.Issue, error) {
	q := s.rebind(`
		SELECT i.id, i.title, i.description, i.type, i.status, i.priority, i.assignee, i.labels, i.parent_id, i.created_at, i.updated_at, i.closed_at
		FROM issues i
		WHERE i.status IN (?, ?)
		  AND NOT EXISTS (
			SELECT 1 FROM dependencies d
			JOIN issues b ON b.id = d.from_id
			WHERE d.to_id = i.id
			  AND d.type = ?
			  AND b.status != ?
		  )
		ORDER BY i.priority ASC, i.created_at ASC`)
	var out []types.Issue
	err := s.db.SelectContext(ctx, &out, q,
		types.StatusOpen, types.StatusInProgress,
		types.DepBlocks, types.StatusClosed)
	return out, err
}

// forwardBlocks returns the set of issues reachable from `id` by following
// outgoing `blocks` edges. Used for cycle detection: if X ∈ forwardBlocks(to),
// then adding X -> to via blocks closes a loop to -> ... -> X -> to.
func (s *Store) forwardBlocks(ctx context.Context, id string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		q := s.rebind(`SELECT to_id FROM dependencies WHERE from_id=? AND type=?`)
		var nexts []string
		if err := s.db.SelectContext(ctx, &nexts, q, cur, types.DepBlocks); err != nil {
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

// ChildCount returns how many issues already declare parent_id == parent.
// Used by the id generator for hierarchical ids.
func (s *Store) ChildCount(ctx context.Context, parent string) (int, error) {
	q := s.rebind(`SELECT COUNT(*) FROM issues WHERE parent_id=?`)
	var n int
	err := s.db.GetContext(ctx, &n, q, parent)
	return n, err
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// SQLite: "UNIQUE constraint failed". Postgres: "duplicate key value violates unique constraint".
	return strings.Contains(msg, "UNIQUE constraint") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "SQLSTATE 23505")
}
