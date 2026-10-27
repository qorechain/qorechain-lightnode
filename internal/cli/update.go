package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/qorechain/qorechain-lightnode/internal/update"
)

// updateCmd checks for a newer release and, unless --check, installs it now.
// A running node also does this on its own every few hours ([update] mode).
func updateCmd() *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for a newer light node release and install it",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			network := update.Network(cfg.ChainID)
			if network == "" {
				return fmt.Errorf("chain %s has no release channel", cfg.ChainID)
			}
			base := os.Getenv("QORE_LIGHTNODE_UPDATE_BASE")
			if base == "" {
				base = update.DefaultBase
			}
			u := &update.Updater{Base: base, Network: network, Edition: NodeType, Current: Version}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			m, err := u.Check(ctx)
			if err != nil {
				return err
			}
			if !update.Newer(m.Version, Version) {
				fmt.Printf("%s v%s is the latest %s release.\n", Binary, Version, network)
				return nil
			}
			fmt.Printf("New release: v%s (this is v%s).\n", m.Version, Version)
			if m.Notes != "" {
				fmt.Println(m.Notes)
			}
			if update.Newer(m.MinCompatible, Version) {
				fmt.Printf("v%s is below the minimum supported version v%s: update now.\n", Version, m.MinCompatible)
			}
			if checkOnly {
				return nil
			}
			exe, err := update.Executable()
			if err != nil {
				return err
			}
			if ok, why := update.CanReplace(exe); !ok {
				return fmt.Errorf("cannot update this binary in place (%s)", why)
			}
			staged, err := u.Download(ctx, m.Version, exe)
			if err != nil {
				return err
			}
			if err := update.Swap(exe, staged); err != nil {
				return err
			}
			fmt.Printf("Installed v%s at %s (signature, checksum and self-test passed); the previous binary is %s.old.\n", m.Version, exe, exe)
			fmt.Println("Restart the node (or its service) to run it. A running node with [update] mode = \"auto\" does this by itself.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only report whether a newer release exists")
	return cmd
}
