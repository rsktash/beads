package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
)

func newFindingCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "finding",
		Aliases: []string{"findings"},
		Short:   "Manage findings (evidence-backed observations)",
	}
	root.AddCommand(newFindingAddCmd())
	return root
}

func newFindingAddCmd() *cobra.Command {
	var evidence string
	cmd := &cobra.Command{
		Use:   "add <issue-id> <text>",
		Short: "File a finding (open to all actors)",
		Long:  `File a finding against a bead. Requires an issue id and text. Optional --evidence stores provenance verbatim. Open to all actors including executors.`,
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
				Kind:    "finding",
				IssueID: &issueID,
				Text:    text,
				FiledBy: identity,
				Status:  "active",
				Scope:   "inherit",
				Evidence: evidence,
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
	cmd.Flags().StringVar(&evidence, "evidence", "", "evidence references (verbatim string)")
	return cmd
}
