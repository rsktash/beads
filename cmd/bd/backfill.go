package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// newStatementsCmd is the `bd statements` command tree.
func newStatementsCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "statements",
		Short: "Manage statements (candidate backfill)",
	}
	root.AddCommand(newStatementsBackfillCmd())
	return root
}

// isBackfillCandidate reports whether a comment body carries a legacy marker.
// Case-sensitive for shouted forms; coordinator form is via commentTag (case-insensitive, leading tag only).
func isBackfillCandidate(text string) bool {
	if strings.Contains(text, "OWNER RULING") {
		return true
	}
	if strings.Contains(text, "RULED:") {
		return true
	}
	if strings.Contains(text, "DEFERRED") {
		return true
	}
	if commentTag(text) == "coordinator" {
		return true
	}
	return false
}

func newStatementsBackfillCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Backfill candidate rulings from legacy comment markers",
		Long: `Scan every comment for legacy markers and file candidate rulings.

Markers (one candidate per matching comment, even if multiple markers match):

  - OWNER RULING  (case-sensitive substring)
  - RULED:        (case-sensitive substring)
  - DEFERRED      (case-sensitive substring)
  - leading [coordinator] tag (case-insensitive, reuse of commentTag)

Each emitted row has kind "ruling" and status "candidate", linked to the
source comment via source_comment_id. The command never promotes candidates
to active and never modifies source comments.

Idempotent: a comment that already has a candidate linked via source_comment_id
is skipped. --dry-run prints what would be emitted and writes nothing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			// Walk every comment. Prefer ListAllComments; fallback to ListIssues+ListComments if needed.
			comments, err := cc.store.ListAllComments(cc.ctx)
			if err != nil {
				return err
			}
			// Fallback: if ListAllComments returned empty but issues exist, walk issues (covers drivers where ListAllComments might not be implemented).
			if len(comments) == 0 {
				issues, lerr := cc.store.ListIssues(cc.ctx, store.ListFilter{})
				if lerr == nil && len(issues) > 0 {
					var viaIssues []beads.Comment
					for _, iss := range issues {
						cs, cerr := cc.store.ListComments(cc.ctx, iss.ID)
						if cerr != nil {
							return cerr
						}
						viaIssues = append(viaIssues, cs...)
					}
					if len(viaIssues) > 0 {
						comments = viaIssues
					}
				}
			}

			// Build set of already-linked source_comment_ids (any status, but at least candidate) to ensure idempotence.
			existingMap := make(map[string]bool)
			allStmts, err := cc.store.ListStatements(cc.ctx, store.StatementFilter{})
			if err != nil {
				return err
			}
			for _, s := range allStmts {
				if s.SourceCommentID != nil && *s.SourceCommentID != "" {
					existingMap[*s.SourceCommentID] = true
				}
			}

			filedBy, _ := resolveActor()

			// Collect matching comments not yet linked.
			var toCreate []beads.Comment
			for _, c := range comments {
				if !isBackfillCandidate(c.Text) {
					continue
				}
				if existingMap[c.ID] {
					continue
				}
				toCreate = append(toCreate, c)
			}

			// Determine JSON output flag via cc.json (derived from global flagJSON) and global fallback.
			isJSON := cc.json || flagJSON

			if dryRun {
				if isJSON {
					// Emit preview array without persisting.
					var preview []beads.Statement
					for _, c := range toCreate {
						// Need copy for pointer
						issueIDCopy := c.IssueID
						var iidPtr *string
						if issueIDCopy != "" {
							v := issueIDCopy
							iidPtr = &v
						}
						srcCopy := c.ID
						preview = append(preview, beads.Statement{
							Kind:            "ruling",
							IssueID:         iidPtr,
							Text:            c.Text,
							FiledBy:         filedBy,
							Status:          "candidate",
							Scope:           "inherit",
							SourceCommentID: &srcCopy,
						})
					}
					if preview == nil {
						preview = []beads.Statement{}
					}
					return writeJSONTo(cmd.OutOrStdout(), preview)
				}
				// Count on stdout, plus what would be emitted.
				fmt.Fprintf(cmd.OutOrStdout(), "would create %d candidates\n", len(toCreate))
				for _, c := range toCreate {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s  %s\n", c.ID, c.IssueID, c.Text)
				}
				return nil
			}

			var created []beads.Statement
			for _, c := range toCreate {
				issueIDCopy := c.IssueID
				var iidPtr *string
				if issueIDCopy != "" {
					v := issueIDCopy
					iidPtr = &v
				}
				srcCopy := c.ID
				st := &beads.Statement{
					Kind:            "ruling",
					IssueID:         iidPtr,
					Text:            c.Text,
					FiledBy:         filedBy,
					Status:          "candidate",
					Scope:           "inherit",
					SourceCommentID: &srcCopy,
				}
				if err := cc.store.CreateStatement(cc.ctx, st); err != nil {
					return err
				}
				created = append(created, *st)
			}

			if isJSON {
				if created == nil {
					created = []beads.Statement{}
				}
				return writeJSONTo(cmd.OutOrStdout(), created)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "backfilled %d candidates\n", len(created))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be emitted and write nothing")
	return cmd
}
