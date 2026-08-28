package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newStatementsListCmd() *cobra.Command {
	var (
		candidates bool
		statusFlag string
	)
	cmd := &cobra.Command{
		Use:   "list [issue-id]",
		Short: "List candidate statements (review surface for backfill)",
		Long: `List statements by status so the owner can review what backfill produced
before promoting. The default view lists candidates; with an issue id it scopes
to that bead. Use --status to list another status instead.

Each row shows: statement id, kind, issue id, source comment id, created date,
and a short text preview. --json emits the full rows.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			status := "candidate"
			if cmd.Flags().Changed("status") {
				status = strings.TrimSpace(statusFlag)
				if status == "" {
					return fmt.Errorf("--status requires a value")
				}
			}
			_ = candidates // --candidates is the default view; the flag makes it explicit

			f := store.StatementFilter{Statuses: []string{status}}
			if len(args) == 1 {
				issueID := strings.TrimSpace(args[0])
				if issueID == "" {
					return fmt.Errorf("issue id is required")
				}
				f.IssueIDs = []string{issueID}
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			list, err := cc.store.ListStatements(cc.ctx, f)
			if err != nil {
				return err
			}

			if cc.json || flagJSON {
				if list == nil {
					list = []beads.Statement{}
				}
				return writeJSONTo(cmd.OutOrStdout(), list)
			}

			for _, s := range list {
				issueStr := "project"
				if s.IssueID != nil && *s.IssueID != "" {
					issueStr = *s.IssueID
				}
				srcStr := "-"
				if s.SourceCommentID != nil && *s.SourceCommentID != "" {
					srcStr = *s.SourceCommentID
				}
				date := s.CreatedAt.Format("2006-01-02")
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  [%s]  src=%s  %s  %s\n",
					s.ID, s.Kind, issueStr, srcStr, date, previewText(s.Text))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&candidates, "candidates", false, "list candidate statements (the default view)")
	cmd.Flags().StringVar(&statusFlag, "status", "", "list statements with this status instead of candidate")
	return cmd
}

// previewText returns a single-line, length-capped preview of statement text.
func previewText(text string) string {
	s := strings.ReplaceAll(text, "\n", " ")
	s = strings.TrimSpace(s)
	const max = 60
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
