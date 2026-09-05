package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// An execution plan orders beads drawn from any number of epics. Each lane is
// a concurrent slice of that order with one cursor and at most one holding
// session; a session closes by appending a typed handoff entry. The list
// columns (queue, rulings, done_ids, parked) are comma-delimited text rather
// than join tables, matching how labels and evidence are already stored.

// ExecutionPlan is one plan record.
type ExecutionPlan struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Preflight string    `json:"preflight,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by,omitempty"`
}

// PlanLane is one concurrent slice of a plan.
type PlanLane struct {
	PlanID    string     `json:"plan_id"`
	Lane      string     `json:"lane"`
	Queue     []string   `json:"queue"`
	Cursor    int        `json:"cursor"`
	Mode      string     `json:"mode"`
	Rulings   []string   `json:"rulings,omitempty"`
	Holder    string     `json:"holder,omitempty"`
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`
}

// QueueSlot identifies one bead's plan, its lane in that plan, and its
// one-based position in the lane.
type QueueSlot struct {
	Plan  string
	Lane  string
	Index int
}

// Next returns the queue entry the cursor points at, or "" when the lane is
// exhausted. Readiness is never read from this value — see cmd/bd plan show.
func (l PlanLane) Next() string {
	if l.Cursor < 0 || l.Cursor >= len(l.Queue) {
		return ""
	}
	return l.Queue[l.Cursor]
}

// Done reports a lane whose cursor has run past the end of its queue.
func (l PlanLane) Done() bool { return l.Cursor >= len(l.Queue) }

// PlanSession is a session that joined a plan. Lane "" is the waiting pool.
type PlanSession struct {
	PlanID    string    `json:"plan_id"`
	SessionID string    `json:"session_id"`
	Lane      string    `json:"lane"`
	JoinedAt  time.Time `json:"joined_at"`
}

// PlanHandoff is the typed entry a session appends when it releases a lane.
// Done holds <bead-id>:<commit-sha> pairs, Parked holds <bead-id>:<question-id>
// pairs; both are validated by the caller before they reach the store.
type PlanHandoff struct {
	ID        string    `json:"id"`
	PlanID    string    `json:"plan_id"`
	Lane      string    `json:"lane"`
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
	Done      []string  `json:"done,omitempty"`
	NextID    string    `json:"next_id,omitempty"`
	Parked    []string  `json:"parked,omitempty"`
	Thread    string    `json:"thread,omitempty"`
}

// LaneHeldError reports a claim that lost the race. The lane's current holder
// is read only after the atomic UPDATE reported zero rows.
type LaneHeldError struct {
	Lane      string
	Holder    string
	ClaimedAt time.Time
}

func (e *LaneHeldError) Error() string {
	return fmt.Sprintf("lane %s is held by session %s since %s",
		e.Lane, e.Holder, e.ClaimedAt.Format(time.RFC3339))
}

// ErrNotHolder is the refusal for a handoff from a session that does not hold
// the lane.
var ErrNotHolder = errors.New("session does not hold the lane")

// splitList parses a comma-delimited column into its entries, dropping empties.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// joinList is splitList's inverse.
func joinList(v []string) string { return strings.Join(v, ",") }

// CreatePlan inserts a plan. The id and created_at are the caller's to choose
// (ids are slug-derived in cmd/bd); status defaults to active.
func (s *Store) CreatePlan(ctx context.Context, p *ExecutionPlan) error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("plan id is required")
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("plan title is required")
	}
	if p.Status == "" {
		p.Status = "active"
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	q := s.rebind(`INSERT INTO execution_plan (id, title, status, preflight, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?)`)
	_, err := s.db.ExecContext(ctx, q, p.ID, p.Title, p.Status, p.Preflight, p.CreatedAt, p.CreatedBy)
	return err
}

// GetPlan reads one plan, ErrNotFound when it does not exist.
func (s *Store) GetPlan(ctx context.Context, id string) (*ExecutionPlan, error) {
	q := s.rebind(`SELECT id, title, status, preflight, created_at, created_by FROM execution_plan WHERE id = ?`)
	var p ExecutionPlan
	err := s.db.QueryRowContext(ctx, q, id).Scan(&p.ID, &p.Title, &p.Status, &p.Preflight, &p.CreatedAt, &p.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ActivePlans returns every active execution plan ordered by id.
func (s *Store) ActivePlans(ctx context.Context) ([]ExecutionPlan, error) {
	q := s.rebind(`SELECT id, title, status, preflight, created_at, created_by FROM execution_plan WHERE status = 'active' ORDER BY id`)
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []ExecutionPlan
	for rows.Next() {
		var p ExecutionPlan
		if err := rows.Scan(&p.ID, &p.Title, &p.Status, &p.Preflight, &p.CreatedAt, &p.CreatedBy); err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return plans, nil
}

// ActivePlanQueues unions the queue slots of every active plan. It returns
// the active plan ids in id order plus a map from bead id to its slot. A bead
// sits in one lane and lanes belong to one plan; should a bead ever appear
// twice, the first slot seen in plan-id order wins.
func (s *Store) ActivePlanQueues(ctx context.Context) ([]string, map[string]QueueSlot, error) {
	plans, err := s.ActivePlans(ctx)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(plans))
	order := make(map[string]QueueSlot)
	for _, plan := range plans {
		lanes, err := s.ListLanes(ctx, plan.ID)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, plan.ID)
		for _, lane := range lanes {
			for i, issueID := range lane.Queue {
				if _, exists := order[issueID]; exists {
					continue
				}
				order[issueID] = QueueSlot{Plan: plan.ID, Lane: lane.Lane, Index: i + 1}
			}
		}
	}
	return ids, order, nil
}

// DeletePlan removes a plan; lanes, sessions and handoffs cascade.
func (s *Store) DeletePlan(ctx context.Context, id string) error {
	q := s.rebind(`DELETE FROM execution_plan WHERE id = ?`)
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddLane inserts a lane onto a plan.
func (s *Store) AddLane(ctx context.Context, l *PlanLane) error {
	if strings.TrimSpace(l.Lane) == "" {
		return fmt.Errorf("lane name is required")
	}
	if l.Mode == "" {
		l.Mode = "subagent"
	}
	q := s.rebind(`INSERT INTO plan_lane (plan_id, lane, queue, cursor, mode, rulings, holder, claimed_at) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL)`)
	_, err := s.db.ExecContext(ctx, q, l.PlanID, l.Lane, joinList(l.Queue), l.Cursor, l.Mode, joinList(l.Rulings))
	return err
}

// GetLane reads one lane, ErrNotFound when it does not exist.
func (s *Store) GetLane(ctx context.Context, planID, lane string) (*PlanLane, error) {
	q := s.rebind(`SELECT plan_id, lane, queue, cursor, mode, rulings, holder, claimed_at FROM plan_lane WHERE plan_id = ? AND lane = ?`)
	l, err := scanLane(s.db.QueryRowContext(ctx, q, planID, lane))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return l, nil
}

// ListLanes reads every lane of a plan in name order.
func (s *Store) ListLanes(ctx context.Context, planID string) ([]PlanLane, error) {
	q := s.rebind(`SELECT plan_id, lane, queue, cursor, mode, rulings, holder, claimed_at FROM plan_lane WHERE plan_id = ? ORDER BY lane`)
	rows, err := s.db.QueryContext(ctx, q, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanLane
	for rows.Next() {
		l, err := scanLane(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func scanLane(sc rowScanner) (*PlanLane, error) {
	var (
		l       PlanLane
		queue   string
		rulings string
		holder  sql.NullString
		claimed sql.NullTime
	)
	if err := sc.Scan(&l.PlanID, &l.Lane, &queue, &l.Cursor, &l.Mode, &rulings, &holder, &claimed); err != nil {
		return nil, err
	}
	l.Queue = splitList(queue)
	l.Rulings = splitList(rulings)
	l.Holder = holder.String
	l.ClaimedAt = nullTimeToPtr(claimed)
	return &l, nil
}

// JoinPlan records a session against a plan. Lane "" is the waiting pool;
// re-joining moves the session's lane rather than failing.
func (s *Store) JoinPlan(ctx context.Context, planID, sessionID, lane string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	q := s.rebind(`INSERT INTO plan_session (plan_id, session_id, lane, joined_at) VALUES (?, ?, ?, ?) ON CONFLICT (plan_id, session_id) DO UPDATE SET lane = excluded.lane`)
	_, err := s.db.ExecContext(ctx, q, planID, sessionID, lane, time.Now().UTC())
	return err
}

// ListPlanSessions reads every session on a plan, waiting pool first.
func (s *Store) ListPlanSessions(ctx context.Context, planID string) ([]PlanSession, error) {
	q := s.rebind(`SELECT plan_id, session_id, lane, joined_at FROM plan_session WHERE plan_id = ? ORDER BY lane, joined_at`)
	rows, err := s.db.QueryContext(ctx, q, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanSession
	for rows.Next() {
		var ps PlanSession
		if err := rows.Scan(&ps.PlanID, &ps.SessionID, &ps.Lane, &ps.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// ClaimLane takes the lane for a session. The whole claim is one conditional
// UPDATE: no read-then-write, no transaction, no second code path. Zero rows
// affected means somebody else holds it, and only then is the holder read —
// so two racing claims can never both succeed.
func (s *Store) ClaimLane(ctx context.Context, planID, lane, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	q := s.rebind(`UPDATE plan_lane SET holder = ?, claimed_at = ? WHERE plan_id = ? AND lane = ? AND holder IS NULL`)
	res, err := s.db.ExecContext(ctx, q, sessionID, time.Now().UTC(), planID, lane)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	held, err := s.GetLane(ctx, planID, lane)
	if err != nil {
		return err
	}
	e := &LaneHeldError{Lane: lane, Holder: held.Holder}
	if held.ClaimedAt != nil {
		e.ClaimedAt = *held.ClaimedAt
	}
	return e
}

// Handoff appends the typed entry and releases the lane in one transaction,
// moving the cursor to the caller's new position. A session that does not
// hold the lane is refused and nothing is written.
func (s *Store) Handoff(ctx context.Context, h *PlanHandoff, cursor int) error {
	if strings.TrimSpace(h.SessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if h.ID == "" {
		return fmt.Errorf("handoff id is required")
	}
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	rel := s.rebind(`UPDATE plan_lane SET holder = NULL, claimed_at = NULL, cursor = ? WHERE plan_id = ? AND lane = ? AND holder = ?`)
	res, err := tx.ExecContext(ctx, rel, cursor, h.PlanID, h.Lane, h.SessionID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: session %s cannot hand off lane %s", ErrNotHolder, h.SessionID, h.Lane)
	}

	ins := s.rebind(`INSERT INTO plan_handoff (id, plan_id, lane, session_id, created_at, done_ids, next_id, parked, thread) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if _, err := tx.ExecContext(ctx, ins, h.ID, h.PlanID, h.Lane, h.SessionID, h.CreatedAt,
		joinList(h.Done), h.NextID, joinList(h.Parked), h.Thread); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// ListHandoffs reads a plan's handoff entries, newest last.
