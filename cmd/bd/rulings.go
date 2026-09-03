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
	var grep string
	cmd := &cobra.Command{
		Use:   "rulings [issue-id]",
		Short: "List active rulings",
		Long:  "List active rulings. Without an issue id, lists every active ruling in the project newest first. With an issue id, lists what that bead is bound by via the inheritance resolver (same set the contract shows). Use --scope project to list only project-scoped rulings (issue_id IS NULL). Use --grep <kw> to substring-match across every scope (case-insensitive, over law+text); it cannot be combined with an issue id.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRulings(cmd, args, scope, grep)
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "filter scope: project (only project-scoped rulings)")
	cmd.Flags().StringVar(&grep, "grep", "", "case-insensitive substring match over law+text, across every scope")
	return cmd
}

// runRulings is the shared implementation behind `bd rulings` and its
// sibling alias `bd ruling list`; both commands parse their own flags into
// local vars and call this with the resolved scope/grep strings.
func runRulings(cmd *cobra.Command, args []string, scope, grep string) error {
	cc, err := openStore(cmd)
	if err != nil {
		return err
	}
	defer cc.store.Close()

	if cmd.Flags().Changed("scope") && scope != "" && scope != "project" {
		return fmt.Errorf("invalid --scope %q (only project is supported)", scope)
	}
	isProjectScope := scope == "project"

	if grep != "" && len(args) == 1 {
		return fmt.Errorf("--grep searches every scope; drop the issue id")
	}

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
		if grep != "" {
			kw := strings.ToLower(grep)
			var matched []beads.Statement
			for _, r := range list {
				if strings.Contains(strings.ToLower(r.Law+" "+r.Text), kw) {
					matched = append(matched, r)
				}
			}
			list = matched
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
			fmt.Fprintln(out, rulingHeadline(r, issueID))
		}
	} else {
		for _, r := range rulings {
			if terse {
				fmt.Fprintln(out, terseRulingLine(r))
				continue
			}
			// issueID "" never matches a real bead id, so the
			// origin marker rulingHeadline appends is always
			// present here, matching the pre-existing project-wide
			// shape (every row carries its scope).
			fmt.Fprintln(out, rulingHeadline(r, ""))
		}
	}
	return nil
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
	return fmt.Sprintf("%s  [%s]  %s", r.ID, scope, headlineText(r.Text))
}
