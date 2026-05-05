package store

import (
	"database/sql"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/internal/db/pgdb"
	"github.com/rsktash/beads/internal/db/sqlitedb"
)

func fromSqliteIssue(r sqlitedb.Issue) *beads.Issue {
	return assemble(issueFields{
		ID: r.ID, ContentHash: r.ContentHash, Title: r.Title,
		Description: r.Description, Design: r.Design,
		AcceptanceCriteria: r.AcceptanceCriteria, Notes: r.Notes,
		Status: r.Status, Priority: int(r.Priority), Type: r.IssueType,
		Assignee: r.Assignee, EstimatedMinutes: int(r.EstimatedMinutes),
		CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy, Owner: r.Owner,
		UpdatedAt: r.UpdatedAt, ClosedAt: r.ClosedAt,
		ClosedBySession: r.ClosedBySession,
		ExternalRef:     r.ExternalRef, SpecID: r.SpecID, Metadata: r.Metadata,
		SourceRepo: r.SourceRepo, SourceSystem: r.SourceSystem, CloseReason: r.CloseReason,
		Sender: r.Sender, Ephemeral: r.Ephemeral != 0, Pinned: r.Pinned != 0,
		IsTemplate: r.IsTemplate != 0,
		WispType:   r.WispType, MolType: r.MolType, RoleType: r.RoleType,
		EventKind: r.EventKind, Actor: r.Actor, Target: r.Target, Payload: r.Payload,
		StartedAt: r.StartedAt, DueAt: r.DueAt, DeferUntil: r.DeferUntil,
	})
}

func fromPgIssue(r pgdb.Issue) *beads.Issue {
	return assemble(issueFields{
		ID: r.ID, ContentHash: r.ContentHash, Title: r.Title,
		Description: r.Description, Design: r.Design,
		AcceptanceCriteria: r.AcceptanceCriteria, Notes: r.Notes,
		Status: r.Status, Priority: int(r.Priority), Type: r.IssueType,
		Assignee: r.Assignee, EstimatedMinutes: int(r.EstimatedMinutes),
		CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy, Owner: r.Owner,
		UpdatedAt: r.UpdatedAt, ClosedAt: r.ClosedAt,
		ClosedBySession: r.ClosedBySession,
		ExternalRef:     r.ExternalRef, SpecID: r.SpecID, Metadata: r.Metadata,
		SourceRepo: r.SourceRepo, SourceSystem: r.SourceSystem, CloseReason: r.CloseReason,
		Sender: r.Sender, Ephemeral: r.Ephemeral != 0, Pinned: r.Pinned != 0,
		IsTemplate: r.IsTemplate != 0,
		WispType:   r.WispType, MolType: r.MolType, RoleType: r.RoleType,
		EventKind: r.EventKind, Actor: r.Actor, Target: r.Target, Payload: r.Payload,
		StartedAt: r.StartedAt, DueAt: r.DueAt, DeferUntil: r.DeferUntil,
	})
}

// issueFields is the union of fields read from either generated Issue type;
// it lets assemble() be the single point that turns a row into beads.Issue.
type issueFields struct {
	ID, ContentHash, Title, Description, Design, AcceptanceCriteria, Notes string
	Status                                                                 string
	Priority                                                               int
	Type                                                                   string
	Assignee                                                               string
	EstimatedMinutes                                                       int
	CreatedAt                                                              time.Time
	CreatedBy, Owner                                                       string
	UpdatedAt                                                              time.Time
	ClosedAt                                                               sql.NullTime
	ClosedBySession                                                        string
	ExternalRef, SpecID, Metadata, SourceRepo, SourceSystem, CloseReason   string
	Sender                                                                 string
	Ephemeral, Pinned, IsTemplate                                          bool
	WispType, MolType, RoleType                                            string
	EventKind, Actor, Target, Payload                                      string
	StartedAt, DueAt, DeferUntil                                           sql.NullTime
}

func assemble(f issueFields) *beads.Issue {
	return &beads.Issue{
		ID: f.ID, ContentHash: f.ContentHash, Title: f.Title,
		Description: f.Description, Design: f.Design,
		AcceptanceCriteria: f.AcceptanceCriteria, Notes: f.Notes,
		Status:           beads.Status(f.Status),
		Priority:         f.Priority,
		Type:             beads.IssueType(f.Type),
		Assignee:         f.Assignee,
		EstimatedMinutes: f.EstimatedMinutes,
		CreatedAt:        f.CreatedAt, CreatedBy: f.CreatedBy, Owner: f.Owner,
		UpdatedAt: f.UpdatedAt, ClosedAt: timePtr(f.ClosedAt),
		ClosedBySession: f.ClosedBySession,
		ExternalRef:     f.ExternalRef, SpecID: f.SpecID, Metadata: f.Metadata,
		SourceRepo: f.SourceRepo, SourceSystem: f.SourceSystem, CloseReason: f.CloseReason,
		Sender: f.Sender, Ephemeral: f.Ephemeral, Pinned: f.Pinned, IsTemplate: f.IsTemplate,
		WispType: f.WispType, MolType: f.MolType, RoleType: f.RoleType,
		EventKind: f.EventKind, Actor: f.Actor, Target: f.Target, Payload: f.Payload,
		StartedAt: timePtr(f.StartedAt), DueAt: timePtr(f.DueAt),
		DeferUntil: timePtr(f.DeferUntil),
	}
}

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// scanIssue reads a row from a *sql.Rows for the dynamic ListIssues path.
// Column order MUST match `SELECT *` from issues — i.e. the schema column
// order. Keep aligned with sql/schema.{sqlite,postgres}.sql.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanIssue(r rowScanner) (*beads.Issue, error) {
	var (
		f                   issueFields
		ephemeral           int64
		pinned              int64
		isTemplate          int64
		priority            int64
		estimatedMinutes    int64
	)
	if err := r.Scan(
		&f.ID, &f.ContentHash, &f.Title, &f.Description, &f.Design,
		&f.AcceptanceCriteria, &f.Notes,
		&f.Status, &priority, &f.Type, &f.Assignee, &estimatedMinutes,
		&f.CreatedAt, &f.CreatedBy, &f.Owner, &f.UpdatedAt,
		&f.StartedAt, &f.ClosedAt, &f.ClosedBySession,
		&f.ExternalRef, &f.SpecID, &f.Metadata,
		&f.SourceRepo, &f.SourceSystem, &f.CloseReason,
		&f.Sender, &ephemeral, &pinned, &isTemplate,
		&f.WispType, &f.MolType, &f.RoleType,
		&f.EventKind, &f.Actor, &f.Target, &f.Payload,
		&f.DueAt, &f.DeferUntil,
	); err != nil {
		return nil, err
	}
	f.Priority = int(priority)
	f.EstimatedMinutes = int(estimatedMinutes)
	f.Ephemeral = ephemeral != 0
	f.Pinned = pinned != 0
	f.IsTemplate = isTemplate != 0
	return assemble(f), nil
}
