package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
)

func newQuestionCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "question",
		Aliases: []string{"questions"},
		Short:   "Manage questions (execution blockers)",
	}
	root.AddCommand(newQuestionAddCmd())
	root.AddCommand(newQuestionAnswerCmd())
	root.AddCommand(newQuestionCloseCmd())
	return root
}

func newQuestionAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <issue-id> <text>",
		Short: "File a question (open to all actors)",
		Long:  `File a question against a bead. Requires an issue id and text. Open to all actors including executors.`,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, _ := resolveActor()
			issueIDStr := strings.TrimSpace(args[0])
			if issueIDStr == "" {
				return fmt.Errorf("issue id is required")
			}
			text := strings.TrimSpace(args[1])
			if text == "" {
				return fmt.Errorf("text is required")
			}
			issueID := issueIDStr
			st := &beads.Statement{
				Kind:    "question",
				IssueID: &issueID,
				Text:    text,
				FiledBy: identity,
				Status:  "active",
				Scope:   "inherit",
			}
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			if err := cc.store.CreateStatement(cc.ctx, st); err != nil {
				return err
			}
			if cc.json {
				return writeJSONTo(cmd.OutOrStdout(), st)
			}
			fmt.Fprintln(cmd.OutOrStdout(), st.ID)
			return nil
		},
	}
	return cmd
}

func newQuestionAnswerCmd() *cobra.Command {
	var (
		rulingID  string
		findingID string
	)
	cmd := &cobra.Command{
		Use:   "answer <question-id> --ruling <id> | --finding <id>",
		Short: "Answer a question with a ruling or a finding (actor-gated: BD_ACTOR=executor is refused)",
		Long: `Mark a question as answered. Sets the question's answered_by to the named statement and its status to answered.

--ruling names a ruling: a judgment settled the question.
--finding names a finding: evidence settled it, and no ruling is minted. Use this
when nobody decided anything and the answer is what the evidence already says.

The two flags are mutually exclusive, and each is checked against the kind it
names, so an evidence-settled question stays distinguishable from a ruled one.

Actor gating: BD_ACTOR=executor cannot answer questions; an executor files the finding and leaves the question open. BD_ACTOR=coordinator or unset (owner) is allowed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				return executorRefusal(identity)
			}

			questionID := strings.TrimSpace(args[0])
			if questionID == "" {
				return fmt.Errorf("question id is required")
			}
			rid := strings.TrimSpace(rulingID)
			fid := strings.TrimSpace(findingID)
			if rid != "" && fid != "" {
				return fmt.Errorf("--ruling and --finding are mutually exclusive")
			}
			// answerID is the statement that settles the question; answerKind is the
			// kind the caller's flag claims it is, and the row must match it.
			answerID, answerKind := rid, "ruling"
			if fid != "" {
				answerID, answerKind = fid, "finding"
			}
			if answerID == "" {
				return fmt.Errorf("--ruling or --finding is required")
			}
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			// Fetch question and validate.
			q, err := cc.store.GetStatement(cc.ctx, questionID)
			if err != nil {
				return fmt.Errorf("question %s not found: %w", questionID, err)
			}
			if q.Kind != "question" {
				return fmt.Errorf("statement %s is not a question (is %s)", questionID, q.Kind)
			}
			if q.Status == "answered" || q.AnsweredBy != nil {
				answeredBy := ""
				if q.AnsweredBy != nil {
					answeredBy = *q.AnsweredBy
				}
				return fmt.Errorf("question %s already answered by %s", questionID, answeredBy)
			}
			// Fetch the answering statement and validate its kind against the flag.
			a, err := cc.store.GetStatement(cc.ctx, answerID)
			if err != nil {
				return fmt.Errorf("%s %s not found: %w", answerKind, answerID, err)
			}
			if a.Kind != answerKind {
				return fmt.Errorf("statement %s is not a %s (is %s)", answerID, answerKind, a.Kind)
			}

			// Perform update.
			if err := cc.store.SetAnsweredBy(cc.ctx, questionID, answerID); err != nil {
				return err
			}
			// Fetch updated question for output.
			updated, err := cc.store.GetStatement(cc.ctx, questionID)
			if err != nil {
				return err
			}
			if cc.json {
				return writeJSONTo(cmd.OutOrStdout(), updated)
			}
			fmt.Fprintln(cmd.OutOrStdout(), updated.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&rulingID, "ruling", "", "ruling id that answers the question (exclusive with --finding)")
	cmd.Flags().StringVar(&findingID, "finding", "", "finding id whose evidence answers the question (exclusive with --ruling)")
	return cmd
}
