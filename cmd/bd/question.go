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
	var rulingID string
	cmd := &cobra.Command{
		Use:   "answer <question-id>",
		Short: "Answer a question with a ruling",
		Long:  `Mark a question as answered by a ruling. Sets the question's answered_by to the ruling id and its status to answered.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			questionID := strings.TrimSpace(args[0])
			if questionID == "" {
				return fmt.Errorf("question id is required")
			}
			rid := strings.TrimSpace(rulingID)
			if rid == "" {
				return fmt.Errorf("--ruling is required")
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
			// Fetch ruling and validate.
			r, err := cc.store.GetStatement(cc.ctx, rid)
			if err != nil {
				return fmt.Errorf("ruling %s not found: %w", rid, err)
			}
			if r.Kind != "ruling" {
				return fmt.Errorf("statement %s is not a ruling (is %s)", rid, r.Kind)
			}

			// Perform update.
			if err := cc.store.SetAnsweredBy(cc.ctx, questionID, rid); err != nil {
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
	cmd.Flags().StringVar(&rulingID, "ruling", "", "ruling id that answers the question (required)")
	_ = cmd.MarkFlagRequired("ruling")
	return cmd
}
