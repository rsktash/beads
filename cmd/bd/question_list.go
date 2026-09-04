package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// questionListStatuses is the closed set the statements table allows.
var questionListStatuses = []string{"active", "answered", "retracted", "superseded", "candidate"}

func newQuestionListCmd() *cobra.Command {
	var statusFlag string
	var staleDays int
	cmd := &cobra.Command{
		Use:   "list [issue-id]",
		Short: "List questions — the blocked frontier (read-only, open to every actor)",
		Long: `List questions. Without an issue id, lists every active question in the project,
newest first: what is blocked right now, and on what. With an issue id, lists that
bead's own questions.

Questions do not inherit. The resolved_statements view carries only rulings past
depth 0, so a question is only ever shown on the bead it was filed on; an issue-scoped
listing never picks up an ancestor's or a project-scoped question.

--status lists another status instead of active (answered, retracted, superseded).
Each row shows: question id, issue id, created date, and a short text preview. Where
a question was answered, the row names what answered it and of which kind. An
active question idle past --stale-days (default 14) carries the marker
"possibly moot (Nd)" with bd question close prepared on the line under it;
--stale-days 0 disables the marker, which never changes a question's status or
stops it blocking bd ready. --json emits the full rows.

Machine contract:
- No matches: prints "questions: no matches" on stdout, exit 0.
- Matches: one line per row on stdout, exit 0. The no-matches line never appears alongside rows.
- Any failure writes the error to stderr and exits non-zero, never printing the no-matches line.

Read-only: open to every actor, including executors. An executor must be able to see what blocks it.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			status := "active"
			if cmd.Flags().Changed("status") {
				status = strings.TrimSpace(statusFlag)
				if status == "" {
					return fmt.Errorf("--status requires a value")
				}
				known := false
				for _, s := range questionListStatuses {
					if s == status {
						known = true
						break
					}
				}
				if !known {
					return fmt.Errorf("invalid --status %q (%s)", status, strings.Join(questionListStatuses, "|"))
				}
			}

			f := store.StatementFilter{
				Kinds:    []string{"question"},
				Statuses: []string{status},
			}
			var issueID string
			if len(args) == 1 {
				issueID = strings.TrimSpace(args[0])
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

			if issueID != "" {
				if _, err := cc.store.GetIssue(cc.ctx, issueID); err != nil {
					return err
				}
			}

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

			if len(list) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "questions: no matches")
				return nil
			}

			answerKinds := map[string]string{}
			for _, q := range list {
				if q.AnsweredBy == nil || *q.AnsweredBy == "" {
					continue
				}
				if _, seen := answerKinds[*q.AnsweredBy]; seen {
					continue
				}
				a, err := cc.store.GetStatement(cc.ctx, *q.AnsweredBy)
				if err != nil {
					continue
				}
				answerKinds[*q.AnsweredBy] = a.Kind
			}

			for _, q := range list {
				issueStr := "project"
				if q.IssueID != nil && *q.IssueID != "" {
					issueStr = *q.IssueID
				}
				line := fmt.Sprintf("%s  [%s]  %s  %s", q.ID, issueStr, q.CreatedAt.Format("2006-01-02"), previewText(q.Text))
				if q.AnsweredBy != nil && *q.AnsweredBy != "" {
					kind := answerKinds[*q.AnsweredBy]
					if kind == "" {
						kind = "statement"
					}
					line += fmt.Sprintf("  %s: %s", kind, *q.AnsweredBy)
				}
				if note := questionExpiryNote(q, time.Now().UTC(), staleDays); note != "" {
					marker, command := expiryNoteParts(note)
					fmt.Fprintln(cmd.OutOrStdout(), line+"  "+marker)
					fmt.Fprintln(cmd.OutOrStdout(), "        "+command)
					continue
				}
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&statusFlag, "status", "", "list questions with this status instead of active (answered|retracted|superseded)")
	cmd.Flags().IntVar(&staleDays, "stale-days", defaultStaleDays, "mark a question idle more than N days possibly moot, with its close command prepared (0 disables)")
	return cmd
}
