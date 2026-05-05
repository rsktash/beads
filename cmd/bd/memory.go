package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// `bd remember <text>` — append agent memory (key/value note).
// `bd memories list [--key <k>]` — read back.
// `bd memories rm <id>` — delete.
//
// Storage is the `memories` table in the same DB; rows are visible to every
// agent connecting to the project. Useful as a structured replacement for
// MEMORY.md when you want server-side recall across sessions.
func newRememberCmd() *cobra.Command {
	var key string
	cmd := &cobra.Command{
		Use:   "remember <text>",
		Short: "Save a project memory (agent recall across sessions)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			text := strings.Join(args, " ")
			if err := cc.store.AddMemory(cc.ctx, key, text, assigneeFromEnv()); err != nil {
				return err
			}
			fmt.Printf("remembered (key=%q)\n", key)
			return nil
		},
	}
	cmd.Flags().StringVarP(&key, "key", "k", "", "optional grouping key")
	return cmd
}

func newMemoriesCmd() *cobra.Command {
	root := &cobra.Command{Use: "memories", Short: "List/delete saved memories"}

	var listKey string
	listC := &cobra.Command{
		Use: "list", Short: "List memories",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			ms, err := cc.store.ListMemories(cc.ctx, listKey)
			if err != nil {
				return err
			}
			if cc.json {
				return writeJSON(ms)
			}
			for _, m := range ms {
				keyPart := ""
				if m.Key != "" {
					keyPart = "[" + m.Key + "] "
				}
				fmt.Printf("%d  %s  %s%s\n",
					m.ID, m.CreatedAt.Format("2006-01-02 15:04"), keyPart, m.Value)
			}
			return nil
		},
	}
	listC.Flags().StringVarP(&listKey, "key", "k", "", "filter by key")

	rmC := &cobra.Command{
		Use: "rm <id>", Short: "Delete a memory by id", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			n, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("id must be integer: %w", err)
			}
			return cc.store.DeleteMemory(cc.ctx, n)
		},
	}

	root.AddCommand(listC, rmC)
	return root
}
