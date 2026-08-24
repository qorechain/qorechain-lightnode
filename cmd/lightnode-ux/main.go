package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/daemon"
	"github.com/qorechain/qorechain-lightnode/internal/dashboard"
)

const version = "3.1.1"

var (
	cfgFile string
	homeDir string
)

func main() {
	root := &cobra.Command{
		Use:   "lightnode-ux",
		Short: "QoreChain UX Light Node",
		Long:  "QoreChain UX Light Node with embedded web dashboard.",
	}

	defaultHome := defaultHomeDir()
	root.PersistentFlags().StringVar(&cfgFile, "config", filepath.Join(defaultHome, "config.toml"), "path to config file")
	root.PersistentFlags().StringVar(&homeDir, "home", defaultHome, "home directory for data and keys")

	root.AddCommand(
		startCmd(),
		versionCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func defaultHomeDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".qorechain-lightnode")
}

func loadConfig() config.Config {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		cfg = config.DefaultConfig()
	}
	if homeDir != "" {
		cfg.DataDir = homeDir
	}
	// UX edition: force dashboard and node type
	cfg.NodeType = "ux"
	cfg.Dashboard.Enabled = true
	if cfg.Dashboard.BindAddr == "" {
		// Loopback by default; see config.DefaultConfig for why.
		cfg.Dashboard.BindAddr = "127.0.0.1:8420"
	}
	return cfg
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
				if _, err := os.Stat(cfgFile); errors.Is(err, os.ErrNotExist) {
					fmt.Fprintf(os.Stderr, "No config file found at %s.\n\n", cfgFile)
					fmt.Fprintf(os.Stderr, "Run 'lightnode-ux onboard' to set up the node interactively\n")
					fmt.Fprintf(os.Stderr, "Or pass --skip-onboarding-check to start with defaults.\n")
					return fmt.Errorf("config file missing — onboarding required")
				}
			}
			cfg := loadConfig()
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
			if warning := dashboard.ExposureWarning(cfg.Dashboard.BindAddr); warning != "" {
				fmt.Fprint(os.Stderr, warning)
			}

			// Start dashboard server in background
			errCh := make(chan error, 1)
			go func() {
				errCh <- srv.Start(ctx)
			}()

			// Run daemon (blocks until signal)
			if err := d.Run(ctx); err != nil {
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
