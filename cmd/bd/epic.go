package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// `bd epic status [<id>]` — child counts grouped by status.
//   - Without an id: lists every open epic with counts (open / in_progress
//     / blocked / closed) of its parent-child descendants.
//   - With an id: shows that one epic's counts plus a header line.
//
// Closes the gap with upstream's `bd epic status` that bd-defaults skills
// rely on.
func newEpicCmd() *cobra.Command {
	root := &cobra.Command{Use: "epic", Short: "Inspect epics"}

	statusC := &cobra.Command{
		Use:   "status [<id>]",
		Short: "Show child counts (open / in_progress / blocked / closed) per epic",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			var epics []beads.Issue
			if len(args) == 1 {
				ep, err := cc.store.GetIssue(cc.ctx, args[0])
				if err != nil {
					return err
				}
				epics = append(epics, *ep)
			} else {
				epicType := beads.TypeEpic
				openStatus := beads.StatusOpen
				rows, err := cc.store.ListIssues(cc.ctx, store.ListFilter{
					Type:   &epicType,
					Status: &openStatus,
				})
				if err != nil {
					return err
				}
				epics = rows
			}

			type counts struct {
				ID, Title                            string
				Status                               beads.Status
				Open, InProgress, Blocked, Closed, Other int
			}
			out := make([]counts, 0, len(epics))
			for _, e := range epics {
				kids, err := walkChildren(cc.ctx, cc.store, e.ID, true)
				if err != nil {
					return err
				}
				c := counts{ID: e.ID, Title: e.Title, Status: e.Status}
				for _, k := range kids {
					switch k.issue.Status {
					case beads.StatusOpen:
						c.Open++
					case beads.StatusInProgress:
						c.InProgress++
					case beads.StatusBlocked:
						c.Blocked++
					case beads.StatusClosed:
						c.Closed++
					default:
						c.Other++
					}
				}
				out = append(out, c)
			}

			if cc.json {
				return writeJSON(out)
			}
			for _, c := range out {
				title := c.Title
				if len(title) > 50 {
					title = title[:47] + "..."
				}
				parts := []string{}
				parts = append(parts, fmt.Sprintf("open=%d", c.Open))
				parts = append(parts, fmt.Sprintf("in_progress=%d", c.InProgress))
				if c.Blocked > 0 {
					parts = append(parts, fmt.Sprintf("blocked=%d", c.Blocked))
				}
				parts = append(parts, fmt.Sprintf("closed=%d", c.Closed))
				if c.Other > 0 {
					parts = append(parts, fmt.Sprintf("other=%d", c.Other))
				}
				fmt.Printf("%s  [%s]  %-50s  %s\n", c.ID, c.Status, title, strings.Join(parts, "  "))
			}
			return nil
		},
	}

	root.AddCommand(statusC)
	return root
}
