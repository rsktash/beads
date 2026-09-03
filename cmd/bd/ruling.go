package main

import (
	"fmt"
	"io"
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
	root.AddCommand(newRulingRetireCmd())
	return root
}

func newRulingAddCmd() *cobra.Command {
	var (
		supersedes string
		answers    string
		scope      string
		deferStr   string
		closeFlag  bool
		parkFlag   bool
		verbatim   string
	)
	cmd := &cobra.Command{
		Use:   "add [<issue-id>] <text>",
		Short: "File a ruling (actor-gated: BD_ACTOR=executor is refused)",
		Long: `File a ruling. With one arg, files a project-scoped ruling (issue_id NULL). With two args, first is issue id, second is text.

Flags --defer, --park and --close atomically update the bead's state in the same transaction as the ruling row.
--park defers the bead to the far future (9999-12-31) and adds label 'parked'; it is deferred+parked label, no new status.

Actor gating: BD_ACTOR=executor cannot file rulings; use a finding or question instead. BD_ACTOR=coordinator or unset (owner) is allowed.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				return executorRefusal(identity)
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
			hasPark := parkFlag
			if hasDefer && hasClose {
				return fmt.Errorf("--defer and --close are mutually exclusive")
			}
			if hasPark && hasDefer {
				return fmt.Errorf("--park and --defer are mutually exclusive")
			}
			if hasPark && hasClose {
				return fmt.Errorf("--park and --close are mutually exclusive")
			}
			// defer/park/close require issue id
			if (hasDefer || hasClose || hasPark) && issueID == nil {
				return fmt.Errorf("--defer, --park and --close require an issue id")
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
				Kind:     "ruling",
				IssueID:  issueID,
				Text:     text,
				FiledBy:  identity,
				Status:   "active",
				Scope:    scopeVal,
				Verbatim: strings.TrimSpace(verbatim),
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
			if hasPark {
				t := store.ParkDeferUntil
				upd = &store.IssueUpdate{DeferUntil: &t, AddLabels: []string{"parked"}}
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

			// Print the bead's own existing rulings to stderr, and refuse a
			// write that repeats one of their headlines — unless this ruling
			// supersedes one explicitly, which is the sanctioned way to
			// restate.
			if err := printExistingRulingsAndCheckDuplicate(cc, cmd.ErrOrStderr(), issueID, text, st.SupersedesID != nil); err != nil {
				return err
			}

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
	cmd.Flags().BoolVar(&parkFlag, "park", false, "park the bead (defer far future + label 'parked', atomic with ruling)")
	cmd.Flags().StringVar(&verbatim, "verbatim", "", "the owner's verbatim sentence backing this ruling (stored untouched, never in the default headline)")
	return cmd
}

// printExistingRulingsAndCheckDuplicate lists the set a new ruling would
// join — the bead's own active rulings for an issue-scoped ruling, or the
// project-scoped rows for a project ruling (never the inheritance-resolved
// set: the refusal below must stay actionable with --supersedes, and a
// child bead cannot supersede a ruling it only inherited). The listing
// always prints, even when the write goes on to succeed or the set is
// empty, and always to errW (stderr) so the command's stdout stays new-id-only.
// When skipDuplicateCheck is false, it also refuses a text whose headline
// (case-insensitive, first 120 runes) repeats an existing one.
func printExistingRulingsAndCheckDuplicate(cc *cmdCtx, errW io.Writer, issueID *string, text string, skipDuplicateCheck bool) error {
	f := store.StatementFilter{Kinds: []string{"ruling"}, Statuses: []string{"active"}}
	var scopeLabel, headlineIssueID string
	if issueID != nil {
		f.IssueIDs = []string{*issueID}
		scopeLabel = *issueID
		headlineIssueID = *issueID
	} else {
		f.IssueIDs = []string{""}
		scopeLabel = "the project"
	}

	list, err := cc.store.ListStatements(cc.ctx, f)
	if err != nil {
		return err
	}

	if len(list) == 0 {
		fmt.Fprintf(errW, "no existing rulings on %s\n", scopeLabel)
	} else {
		fmt.Fprintf(errW, "existing rulings on %s (%d):\n", scopeLabel, len(list))
		for _, r := range list {
			fmt.Fprintln(errW, "  "+rulingHeadline(r, headlineIssueID))
		}
	}

	if skipDuplicateCheck {
		return nil
	}

	newHeadline := strings.ToLower(headlineText(text))
	for _, r := range list {
		if strings.ToLower(headlineText(r.Text)) == newHeadline {
			addArgs := fmt.Sprintf("%q", "<text>")
			if issueID != nil {
				addArgs = fmt.Sprintf("%s %q", *issueID, "<text>")
			}
			return fmt.Errorf("%s already says this on %s — amend it with `bd ruling add %s --supersedes %s`, or file a question if it is genuinely a new decision", r.ID, scopeLabel, addArgs, r.ID)
		}
	}
	return nil
}
