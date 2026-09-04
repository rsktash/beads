package main

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/store"
)

// beadIDRE is the shape of a bead id. `bd areas resolve` treats its argument
// as a bead only when it matches this and the store finds it; every seeded
// glob carries a '/', which the character class excludes, so no path is
// mistaken for a bead.
var beadIDRE = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9.]+$`)

func newAreasCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "areas",
		Short: "Show and maintain the workspace and concern vocabularies",
		Long: `Print the area vocabulary: the workspace roots and the concerns, with the
number of open beads and distinct topics resolving to each.

Subcommands add, rename and merge maintain the vocabulary; resolve reports the
areas a path or a bead binds.`,
		Args: cobra.NoArgs,
		RunE: runAreasList,
	}
	root.AddCommand(newAreasAddCmd())
	root.AddCommand(newAreasResolveCmd())
	root.AddCommand(newAreasRenameCmd())
	root.AddCommand(newAreasMergeCmd())
	return root
}

func runAreasList(cmd *cobra.Command, _ []string) error {
	cc, err := openStore(cmd)
	if err != nil {
		return err
	}
	defer cc.store.Close()

	areas, err := cc.store.ListAreas(cc.ctx)
	if err != nil {
		return err
	}
	writeAreaTable(cmd.OutOrStdout(), areas)
	return nil
}

// areaSecondColumn is the root for a workspace and the pattern count for a
// concern — the same column, read differently per kind.
func areaSecondColumn(a store.Area) string {
	if a.Kind == store.AreaWorkspace {
		return a.Root
	}
	n := len(a.Patterns())
	if n == 1 {
		return "1 pattern"
	}
	return fmt.Sprintf("%d patterns", n)
}

// writeAreaTable prints one line per vocabulary entry, columns padded to the
// widest entry so the whole listing reads as a table.
func writeAreaTable(w io.Writer, areas store.Areas) {
	kindW, nameW, colW, openW := 0, 0, 0, 1
	for _, a := range areas {
		kindW = max(kindW, len(a.Kind))
		nameW = max(nameW, len(a.Name))
		colW = max(colW, len(areaSecondColumn(a)))
		openW = max(openW, len(fmt.Sprint(a.Open)))
	}
	for _, a := range areas {
		fmt.Fprintf(w, "%-*s  %-*s %-*s  open %*d  topics %d\n",
			kindW, a.Kind, nameW, a.Name, colW, areaSecondColumn(a), openW, a.Open, a.Topics)
	}
}

func newAreasAddCmd() *cobra.Command {
	var root, paths string
	cmd := &cobra.Command{
		Use:   "add <workspace|concern> <name>",
		Short: "Add a workspace root or a concern (adding a concern is actor-gated)",
		Long: `Add one vocabulary entry.

  bd areas add workspace <name> --root <path>
  bd areas add concern <name> --paths "<glob>,<glob>"

Actor gating: adding a concern is a decision, so BD_ACTOR=executor is refused.
Adding a workspace is open to every actor — a new workspace directory is a fact
about the repo, not a decision.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := strings.TrimSpace(args[0])
			name := strings.TrimSpace(args[1])
			switch kind {
			case store.AreaWorkspace:
				if strings.TrimSpace(root) == "" {
					return fmt.Errorf("--root is required for a workspace")
				}
				root = normalizeAreaRoot(root)
				paths = ""
			case store.AreaConcern:
				identity, isExecutor := resolveActor()
				if isExecutor {
					return executorRefusal(identity)
				}
				if strings.TrimSpace(paths) == "" {
					return fmt.Errorf("--paths is required for a concern")
				}
				root = ""
			default:
				return fmt.Errorf("unknown area kind %q (want workspace or concern)", kind)
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			if err := cc.store.AddArea(cc.ctx, kind, name, root, paths); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added %s %s\n", kind, name)
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", "", "workspace directory root (a trailing / is added when missing)")
	cmd.Flags().StringVar(&paths, "paths", "", "concern globs, comma-separated (** spans segments)")
	return cmd
}

// normalizeAreaRoot makes a workspace root prefix-safe: without the trailing
// separator, root "mobile/cm" would also claim "mobile/cmx/a.ts".
func normalizeAreaRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" || strings.HasSuffix(root, "/") {
		return root
	}
	return root + "/"
}

func newAreasResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve <path | bead-id>",
		Short: "Report the areas a path or a bead binds",
		Long: `Resolve one argument to its areas. The argument is a bead id when it has the
shape of one and the store finds it; otherwise it is a path.

A bead resolves through the paths in its ## Files section. A bead with no such
section resolves to nothing and says so, exiting 0.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := strings.TrimSpace(args[0])
			if arg == "" {
				return fmt.Errorf("a path or a bead id is required")
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			out := cmd.OutOrStdout()
			if beadIDRE.MatchString(arg) {
				if _, err := cc.store.GetIssue(cc.ctx, arg); err == nil {
					ws, cs, err := cc.store.ResolveIssue(cc.ctx, arg)
					if err != nil {
						return err
					}
					if len(ws) == 0 && len(cs) == 0 {
						fmt.Fprintf(cmd.ErrOrStderr(), "%s: no ## Files section — no workspace and no concern\n", arg)
						return nil
					}
					writeAreaResolution(out, ws, cs)
					return nil
				}
			}

			areas, err := cc.store.ListAreas(cc.ctx)
			if err != nil {
				return err
			}
			w, cs := areas.ResolvePath(arg)
			var ws []string
			if w != "" {
				ws = []string{w}
			}
			writeAreaResolution(out, ws, cs)
			return nil
		},
	}
	return cmd
}

func writeAreaResolution(w io.Writer, workspaces, concerns []string) {
	fmt.Fprintf(w, "workspace: %s\n", joinAreaNames(workspaces))
	fmt.Fprintf(w, "concerns: %s\n", joinAreaNames(concerns))
}

func joinAreaNames(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

func newAreasRenameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <workspace|concern> <old> <new>",
		Short: "Rename a vocabulary entry (actor-gated)",
		Long: `Rename one vocabulary entry. Every statement naming it is rewritten in the
same transaction, so no statement is left naming an entry that is gone.

Actor gating: BD_ACTOR=executor is refused for both kinds.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				return executorRefusal(identity)
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			kind := strings.TrimSpace(args[0])
			if err := cc.store.RenameArea(cc.ctx, kind, args[1], args[2]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "renamed %s %s -> %s\n", kind, strings.TrimSpace(args[1]), strings.TrimSpace(args[2]))
			return nil
		},
	}
	return cmd
}

func newAreasMergeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge <workspace|concern> <from> <into>",
		Short: "Fold one vocabulary entry into another (actor-gated)",
		Long: `Merge one vocabulary entry into another: every statement naming <from> is
rewritten to <into> and the <from> entry is dropped, in one transaction.

Actor gating: BD_ACTOR=executor is refused for both kinds.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				return executorRefusal(identity)
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			kind := strings.TrimSpace(args[0])
			if err := cc.store.MergeAreas(cc.ctx, kind, args[1], args[2]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "merged %s %s into %s\n", kind, strings.TrimSpace(args[1]), strings.TrimSpace(args[2]))
			return nil
		},
	}
	return cmd
}
