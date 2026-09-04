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
	var topic string
	cmd := &cobra.Command{
		Use:   "add <issue-id> <text> --topic <slug>",
		Short: "File a finding (open to all actors)",
		Long: `File a finding against a bead. Requires an issue id, text and a topic slug. Optional --evidence stores provenance verbatim. Open to all actors including executors.

--topic is required: a finding nobody can find under the topic it bears on is a
finding nobody reads. Without it the command prints the catalogue for the
bead's areas and refuses.`,
		Args: cobra.ExactArgs(2),
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
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			slug, workspace, concern, err := resolveStatementTopic(cc, cmd.ErrOrStderr(), issueID, topic)
			if err != nil {
				return err
			}
			st := &beads.Statement{
				Kind:      "finding",
				IssueID:   &issueID,
				Text:      text,
				FiledBy:   identity,
				Status:    "active",
				Scope:     "inherit",
				Evidence:  evidence,
				Topic:     slug,
				Workspace: workspace,
				Concern:   concern,
			}
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
	cmd.Flags().StringVar(&topic, "topic", "", "topic slug this finding belongs to (required; [a-z0-9-]{2,48})")
	return cmd
}
