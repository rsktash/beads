package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/rustamsmax/beads/internal/types"
)

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printIssueTable(issues []types.Issue) {
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
