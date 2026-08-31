package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newRulingsCmd() *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "rulings [issue-id]",
		Short: "List active rulings",
		Long:  "List active rulings. Without an issue id, lists every active ruling in the project newest first. With an issue id, lists what that bead is bound by via the inheritance resolver (same set the contract shows). Use --scope project to list only project-scoped rulings (issue_id IS NULL).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			if cmd.Flags().Changed("scope") && scope != "" && scope != "project" {
				return fmt.Errorf("invalid --scope %q (only project is supported)", scope)
			}
			isProjectScope := scope == "project"

			var rulings []beads.Statement

			if len(args) == 1 {
				if isProjectScope {
					return fmt.Errorf("--scope project cannot be combined with an issue id")
				}
				issueID := strings.TrimSpace(args[0])
				if issueID == "" {
					return fmt.Errorf("issue id is required")
				}
				if _, err := cc.store.GetIssue(cc.ctx, issueID); err != nil {
					return err
				}
				cv, err := cc.store.ContractStatements(cc.ctx, issueID)
				if err != nil {
					return err
				}
				rulings = cv.Rulings
			} else {
				f := store.StatementFilter{
					Kinds:    []string{"ruling"},
					Statuses: []string{"active"},
				}
				if isProjectScope {
					f.IssueIDs = []string{""}
				}
				list, err := cc.store.ListStatements(cc.ctx, f)
				if err != nil {
					return err
				}
				// Terminal reduction: exclude superseded ids even if status still active.
				// This ensures a chain R-1 -> R-2 where R-1 remains active status but is superseded is not listed.
				if len(list) > 0 {
					superseded := map[string]bool{}
					for _, r := range list {
						if r.SupersedesID != nil && *r.SupersedesID != "" {
							superseded[*r.SupersedesID] = true
						}
					}
					var terminals []beads.Statement
					for _, r := range list {
						if !superseded[r.ID] {
							terminals = append(terminals, r)
						}
					}
					list = terminals
				}
				rulings = list
			}

			if cc.json {
				if rulings == nil {
					rulings = []beads.Statement{}
				}
				return writeJSONTo(cmd.OutOrStdout(), rulings)
			}

			out := cmd.OutOrStdout()
			terse := wantTerse(out)

			if len(args) == 1 {
				issueID := strings.TrimSpace(args[0])
				for _, r := range rulings {
					if terse {
						fmt.Fprintln(out, terseRulingLine(r))
						continue
					}
					date := r.CreatedAt.Format("2006-01-02")
					prefix := fmt.Sprintf("%s  %s", r.ID, date)
					if r.IssueID == nil {
						prefix += "  [project]"
					} else if *r.IssueID != issueID {
						prefix += fmt.Sprintf("  [%s]", *r.IssueID)
					}
					fmt.Fprintf(out, "%s  %s\n", prefix, r.Text)
				}
			} else {
				for _, r := range rulings {
					if terse {
						fmt.Fprintln(out, terseRulingLine(r))
						continue
					}
					issueStr := "project"
					if r.IssueID != nil && *r.IssueID != "" {
						issueStr = *r.IssueID
					}
					date := r.CreatedAt.Format("2006-01-02")
					fmt.Fprintf(out, "%s  %s  [%s]  %s\n", r.ID, date, issueStr, r.Text)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "filter scope: project (only project-scoped rulings)")
	return cmd
}

// terseRulingLine renders the terse-mode row: id, scope marker, text — no
// date, no decorative framing. The scope marker is always present (project
// or the owning issue id), unlike the human render, which omits it when the
// ruling belongs to the queried issue — terse output stays a fixed 3-field
// shape a caller can parse without knowing which branch produced it.
func terseRulingLine(r beads.Statement) string {
	scope := "project"
	if r.IssueID != nil && *r.IssueID != "" {
		scope = *r.IssueID
	}
	return fmt.Sprintf("%s  [%s]  %s", r.ID, scope, r.Text)
}
