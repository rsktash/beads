// Package store is the persistence layer for bd. It wraps sqlc-generated
// per-engine code (SQLite, Postgres) behind one Go API.
//
// Project settings (issue prefix, id mode, custom statuses/types) live in
// the DB `config` table — same DB, every client agrees. Connection state
// (just the DSN) lives in .bd/config on disk; see internal/config.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"regexp"
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

// Config keys recognised in the in-DB `config` table.
const (
	CfgIssuePrefix       = "issue_prefix"
	CfgIssueIDMode       = "issue_id_mode" // "hash" (default) or "counter"
	CfgStatusCustom      = "status.custom" // JSON array of extra status names
	CfgTypesCustom       = "types.custom"  // JSON array of extra issue types
	CfgMaxCollisionProb  = "max_collision_prob"
	CfgMinHashLength     = "min_hash_length"
	CfgMaxHashLength     = "max_hash_length"
	IDModeHash           = "hash"
	IDModeCounter        = "counter"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrCycle         = errors.New("dependency would create a cycle")
	ErrNoPrefix      = errors.New("no issue_prefix configured (run `bd init --prefix <name>`)")
	ErrDepthExceeded = errors.New("hierarchy depth exceeded")
	ErrBadSchemaName = errors.New("invalid schema name (only [a-z0-9_-]+ allowed)")
)

// schemaNameRE constrains postgres search_path values we accept. We must
// interpolate this into DDL (CREATE SCHEMA / SET search_path) — quoting via
// `"name"` would protect against injection, but we additionally enforce a
// strict charset because schemas are also user-visible identifiers.
var schemaNameRE = regexp.MustCompile(`^[a-z0-9_-]+$`)

//go:embed schema.sqlite.sql
var schemaSQLite string

//go:embed schema.postgres.sql
var schemaPostgres string

