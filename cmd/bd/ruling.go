package main

import (
	"errors"

	"github.com/spf13/cobra"
)

// errRulingsClosed refuses every new ruling, whoever the actor: the rule source
// is the AGENTS.md of each folder.
var errRulingsClosed = errors.New("rulings are legacy: no ruling is added. Propose the rule as an AGENTS.md edit for the owner; an answered question closes with bd question close <Q-id> --commit <sha>")

func newRulingCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ruling",
		Short: "Legacy rulings: bind only where no AGENTS.md covers the code",
		Long: `Rulings are legacy. The rule source is the AGENTS.md of each folder. A ruling
binds only where no AGENTS.md covers the code yet (a project not yet migrated, or a
module whose AGENTS.md has not landed); elsewhere it is raw material for extraction.
No ruling is added: ruling add is refused. ruling retire runs only from the owner's
interactive terminal.`,
	}
	root.AddCommand(newRulingAddCmd())
	root.AddCommand(newRulingRetireCmd())
	root.AddCommand(newRulingListCmd())
	return root
}

// newRulingListCmd is `bd ruling list`, a sibling alias for `bd rulings`
// (registered separately, not via cobra's Aliases, because the alias sits
// under a different parent command). Same flags, same runRulings body, so
// output is byte-identical for the same arguments.
func newRulingListCmd() *cobra.Command {
	var scope string
	var grep string
	cmd := &cobra.Command{
		Use:   "list [issue-id]",
		Short: "List legacy rulings (alias for `bd rulings`)",
		Long:  "List active rulings. Without an issue id, lists every active ruling in the project newest first. With an issue id, lists what that bead is bound by via the inheritance resolver (same set the contract shows). Use --scope project to list only project-scoped rulings (issue_id IS NULL). Use --grep <kw> to substring-match across every scope (case-insensitive, over law+text); it cannot be combined with an issue id.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRulings(cmd, args, scope, grep)
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "filter scope: project (only project-scoped rulings)")
	cmd.Flags().StringVar(&grep, "grep", "", "case-insensitive substring match over law+text, across every scope")
	return cmd
}

// newRulingAddCmd refuses every invocation. Flag parsing is off so any flag a
// caller still passes reaches the refusal instead of an unknown-flag error.
func newRulingAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "add",
		Short:              "Refused for every actor: rulings are legacy",
		Long:               "Refused for every actor: rulings are legacy. Propose the rule as an AGENTS.md edit in the folder it governs; the owner approves it. A question the owner answered closes with bd question close <Q-id> --commit <sha>.",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errRulingsClosed
		},
	}
}