func (s *Store) ListHandoffs(ctx context.Context, planID string) ([]PlanHandoff, error) {
	q := s.rebind(`SELECT id, plan_id, lane, session_id, created_at, done_ids, next_id, parked, thread FROM plan_handoff WHERE plan_id = ? ORDER BY lane, created_at, id`)
	rows, err := s.db.QueryContext(ctx, q, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanHandoff
	for rows.Next() {
		var (
			h      PlanHandoff
			done   string
			parked string
		)
		if err := rows.Scan(&h.ID, &h.PlanID, &h.Lane, &h.SessionID, &h.CreatedAt, &done, &h.NextID, &parked, &h.Thread); err != nil {
			return nil, err
		}
		h.Done = splitList(done)
		h.Parked = splitList(parked)
		out = append(out, h)
	}
	return out, rows.Err()
}

// LastHandoffAt returns the newest handoff instant for one plan lane. A lane
// with no handoff has a nil instant and no error.
func (s *Store) LastHandoffAt(ctx context.Context, planID, lane string) (*time.Time, error) {
	q := s.rebind(`SELECT created_at FROM plan_handoff WHERE plan_id = ? AND lane = ? ORDER BY created_at DESC, id DESC LIMIT 1`)
	var at time.Time
	err := s.db.QueryRowContext(ctx, q, planID, lane).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &at, nil
}

// NewestHandoffAt returns the newest handoff across every plan and lane. No
// handoff is represented by a nil instant.
func (s *Store) NewestHandoffAt(ctx context.Context) (*time.Time, error) {
	var at time.Time
	err := s.db.QueryRowContext(ctx, `SELECT created_at FROM plan_handoff WHERE created_at = (SELECT MAX(created_at) FROM plan_handoff) LIMIT 1`).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &at, nil
}

// BlockerClearedSince returns beads whose question was answered or whose
// blocking dependency closed at or after since.
func (s *Store) BlockerClearedSince(ctx context.Context, since time.Time) (map[string]bool, error) {
	q := s.rebind(`
SELECT issue_id
FROM statements
WHERE kind = 'question'
  AND status = 'answered'
  AND issue_id IS NOT NULL
  AND COALESCE(changed_at, created_at) >= ?
UNION
SELECT d.issue_id
FROM dependencies d
JOIN issues blocker ON blocker.id = d.depends_on_id
WHERE d.type = 'blocks'
  AND blocker.closed_at IS NOT NULL
  AND blocker.closed_at >= ?`)
	rows, err := s.db.QueryContext(ctx, q, since, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var issueID string
		if err := rows.Scan(&issueID); err != nil {
			return nil, err
		}
		if issueID != "" {
			out[issueID] = true
		}
	}
	return out, rows.Err()
}

// LastHandoffPerLane keys a plan's most recent handoff entry by lane.
func (s *Store) LastHandoffPerLane(ctx context.Context, planID string) (map[string]PlanHandoff, error) {
	all, err := s.ListHandoffs(ctx, planID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]PlanHandoff, len(all))
	for _, h := range all {
		out[h.Lane] = h // ordered lane, created_at, id — the last write wins
	}
	return out, nil
}
