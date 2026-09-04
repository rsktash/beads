package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// statementIDPrefixes are the citable statement prefixes. An id carrying one of
// them addresses the statements table; anything else is a comment uuid.
var statementIDPrefixes = []string{"R-", "Q-", "F-"}

func isStatementID(id string) bool {
	for _, p := range statementIDPrefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

func newAnnotateCmd() *cobra.Command {
	var sessionID, msgID, toolUseID, transcript string
	cmd := &cobra.Command{
		Use:   "annotate <id>",
		Short: "Write a session/message/tool-call pointer onto a statement or comment",
		Long: `Write the provenance pointer onto a statement (R-/Q-/F-) or a comment uuid.

The pointer is a locator, not authority: annotating twice overwrites, and no
history is kept. Read it back with bd source <id>.

--transcript is the transcript file the session actually wrote in; bd source
prefers it over the cwd-derived path, so a pointer written from another
project directory still resolves. Omitting the flag on a re-annotate clears
the stored path.

Open to every actor including executors — the SessionEnd hook runs it, not a
person, so there is no actor gate.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			session := strings.TrimSpace(sessionID)
			msg := strings.TrimSpace(msgID)
			tool := strings.TrimSpace(toolUseID)
			transcriptPath := strings.TrimSpace(transcript)
			if session == "" || msg == "" || tool == "" {
				return fmt.Errorf("--session, --msg and --tool are all required")
			}

			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("id is required")
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			if isStatementID(id) {
				err = cc.store.AnnotateStatement(cc.ctx, id, session, msg, tool, transcriptPath)
			} else {
				err = cc.store.AnnotateComment(cc.ctx, id, session, msg, tool, transcriptPath)
			}
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "session id the write happened in (required)")
	cmd.Flags().StringVar(&msgID, "msg", "", "uuid of the transcript record for the write (required)")
	cmd.Flags().StringVar(&toolUseID, "tool", "", "tool_use_id of the call that wrote the record (required)")
	cmd.Flags().StringVar(&transcript, "transcript", "", "path of the transcript file the session wrote in (optional; omitted clears)")
	return cmd
}
