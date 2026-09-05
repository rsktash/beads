package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
	"github.com/spf13/cobra"
)

func newReadyCmd() *cobra.Command {
	var (
		parent string
		full   bool
		limit  int
	)
	cmd := &cobra.Command{
		Use:   "ready",
		Short: "List beads with no open blockers, deferred, ephemeral, or open questions. --parent <id> scopes to descendants of that issue.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			out, err := cc.store.Ready(cc.ctx)
			if err != nil {
				return err
			}
			if parent != "" {
				// Walk the descendant tree of parent and intersect with
				// ready. Used by the executing-plans loop to drive work
				// against a single epic.
				descendants, err := walkChildren(cc.ctx, cc.store, parent, true)
				if err != nil {
					return err
				}
				inTree := make(map[string]bool, len(descendants))
				for _, d := range descendants {
					inTree[d.issue.ID] = true
				}
				filtered := out[:0]
				for _, i := range out {
					if inTree[i.ID] {
						filtered = append(filtered, i)
					}
				}
				out = filtered
			}
			planIDs, queue, err := orderReadyIssues(cc.ctx, cc.store, out)
			if err != nil {
				return err
			}
			if limit > 0 && len(out) > limit {
				out = out[:limit]
			}
			if cc.json {
				if full {
					return writeJSON(out)
				}
				rows, err := slimIssues(cc.ctx, cc.store, out)
				if err != nil {
					return err
				}
				return writeJSON(readyJSONRows(rows, queue))
			}
			if len(planIDs) > 0 {
				printReadyPlanTable(out, queue)
			} else {
				printIssueTable(out)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "scope to descendants of this issue id (parent-child)")
	cmd.Flags().BoolVar(&full, "full", false, "emit full Issue rows in --json (default: id/title/status/priority/type/assignee)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "cap returned rows (0 = unlimited)")
	return cmd
}

func orderReadyIssues(ctx context.Context, st *store.Store, issues []beads.Issue) ([]string, map[string]store.QueueSlot, error) {
	planIDs, queue, err := st.ActivePlanQueues(ctx)
	if err != nil {
		return nil, nil, err
	}

	cleared := map[string]bool(nil)
	lastHandoff, err := st.NewestHandoffAt(ctx)
	if err != nil {
		return nil, nil, err
	}
	if lastHandoff != nil {
		cleared, err = st.BlockerClearedSince(ctx, *lastHandoff)
		if err != nil {
			return nil, nil, err
		}
	}

	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		aSlot, aQueued := queue[a.ID]
		bSlot, bQueued := queue[b.ID]
		if aQueued != bQueued {
			return aQueued
		}
		if aQueued {
			if aSlot.Plan != bSlot.Plan {
				return aSlot.Plan < bSlot.Plan
			}
			if aSlot.Lane != bSlot.Lane {
				return aSlot.Lane < bSlot.Lane
			}
			return aSlot.Index < bSlot.Index
		}
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if lastHandoff != nil && cleared[a.ID] != cleared[b.ID] {
			return cleared[a.ID]
		}
		return a.UpdatedAt.After(b.UpdatedAt)
	})
	return planIDs, queue, nil
}

// readyJSONRow is the slim ready row plus the bead's queue slot: plan, lane,
// and one-based lane index. Beads in no lane keep the plain slim shape.
type readyJSONRow struct {
	slimIssue
	Plan  string `json:"plan,omitempty"`
	Lane  string `json:"lane,omitempty"`
	Index int    `json:"index,omitempty"`
}

func readyJSONRows(rows []slimIssue, queue map[string]store.QueueSlot) []readyJSONRow {
	out := make([]readyJSONRow, len(rows))
	for i, r := range rows {
		out[i] = readyJSONRow{slimIssue: r}
		if slot, ok := queue[r.ID]; ok {
			out[i].Plan = slot.Plan
			out[i].Lane = slot.Lane
			out[i].Index = slot.Index
		}
	}
	return out
}

// printReadyPlanTable renders the ready table with lane positions. The PLAN
// column leads only when the printed rows span more than one plan; a
// single-plan render keeps the single-plan columns byte for byte.
func printReadyPlanTable(issues []beads.Issue, queue map[string]store.QueueSlot) {
	if len(issues) == 0 {
		return
	}
	seen := map[string]bool{}
	for _, issue := range issues {
		if slot, ok := queue[issue.ID]; ok && slot.Plan != "" {
			seen[slot.Plan] = true
		}
	}
	multiPlan := len(seen) > 1
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	if multiPlan {
		fmt.Fprintln(w, "PLAN\tLANE\tID\tP\tSTATUS\tTYPE\tASSIGNEE\tTITLE")
	} else {
		fmt.Fprintln(w, "LANE\tID\tP\tSTATUS\tTYPE\tASSIGNEE\tTITLE")
	}
	for _, issue := range issues {
		lane := ""
		plan := ""
		if slot, ok := queue[issue.ID]; ok {
			lane = fmt.Sprintf("%s%d", slot.Lane, slot.Index)
			plan = slot.Plan
		}
		assignee := issue.Assignee
		if assignee == "" {
			assignee = "-"
		}
		title := issue.Title
		if len(title) > 64 {
			title = title[:61] + "..."
		}
		if multiPlan {
			if plan == "" {
				plan = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\tp%d\t%s\t%s\t%s\t%s\n",
				plan, lane, issue.ID, issue.Priority, issue.Status, issue.Type, assignee, strings.ReplaceAll(title, "\n", " "))
			continue
		}
		fmt.Fprintf(w, "%s\t%s\tp%d\t%s\t%s\t%s\t%s\n",
			lane, issue.ID, issue.Priority, issue.Status, issue.Type, assignee, strings.ReplaceAll(title, "\n", " "))
	}
	w.Flush()
}
