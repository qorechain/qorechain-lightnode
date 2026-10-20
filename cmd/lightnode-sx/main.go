package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/qorechain/qorechain-lightnode/internal/cli"
	"github.com/qorechain/qorechain-lightnode/internal/daemon"
	"github.com/qorechain/qorechain-lightnode/internal/dashboard"
)

const version = "3.1.10"

func main() {
	root := &cobra.Command{
		Use:   "lightnode-sx",
		Short: "QoreChain SX Light Node",
		Long:  "QoreChain SX Light Node daemon and management CLI.",
	}

	cli.Binary, cli.NodeType, cli.Version = "lightnode-sx", "sx", version
	cli.BindRootFlags(root)

	root.AddCommand(startCmd(), versionCmd())
	root.AddCommand(cli.Commands()...)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// startCmd runs the daemon until interrupted.
//
// On first launch (no config file present) it bails out with a friendly
// pointer to the onboarding wizard instead of trying to run with empty
// defaults. Operators who want to script around this can pass
// --skip-onboarding-check.
func startCmd() *cobra.Command {
	var skipOnboardingCheck bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the SX light node daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !skipOnboardingCheck {
				if _, err := os.Stat(cli.CfgFile); errors.Is(err, os.ErrNotExist) {
					fmt.Fprintf(os.Stderr, "No config file found at %s.\n\n", cli.CfgFile)
					fmt.Fprintf(os.Stderr, "Run 'lightnode-sx onboard' to set up the node interactively\n")
					fmt.Fprintf(os.Stderr, "(PQC self-test + chain endpoint + private key).\n\n")
					fmt.Fprintf(os.Stderr, "Or pass --skip-onboarding-check to start with defaults\n")
					fmt.Fprintf(os.Stderr, "(local-only mode — no chain RPC connection).\n")
					return fmt.Errorf("config file missing — onboarding required")
				}
			}

			cfg, err := cli.LoadConfig()
			if err != nil {
				return err
			}
			d, err := daemon.New(cfg)
			if err != nil {
				return fmt.Errorf("initializing daemon: %w", err)
			}
			defer d.Close()

			fmt.Fprintf(os.Stderr, "QoreChain SX Light Node v%s starting...\n", version)
			if cfg.RPCAddr == "" {
				fmt.Fprintf(os.Stderr, "Running in LOCAL-ONLY mode (no chain RPC configured).\n")
				fmt.Fprintf(os.Stderr, "Re-run 'lightnode-sx onboard' to connect to a chain.\n")
			}

			// The dashboard API, opt-in with [dashboard] enabled = true. An SX
			// node is the one that registers and earns, and QoreX pairs with a
			// node over this API ("the SX dashboard bind address", in its own
			// pairing record) - but until 3.1.6 only the UX binary served it, so
			// the nodes worth watching were the ones QoreX could not pair with.
			// Off by default: a server has no reason to open a port it was not
			// asked to, and an operator who turns it on beyond loopback gets the
			// same exposure notice, and the same token, as the UX build.
			ctx := context.Background()
			if cfg.Dashboard.Enabled {
				bind := cfg.Dashboard.BindAddr
				if bind == "" {
					bind = "127.0.0.1:8420"
				}
				api := dashboard.NewAPI(d.Chain(), d.Store(), d.LightClient(), d.Delegations(), d.Cfg(), d.Logger())
				srv := dashboard.New(bind, api, d.Logger())
				fmt.Fprintf(os.Stderr, "Dashboard API: %s\n", dashboard.DisplayURL(bind))
				if warning := dashboard.ExposureWarningWithToken(bind, cfg.Dashboard.Token); warning != "" {
					fmt.Fprint(os.Stderr, warning)
				}
				go func() {
					if err := srv.Start(ctx); err != nil {
						d.Logger().Error("dashboard server stopped", "error", err)
					}
				}()
			}
			return d.Run(ctx)
		},
	}
	cmd.Flags().BoolVar(&skipOnboardingCheck, "skip-onboarding-check", false, "do not require config.toml at startup (allows local-only start)")
	return cmd
}

// versionCmd prints the binary version.
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("lightnode-sx v%s\n", version)
		},
	}
}
