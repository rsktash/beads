package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newFindingCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "finding",
		Aliases: []string{"findings"},
		Short:   "Manage findings (evidence-backed observations)",
	}
	root.AddCommand(newFindingAddCmd())
	root.AddCommand(newFindingCloseCmd())
	return root
}

func newFindingCloseCmd() *cobra.Command {
	var (
		rulingID string
		note     string
	)
	cmd := &cobra.Command{
		Use:   "close <finding-id> --ruling <id> [--note <why>]",
		Short: "Close a finding citing the ruling that settled it (actor-gated: BD_ACTOR=executor is refused)",
		Long: `Close a finding a ruling has settled. Its status becomes answered, answered_by
names the ruling, and closed_by records who closed it; filed_by and evidence
are untouched. A closed finding drops out of the FINDINGS block and out of
active listings, and stays readable via bd get and
bd statements list --status answered.

--ruling is required and must name an existing ruling. --note is optional and
records why, in words.

Refused for a finding that is already closed.

Actor gating: BD_ACTOR=executor cannot close findings; an executor files a finding or question instead. BD_ACTOR=coordinator or unset (owner) is allowed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				return executorRefusal(identity)
			}

			findingID := strings.TrimSpace(args[0])
			if findingID == "" {
				return fmt.Errorf("finding id is required")
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

			f, err := cc.store.GetStatement(cc.ctx, findingID)
			if err != nil {
				return fmt.Errorf("finding %s not found: %w", findingID, err)
			}
			if f.Kind != "finding" {
				return fmt.Errorf("statement %s is not a finding (is %s)", findingID, f.Kind)
			}
			if f.Status != "active" {
				return fmt.Errorf("finding %s is not active (is %s); it is already closed", findingID, f.Status)
			}
			r, err := cc.store.GetStatement(cc.ctx, rid)
			if err != nil {
				return fmt.Errorf("ruling %s not found: %w", rid, err)
			}
			if r.Kind != "ruling" {
				return fmt.Errorf("statement %s is not a ruling (is %s)", rid, r.Kind)
			}

			if err := cc.store.CloseFinding(cc.ctx, findingID, rid, identity, strings.TrimSpace(note)); err != nil {
				return err
			}

			updated, err := cc.store.GetStatement(cc.ctx, findingID)
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
	cmd.Flags().StringVar(&rulingID, "ruling", "", "ruling id that settled the finding (required)")
	cmd.Flags().StringVar(&note, "note", "", "why the ruling settles it, in words (optional)")
	return cmd
}

func newFindingAddCmd() *cobra.Command {
	var evidence string
	var topic string
	var source string
	cmd := &cobra.Command{
		Use:   "add <issue-id> <text> --topic <slug>",
		Short: "File a finding (open to all actors)",
		Long: `File a finding against a bead. Requires an issue id, text and a topic slug. Optional --evidence stores provenance verbatim. Open to all actors including executors.

--topic is required: a finding nobody can find under the topic it bears on is a
finding nobody reads. Without it the command prints the catalogue for the
bead's areas and refuses.

Optional --source <issue-id> records the bead the filer was working when the
finding was hit; it renders as from: on the finding's line and in a FINDINGS
FILED FROM HERE section on the source bead's own show.`,
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
			sourceID := strings.TrimSpace(source)
			if sourceID != "" {
				if sourceID == issueID {
					return fmt.Errorf("--source names the bead the finding is already on")
				}
				if _, err := cc.store.GetIssue(cc.ctx, sourceID); err != nil {
					if errors.Is(err, store.ErrNotFound) {
						return fmt.Errorf("--source %s: no such issue", sourceID)
					}
					return err
				}
			}
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
			if sourceID != "" {
				st.SourceIssueID = &sourceID
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
	cmd.Flags().StringVar(&source, "source", "", "bead the filer was working when the finding was hit (optional; renders as from:)")
	return cmd
}
