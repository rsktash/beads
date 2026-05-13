package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/rsktash/beads"
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
}

func slimIssues(in []beads.Issue) []slimIssue {
	out := make([]slimIssue, len(in))
	for i, x := range in {
		out[i] = slimIssue{
			ID:       x.ID,
			Title:    x.Title,
			Status:   x.Status,
			Priority: x.Priority,
			Type:     x.Type,
			Assignee: x.Assignee,
		}
	}
	return out
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
