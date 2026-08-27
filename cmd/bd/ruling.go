package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newRulingCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ruling",
		Short: "Manage rulings (typed authority records)",
	}
	root.AddCommand(newRulingAddCmd())
	return root
}

func newRulingAddCmd() *cobra.Command {
	var (
		supersedes string
		answers    string
		scope      string
		deferStr   string
		closeFlag  bool
	)
	cmd := &cobra.Command{
		Use:   "add [<issue-id>] <text>",
		Short: "File a ruling (actor-gated: BD_ACTOR=executor is refused)",
		Long: `File a ruling. With one arg, files a project-scoped ruling (issue_id NULL). With two args, first is issue id, second is text.

Flags --defer and --close atomically update the bead's state in the same transaction as the ruling row.

Actor gating: BD_ACTOR=executor cannot file rulings; use a finding or question instead. BD_ACTOR=coordinator or unset (owner) is allowed.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				// Extract actor word for message (before colon)
				word := identity
				if idx := strings.IndexByte(word, ':'); idx >= 0 {
					word = word[:idx]
				}
				if word == "" {
					word = "executor"
				}
				// Must name BD_ACTOR and what executor may file instead
				return fmt.Errorf("BD_ACTOR=%s: executors cannot file rulings; executors may file findings or questions instead", word)
			}

			// Determine issue id and text
			var issueID *string
			var text string
			if len(args) == 1 {
				text = strings.TrimSpace(args[0])
				// project-scoped: issueID stays nil
			} else {
				id := strings.TrimSpace(args[0])
				if id == "" {
					return fmt.Errorf("issue id is required when two args given")
				}
				issueID = &id
				text = strings.TrimSpace(args[1])
			}
			if text == "" {
				return fmt.Errorf("text is required")
			}

			// Validate mutually exclusive state flags
			f := cmd.Flags()
			hasDefer := f.Changed("defer")
			hasClose := f.Changed("close")
			if hasDefer && hasClose {
				return fmt.Errorf("--defer and --close are mutually exclusive")
			}
			// defer/close require issue id
			if (hasDefer || hasClose) && issueID == nil {
				return fmt.Errorf("--defer and --close require an issue id")
			}

			// Scope
			scopeVal := "inherit"
			if f.Changed("scope") {
				if scope == "self" {
					scopeVal = "self"
				} else if scope == "inherit" {
					scopeVal = "inherit"
				} else {
					return fmt.Errorf("invalid --scope %q (self|inherit)", scope)
				}
			}

			// Build statement
			st := &beads.Statement{
				Kind:    "ruling",
				IssueID: issueID,
				Text:    text,
				FiledBy: identity,
				Status:  "active",
				Scope:   scopeVal,
			}
			if f.Changed("supersedes") && supersedes != "" {
				s := strings.TrimSpace(supersedes)
				st.SupersedesID = &s
			}
			// answers is handled via store transaction param, not via st.AnsweredBy directly,
			// but we also support st.AnsweredBy path for the two-param store API.
			answersID := ""
			if f.Changed("answers") && answers != "" {
				answersID = strings.TrimSpace(answers)
			}

			// Build issue update if needed
			var upd *store.IssueUpdate
			if hasDefer {
				t, err := time.Parse(time.RFC3339, deferStr)
				if err != nil {
					return fmt.Errorf("--defer: %w", err)
				}
				upd = &store.IssueUpdate{DeferUntil: &t}
			}
			if hasClose {
				closed := beads.StatusClosed
				upd = &store.IssueUpdate{Status: &closed}
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			// Use atomic transaction that spans statements and issues.
			if answersID != "" {
				err = cc.store.CreateRulingWithStateChange(cc.ctx, st, upd, answersID)
			} else {
				err = cc.store.CreateStatementWithIssueUpdate(cc.ctx, st, upd)
			}
			if err != nil {
				return err
			}

			// Print new id to caller's stdout (cobra managed)
			fmt.Fprintln(cmd.OutOrStdout(), st.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&supersedes, "supersedes", "", "ruling id this supersedes (sets old to superseded)")
	cmd.Flags().StringVar(&answers, "answers", "", "question id this ruling answers")
	cmd.Flags().StringVar(&scope, "scope", "", "scope: self or inherit (default inherit)")
	cmd.Flags().StringVar(&deferStr, "defer", "", "defer bead until RFC3339 timestamp (atomic with ruling)")
	cmd.Flags().BoolVar(&closeFlag, "close", false, "close the bead (atomic with ruling)")
	return cmd
}