// Store is the canonical handle.
type Store struct {
	db     *sql.DB
	driver Driver
	sqlite *sqlitedb.Queries
	pg     *pgdb.Queries
}

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
	if driver == DriverPostgres {
		// Multi-project: if the DSN names a search_path schema, ensure it
		// exists and is set for this session. lib/pq honours search_path in
		// the URL on connect, but the schema must be there first.
		if schema, ok := postgresSearchPath(dsn); ok && schema != "" {
			if !schemaNameRE.MatchString(schema) {
				return nil, fmt.Errorf("%w: %q", ErrBadSchemaName, schema)
			}
			if _, err := db.ExecContext(ctx,
				fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %q`, schema)); err != nil {
				return nil, fmt.Errorf("create schema %q: %w", schema, err)
			}
			if _, err := db.ExecContext(ctx,
				fmt.Sprintf(`SET search_path TO %q`, schema)); err != nil {
				return nil, fmt.Errorf("set search_path: %w", err)
			}
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

// PostgresSearchPathForTest exposes the internal helper for unit tests.
func PostgresSearchPathForTest(dsn string) (string, bool) { return postgresSearchPath(dsn) }

// postgresSearchPath returns the value of `search_path` from a postgres URI
// DSN, if any. Returns ("", false) when the DSN has no such param or isn't a
// URI form.
func postgresSearchPath(dsn string) (string, bool) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", false
	}
	q := u.Query()
	if !q.Has("search_path") {
		return "", false
	}
	return q.Get("search_path"), true
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

// ---------- in-DB config ----------

// GetConfig returns the value for a config key, or "" if unset.
func (s *Store) GetConfig(ctx context.Context, key string) (string, error) {
	var v string
	var err error
	switch s.driver {
	case DriverSQLite:
		v, err = s.sqlite.GetConfigValue(ctx, key)
	case DriverPostgres:
		v, err = s.pg.GetConfigValue(ctx, key)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetConfig upserts a config key.
func (s *Store) SetConfig(ctx context.Context, key, value string) error {
	switch s.driver {
	case DriverSQLite:
		return s.sqlite.SetConfigValue(ctx, sqlitedb.SetConfigValueParams{Key: key, Value: value})
	case DriverPostgres:
		return s.pg.SetConfigValue(ctx, pgdb.SetConfigValueParams{Key: key, Value: value})
	}
	return fmt.Errorf("unknown driver")
}

// ListConfig returns all key/value pairs (excludes empty rows).
func (s *Store) ListConfig(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	switch s.driver {
	case DriverSQLite:
		rows, err := s.sqlite.ListConfig(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.Key] = r.Value
		}
	case DriverPostgres:
		rows, err := s.pg.ListConfig(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.Key] = r.Value
		}
	}
	return out, nil
}

// Prefix returns the configured issue_prefix, or ErrNoPrefix if unset.
func (s *Store) Prefix(ctx context.Context) (string, error) {
	v, err := s.GetConfig(ctx, CfgIssuePrefix)
	if err != nil {
		return "", err
	}
	v = strings.TrimSuffix(v, "-")
	if v == "" {
		return "", ErrNoPrefix
	}
	return v, nil
}

// ---------- issues ----------

// CreateIssue inserts a new issue. If i.ID is empty an id is allocated based
// on the configured issue_id_mode (hash or counter).
func (s *Store) CreateIssue(ctx context.Context, i *beads.Issue) error {
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

	if i.ID == "" {
		id, err := s.allocateID(ctx, i)
		if err != nil {
			return err
		}
		i.ID = id
	}
	return s.insertIssue(ctx, i)
}

func (s *Store) allocateID(ctx context.Context, i *beads.Issue) (string, error) {
	prefix, err := s.Prefix(ctx)
	if err != nil {
		return "", err
	}
	mode, _ := s.GetConfig(ctx, CfgIssueIDMode)
	if mode == IDModeCounter {
		var n int64
		switch s.driver {
		case DriverSQLite:
			n, err = s.sqlite.NextCounterID(ctx, prefix)
		case DriverPostgres:
			v, e := s.pg.NextCounterID(ctx, prefix)
			n, err = int64(v), e
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s-%d", prefix, n), nil
	}

	// Hash mode: adaptive length grows with project size.
	count, err := s.countIssuesWithPrefix(ctx, prefix)
	if err != nil {
		return "", err
	}
	cfg := s.adaptiveCfg(ctx)
	baseLen := idgen.AdaptiveLength(count, cfg)
	for length := baseLen; length <= cfg.MaxLength; length++ {
		for nonce := 0; nonce < 10; nonce++ {
			cand := idgen.GenerateHashID(prefix, i.Title, i.Description, i.CreatedBy, i.CreatedAt, length, nonce)
			if existing, _ := s.GetIssue(ctx, cand); existing == nil {
				return cand, nil
			}
		}
	}
	return "", fmt.Errorf("idgen: exhausted lengths %d-%d * 10 nonces", baseLen, cfg.MaxLength)
}

func (s *Store) adaptiveCfg(ctx context.Context) idgen.AdaptiveConfig {
	cfg := idgen.DefaultAdaptive()
	if v, _ := s.GetConfig(ctx, CfgMaxCollisionProb); v != "" {
		var p float64
		_, _ = fmt.Sscanf(v, "%f", &p)
		if p > 0 {
			cfg.MaxCollisionProbability = p
		}
	}
	if v, _ := s.GetConfig(ctx, CfgMinHashLength); v != "" {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		if n > 0 {
			cfg.MinLength = n
		}
	}
	if v, _ := s.GetConfig(ctx, CfgMaxHashLength); v != "" {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		if n > 0 {
			cfg.MaxLength = n
		}
	}
	return cfg
}

func (s *Store) countIssuesWithPrefix(ctx context.Context, prefix string) (int, error) {
	pattern := prefix + "-%"
	switch s.driver {
	case DriverSQLite:
		n, err := s.sqlite.CountIssuesWithPrefix(ctx, pattern)
		return int(n), err
	case DriverPostgres:
		n, err := s.pg.CountIssuesWithPrefix(ctx, pattern)
		return int(n), err
	}
	return 0, fmt.Errorf("unknown driver")
}

// NextChildID atomically allocates a hierarchical id under parent. Caps at
// idgen.MaxHierarchyDepth.
func (s *Store) NextChildID(ctx context.Context, parent string) (string, error) {
	if idgen.HierarchyDepth(parent) >= idgen.MaxHierarchyDepth {
		return "", ErrDepthExceeded
	}
	var n int
	switch s.driver {
	case DriverSQLite:
		v, err := s.sqlite.NextChildIndex(ctx, parent)
		if err != nil {
			return "", err
		}
		n = int(v)
	case DriverPostgres:
		v, err := s.pg.NextChildIndex(ctx, parent)
		if err != nil {
			return "", err
		}
		n = int(v)
	}
	return idgen.ChildID(parent, n), nil
}

func (s *Store) insertIssue(ctx context.Context, i *beads.Issue) error {
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
			UpdatedAt: i.UpdatedAt,
			StartedAt: nullTime(i.StartedAt), ClosedAt: nullTime(i.ClosedAt),
			ClosedBySession: i.ClosedBySession,
			ExternalRef:     i.ExternalRef, SpecID: i.SpecID, Metadata: i.Metadata,
			SourceRepo: i.SourceRepo, SourceSystem: i.SourceSystem, CloseReason: i.CloseReason,
			Sender:    i.Sender,
			Ephemeral: boolToInt64(i.Ephemeral), Pinned: boolToInt64(i.Pinned),
			IsTemplate: boolToInt64(i.IsTemplate),
			WispType:   i.WispType, MolType: i.MolType, RoleType: i.RoleType,
			EventKind: i.EventKind, Actor: i.Actor, Target: i.Target, Payload: i.Payload,
			DueAt:     nullTime(i.DueAt), DeferUntil: nullTime(i.DeferUntil),
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
			UpdatedAt: i.UpdatedAt,
			StartedAt: nullTime(i.StartedAt), ClosedAt: nullTime(i.ClosedAt),
			ClosedBySession: i.ClosedBySession,
			ExternalRef:     i.ExternalRef, SpecID: i.SpecID, Metadata: i.Metadata,
			SourceRepo: i.SourceRepo, SourceSystem: i.SourceSystem, CloseReason: i.CloseReason,
			Sender:    i.Sender,
			Ephemeral: boolToInt32(i.Ephemeral), Pinned: boolToInt32(i.Pinned),
			IsTemplate: boolToInt32(i.IsTemplate),
			WispType:   i.WispType, MolType: i.MolType, RoleType: i.RoleType,
			EventKind: i.EventKind, Actor: i.Actor, Target: i.Target, Payload: i.Payload,
			DueAt:     nullTime(i.DueAt), DeferUntil: nullTime(i.DeferUntil),
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

func (s *Store) ListIssues(ctx context.Context, f ListFilter) ([]beads.Issue, error) {
	var args []any
	var where []string
	add := func(c string, v any) { where = append(where, c); args = append(args, v) }
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

// ---------- dependencies ----------

func (s *Store) AddDependency(ctx context.Context, d beads.Dependency) error {
	if d.IssueID == d.DependsOnID {
		return fmt.Errorf("self-dependency not allowed")
	}
	if _, err := s.GetIssue(ctx, d.IssueID); err != nil {
		return fmt.Errorf("issue %s: %w", d.IssueID, err)
	}
	if _, err := s.GetIssue(ctx, d.DependsOnID); err != nil {
		return fmt.Errorf("depends_on %s: %w", d.DependsOnID, err)
	}
	if d.Type == beads.DepBlocks {
		reach, err := s.forwardBlocks(ctx, d.DependsOnID)
		if err != nil {
			return err
		}
		if _, hit := reach[d.IssueID]; hit {
			return ErrCycle
		}
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	if d.Metadata == "" {
		d.Metadata = "{}"
	}
	var err error
	switch s.driver {
	case DriverSQLite:
		err = s.sqlite.AddDependency(ctx, sqlitedb.AddDependencyParams{
			IssueID: d.IssueID, DependsOnID: d.DependsOnID,
			Type: string(d.Type), CreatedAt: d.CreatedAt,
			CreatedBy: d.CreatedBy, Metadata: d.Metadata, ThreadID: d.ThreadID,
		})
	case DriverPostgres:
		err = s.pg.AddDependency(ctx, pgdb.AddDependencyParams{
			IssueID: d.IssueID, DependsOnID: d.DependsOnID,
			Type: string(d.Type), CreatedAt: d.CreatedAt,
			CreatedBy: d.CreatedBy, Metadata: d.Metadata, ThreadID: d.ThreadID,
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

// ---------- ready ----------

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

// ---------- labels ----------

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

// ---------- comments ----------

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

// ---------- helpers ----------

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
