package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/qorechain/qorechain-lightnode/internal/cli"
	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/daemon"
	"github.com/qorechain/qorechain-lightnode/internal/dashboard"
	"github.com/qorechain/qorechain-lightnode/internal/update"
)

const version = "3.1.12"

func main() {
	root := &cobra.Command{
		Use:   "lightnode-ux",
		Short: "QoreChain UX Light Node",
		Long:  "QoreChain UX Light Node with embedded web dashboard.",
	}

	cli.Binary, cli.NodeType, cli.Version = "lightnode-ux", "ux", version
	cli.BindRootFlags(root)

	root.AddCommand(startCmd(), versionCmd())
	root.AddCommand(cli.Commands()...)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// loadConfig is the shared loader with the UX edition's fixed settings. A
// config that exists but cannot be read is an error here too: it used to fall
// back to defaults silently, running the desktop node on the testnet.
func loadConfig() (config.Config, error) {
	cfg, err := cli.LoadConfig()
	if err != nil {
		return cfg, err
	}
	// UX edition: force dashboard and node type
	cfg.NodeType = "ux"
	cfg.Dashboard.Enabled = true
	if cfg.Dashboard.BindAddr == "" {
		// Loopback by default; see config.DefaultConfig for why.
		cfg.Dashboard.BindAddr = "127.0.0.1:8420"
	}
	return cfg, nil
}

// startCmd runs the daemon + dashboard server until interrupted.
//
// Like the SX daemon, on first launch (no config file) it bails out with a
// pointer to the onboarding wizard unless --skip-onboarding-check is passed,
// giving the two binaries a consistent start interface.
func startCmd() *cobra.Command {
	var skipOnboardingCheck bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the UX light node with embedded dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !skipOnboardingCheck {
				if _, err := os.Stat(cli.CfgFile); errors.Is(err, os.ErrNotExist) {
					fmt.Fprintf(os.Stderr, "No config file found at %s.\n\n", cli.CfgFile)
					fmt.Fprintf(os.Stderr, "Run 'lightnode-ux onboard' to set up the node interactively\n")
					fmt.Fprintf(os.Stderr, "Or pass --skip-onboarding-check to start with defaults.\n")
					return fmt.Errorf("config file missing — onboarding required")
				}
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			d, err := daemon.New(cfg)
			if err != nil {
				return fmt.Errorf("initializing daemon: %w", err)
			}
			defer d.Close()

			// Create dashboard API and server
			api := dashboard.NewAPI(
				d.Chain(),
				d.Store(),
				d.LightClient(),
				d.Delegations(),
				d.Cfg(),
				d.Logger(),
			)
			srv := dashboard.New(cfg.Dashboard.BindAddr, api, d.Logger())

			ctx := context.Background()

			fmt.Fprintf(os.Stderr, "QoreChain UX Light Node v%s starting...\n", version)
			fmt.Fprintf(os.Stderr, "Dashboard: %s\n", dashboard.DisplayURL(cfg.Dashboard.BindAddr))
			if warning := dashboard.ExposureWarningWithToken(cfg.Dashboard.BindAddr, cfg.Dashboard.Token); warning != "" {
				fmt.Fprint(os.Stderr, warning)
			}

			// Start dashboard server in background
			errCh := make(chan error, 1)
			go func() {
				errCh <- srv.Start(ctx)
			}()

			// Run daemon (blocks until signal)
			if err := restartIfUpdated(d, d.Run(ctx)); err != nil {
				return err
			}

			// Check if dashboard had an error
			select {
			case err := <-errCh:
				return err
			default:
				return nil
			}
		},
	}
	cmd.Flags().BoolVar(&skipOnboardingCheck, "skip-onboarding-check", false, "do not require config.toml at startup (allows local-only start)")
	return cmd
}

// restartIfUpdated execs the new binary when Run stopped for an update. On
// success it does not return.
func restartIfUpdated(d *daemon.Daemon, err error) error {
	var re *daemon.RestartError
	if !errors.As(err, &re) {
		return err
	}
	d.Close()
	return update.Restart(re.Exe)
}

// versionCmd prints the binary version.
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("lightnode-ux v%s\n", version)
		},
	}
}
