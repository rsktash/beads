package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/store"
)

// sessionTimeFormat is the stamp every checklist row carries.
const sessionTimeFormat = "2006-01-02 15:04"

func newSessionCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "session",
		Short: "Session-scoped checklists",
	}
	root.AddCommand(newSessionCloseCmd())
	return root
}

func newSessionCloseCmd() *cobra.Command {
	var sessionFlag string
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Print what this session left untyped",
		Long: `Print what this session left untyped: owner decisions with no ruling, open
questions it touched, and beads it claimed but did not close.

The session id comes from --session, else $CLAUDE_SESSION_ID, else $BD_SESSION_ID.

Each row is followed by the exact command that would resolve it, with the parts
only a person can supply left as literal <text>, <slug> and <why> placeholders:
the report is a prompt for a human, not an executor contract, and pre-filling a
reason is how a wrong reason gets committed.

bd session close writes nothing — it is a read, and the acts it names stay owner
or coordinator acts. Exit code is 0 even when items are found; this is a
checklist, not a gate.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			session := resolveSessionID(sessionFlag)
			if session == "" {
				return fmt.Errorf("no session id — pass --session or set CLAUDE_SESSION_ID")
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			rep, err := cc.store.SessionClose(cc.ctx, session)
			if err != nil {
				return err
			}

			writeSessionCloseReport(cmd.OutOrStdout(), rep)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionFlag, "session", "", "session id to close (defaults to $CLAUDE_SESSION_ID, then $BD_SESSION_ID)")
	return cmd
}

// resolveSessionID takes the flag first, then the two session environment
// variables in order.
func resolveSessionID(flag string) string {
	for _, v := range []string{flag, os.Getenv("CLAUDE_SESSION_ID"), os.Getenv("BD_SESSION_ID")} {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func writeSessionCloseReport(w io.Writer, rep store.SessionCloseReport) {
	fmt.Fprintf(w, "SESSION %s  closing\n", rep.SessionID)

	sessionSection(w, "OWNER DECISIONS WITH NO RULING", len(rep.OwnerDecisions), func() {
		for _, d := range rep.OwnerDecisions {
			// The author column is the resolved role, and every listed row is
			// the owner by construction of the section.
			fmt.Fprintf(w, "  %s  %s  owner  %s\n", d.CommentID, d.CreatedAt.Format(sessionTimeFormat), previewText(d.Text))
			fmt.Fprintf(w, "          bd ruling add %s \"<text>\" --topic <slug>\n", d.IssueID)
		}
	})

	sessionSection(w, "OPEN QUESTIONS TOUCHED", len(rep.OpenQuestions), func() {
		for _, q := range rep.OpenQuestions {
			author := q.Author
			if author == "" {
				author = actorWord(q.FiledBy)
			}
			fmt.Fprintf(w, "  %s  %s  %s  %s\n", q.ID, q.CreatedAt.Format(sessionTimeFormat), author, previewText(q.Text))
			fmt.Fprintf(w, "        bd question close %s --reason moot --note \"<why>\"\n", q.ID)
		}
	})

	sessionSection(w, "CLAIMED BUT NOT CLOSED", len(rep.ClaimedNotClosed), func() {
		for _, c := range rep.ClaimedNotClosed {
			fmt.Fprintf(w, "  %s  [%s]  assignee %s\n", c.IssueID, c.Status, c.Assignee)
			fmt.Fprintf(w, "        bd close %s --reason \"<what landed>\"\n", c.IssueID)
		}
	})
}

// sessionSection prints a section header and its rows. An empty section still
// prints its header and (none): a missing section reads as "not checked".
func sessionSection(w io.Writer, title string, n int, rows func()) {
	fmt.Fprintf(w, "\n%s (%d)\n", title, n)
	if n == 0 {
		fmt.Fprintln(w, "  (none)")
		return
	}
	rows()
}
