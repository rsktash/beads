package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func writeJSON(v any) error {
	return writeJSONTo(os.Stdout, v)
}

func writeJSONTo(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// slimIssue is the default JSON row for list/ready: just the fields skills
// actually triage from. Heavy bodies (description/design/notes), labels,
// timestamps, and the discriminator columns stay out unless --full opts in.
type slimIssue struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Status   beads.Status    `json:"status"`
	Priority int             `json:"priority"`
	Type     beads.IssueType `json:"issue_type"`
	Assignee string          `json:"assignee,omitempty"`
	Labels   []string        `json:"labels,omitempty"`
}

// slimIssues converts to the slim JSON row, attaching each issue's labels.
// ListIssues/Ready don't preload labels, so this is one ListLabels call per
// row; list/ready result sets are the kind of size (tens, not thousands)
// where that's cheap relative to the query that produced them. omitempty
// matches beads.Issue.Labels' own json tag (beads.go) — unlabeled beads emit
// no "labels" key rather than "labels": [].
func slimIssues(ctx context.Context, st *store.Store, in []beads.Issue) ([]slimIssue, error) {
	out := make([]slimIssue, len(in))
	for i, x := range in {
		labels, err := st.ListLabels(ctx, x.ID)
		if err != nil {
			return nil, err
		}
		out[i] = slimIssue{
			ID:       x.ID,
			Title:    x.Title,
			Status:   x.Status,
			Priority: x.Priority,
			Type:     x.Type,
			Assignee: x.Assignee,
			Labels:   labels,
		}
	}
	return out, nil
}

func printIssueTable(issues []beads.Issue) {
	if len(issues) == 0 {
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tP\tSTATUS\tTYPE\tASSIGNEE\tTITLE")
	for _, i := range issues {
		assignee := i.Assignee
		if assignee == "" {
			assignee = "-"
		}
		title := i.Title
		if len(title) > 64 {
			title = title[:61] + "..."
		}
		fmt.Fprintf(w, "%s\tp%d\t%s\t%s\t%s\t%s\n",
			i.ID, i.Priority, i.Status, i.Type, assignee, strings.ReplaceAll(title, "\n", " "))
	}
	w.Flush()
}
