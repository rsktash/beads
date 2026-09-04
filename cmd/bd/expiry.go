package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/rsktash/beads"
)

// defaultStaleDays is the idle window every renderer marks a question over
// unless the invocation overrides it (bd question list --stale-days).
const defaultStaleDays = 14

// questionExpiryNote is the single source of the "possibly moot" marker.
// Staleness is computed at read time — now minus changed_at, else created_at —
// and nothing is stored and no status changes: closing stays an owner or
// coordinator act, gated exactly as bd question close already is. It returns
// "" for a question that carries no marker, and otherwise the marker line and
// the prepared close command, newline-separated. bd never invents a reason:
// the command carries a literal "<why>" placeholder for the reader to fill.
func questionExpiryNote(q beads.Statement, now time.Time, days int) string {
	if days <= 0 || q.Status != "active" {
		return ""
	}
	at := q.CreatedAt
	if q.ChangedAt != nil {
		at = *q.ChangedAt
	}
	idle := now.Sub(at)
	if idle <= time.Duration(days)*24*time.Hour {
		return ""
	}
	marker := fmt.Sprintf("possibly moot (%dd)", int(idle.Hours()/24))
	command := fmt.Sprintf("bd question close %s --reason moot --note \"<why>\"", q.ID)
	return marker + "\n" + command
}

// expiryNoteParts splits questionExpiryNote's two lines: the marker that ends
// the row and the prepared command printed under it.
func expiryNoteParts(note string) (marker, command string) {
	marker, command, _ = strings.Cut(note, "\n")
	return marker, command
}
