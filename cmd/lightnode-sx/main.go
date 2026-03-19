package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/daemon"
	"github.com/qorechain/qorechain-lightnode/internal/keyring"
)

const version = "1.15.0"

var (
	cfgFile string
	homeDir string
)

func main() {
	root := &cobra.Command{
		Use:   "lightnode-sx",
		Short: "QoreChain SX Light Node",
		Long:  "QoreChain SX Light Node daemon and management CLI.",
	}

	defaultHome := defaultHomeDir()
	root.PersistentFlags().StringVar(&cfgFile, "config", filepath.Join(defaultHome, "config.toml"), "path to config file")
	root.PersistentFlags().StringVar(&homeDir, "home", defaultHome, "home directory for data and keys")

	root.AddCommand(
		startCmd(),
		statusCmd(),
		keysCmd(),
		registerCmd(),
		validatorsCmd(),
		delegationCmd(),
		rewardsCmd(),
		networkCmd(),
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

func loadConfig() (config.Config, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		// If config file not found, use defaults with home dir override
		cfg = config.DefaultConfig()
	}
	if homeDir != "" {
		cfg.DataDir = homeDir
	}
	return cfg, nil
}

// startCmd runs the daemon until interrupted.
func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the SX light node daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			d, err := daemon.New(cfg)
			if err != nil {
				return fmt.Errorf("initializing daemon: %w", err)
			}
			defer d.Close()

			fmt.Fprintf(os.Stderr, "QoreChain SX Light Node v%s starting...\n", version)
			return d.Run(context.Background())
		},
	}
}

// statusCmd prints node and light client status.
func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show node and light client sync status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			d, err := daemon.New(cfg)
			if err != nil {
				return err
			}
			defer d.Close()

			ctx := context.Background()
			status, err := d.Chain().NodeStatus(ctx)
			if err != nil {
				return fmt.Errorf("querying node status: %w", err)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "Chain ID:\t%s\n", status.Result.NodeInfo.Network)
			fmt.Fprintf(w, "Node Version:\t%s\n", status.Result.NodeInfo.Version)
			fmt.Fprintf(w, "Latest Height:\t%s\n", status.Result.SyncInfo.LatestBlockHeight)
			fmt.Fprintf(w, "Latest Time:\t%s\n", status.Result.SyncInfo.LatestBlockTime)
			fmt.Fprintf(w, "Catching Up:\t%v\n", status.Result.SyncInfo.CatchingUp)
			fmt.Fprintf(w, "LC Synced Height:\t%d\n", d.LightClient().LatestHeight())
			fmt.Fprintf(w, "LC Syncing:\t%v\n", d.LightClient().IsSyncing())
			w.Flush()
			return nil
		},
	}
}

// keysCmd provides key management subcommands.
func keysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage keyring",
	}
	cmd.AddCommand(keysCreateCmd(), keysListCmd(), keysImportCmd(), keysExportCmd())
	return cmd
}

func keysCreateCmd() *cobra.Command {
	var keyType string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
			if err != nil {
				return err
			}
			info, err := keys.Create(args[0], keyring.KeyType(keyType))
			if err != nil {
				return fmt.Errorf("creating key: %w", err)
			}
			fmt.Printf("Name:    %s\n", info.Name)
			fmt.Printf("Type:    %s\n", info.Type)
			fmt.Printf("Address: %s\n", info.Address)
			return nil
		},
	}
	cmd.Flags().StringVar(&keyType, "type", "secp256k1", "key type: secp256k1, ed25519, dilithium5")
	return cmd
}

func keysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
			if err != nil {
				return err
			}
			list, err := keys.List()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println("No keys found.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "NAME\tTYPE\tADDRESS\n")
			for _, k := range list {
				fmt.Fprintf(w, "%s\t%s\t%s\n", k.Name, k.Type, k.Address)
			}
			w.Flush()
			return nil
		},
	}
}

func keysImportCmd() *cobra.Command {
	var keyType string
	cmd := &cobra.Command{
		Use:   "import <name> <hex-privkey>",
		Short: "Import a private key",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
			if err != nil {
				return err
			}
			privBytes, err := hex.DecodeString(args[1])
			if err != nil {
				return fmt.Errorf("invalid hex key: %w", err)
			}
			info, err := keys.Import(args[0], keyring.KeyType(keyType), privBytes)
			if err != nil {
				return fmt.Errorf("importing key: %w", err)
			}
			fmt.Printf("Imported key: %s (%s)\n", info.Name, info.Address)
			return nil
		},
	}
	cmd.Flags().StringVar(&keyType, "type", "secp256k1", "key type: secp256k1, ed25519, dilithium5")
	return cmd
}

func keysExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a private key in hex",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
			if err != nil {
				return err
			}
			privBytes, err := keys.Export(args[0])
			if err != nil {
				return fmt.Errorf("exporting key: %w", err)
			}
			fmt.Println(hex.EncodeToString(privBytes))
			return nil
		},
	}
}

// registerCmd prints registration information.
func registerCmd() *cobra.Command {
	var nodeType, ver string
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Print light node registration info for chain submission",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
			if err != nil {
				return err
			}
			info, err := keys.Get(cfg.KeyName)
			if err != nil {
				return fmt.Errorf("operator key %q not found: %w", cfg.KeyName, err)
			}
			fmt.Println("Registration command:")
			fmt.Printf("  qorechaind tx lightnode register-node %s %s --from %s --chain-id %s\n",
				nodeType, ver, info.Address, cfg.ChainID)
			fmt.Println()
			fmt.Printf("Operator Address: %s\n", info.Address)
			fmt.Printf("Node Type:        %s\n", nodeType)
			fmt.Printf("Version:          %s\n", ver)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeType, "type", "sx", "node type: sx or ux")
	cmd.Flags().StringVar(&ver, "version", version, "node version")
	return cmd
}

// validatorsCmd queries and displays bonded validators.
func validatorsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validators",
		Short: "List bonded validators",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			d, err := daemon.New(cfg)
			if err != nil {
				return err
			}
			defer d.Close()

			vals, err := d.Chain().Validators(context.Background())
			if err != nil {
				return fmt.Errorf("querying validators: %w", err)
			}

			if len(vals) == 0 {
				fmt.Println("No validators found.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "OPERATOR\tSTATUS\tTOKENS\tJAILED\n")
			for _, v := range vals {
				fmt.Fprintf(w, "%s\t%s\t%s\t%v\n", v.OperatorAddress, v.Status, v.Tokens, v.Jailed)
			}
			w.Flush()
			return nil
		},
	}
}

// delegationCmd shows current delegations from local DB.
func delegationCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delegation",
		Short: "Show current delegations",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			d, err := daemon.New(cfg)
			if err != nil {
				return err
			}
			defer d.Close()

			delegations, err := d.Delegations().GetDelegations(context.Background())
			if err != nil {
				return fmt.Errorf("querying delegations: %w", err)
			}

			if len(delegations) == 0 {
				fmt.Println("No delegations found. Run the daemon to sync delegation state.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "VALIDATOR\tAMOUNT (uqor)\tUPDATED\n")
			for _, del := range delegations {
				fmt.Fprintf(w, "%s\t%s\t%s\n", del.Validator, del.Amount, del.UpdatedAt)
			}
			w.Flush()
			return nil
		},
	}
}

// rewardsCmd shows pending rewards from chain.
func rewardsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rewards",
		Short: "Show pending staking rewards",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			d, err := daemon.New(cfg)
			if err != nil {
				return err
			}
			defer d.Close()

			total, err := d.Delegations().GetTotalRewards(context.Background())
			if err != nil {
				return fmt.Errorf("querying rewards: %w", err)
			}
			fmt.Printf("Pending Rewards: %s uqor\n", total)
			return nil
		},
	}
}

// networkCmd shows network telemetry from local DB.
func networkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "network",
		Short: "Show network telemetry from local database",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			d, err := daemon.New(cfg)
			if err != nil {
				return err
			}
			defer d.Close()

			// Show recent synced headers as a quick network overview
			headers, err := d.LightClient().RecentHeaders(5)
			if err != nil {
				return fmt.Errorf("querying headers: %w", err)
			}

			fmt.Printf("Latest synced height: %d\n\n", d.LightClient().LatestHeight())
			if len(headers) > 0 {
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintf(w, "HEIGHT\tTIME\tHASH\n")
				for _, h := range headers {
					hashDisplay := h.Hash
					if len(hashDisplay) > 16 {
						hashDisplay = hashDisplay[:16] + "..."
					}
					fmt.Fprintf(w, "%d\t%s\t%s\n", h.Height, h.Time.Format("2006-01-02 15:04:05"), hashDisplay)
				}
				w.Flush()
			} else {
				fmt.Println("No headers synced yet. Start the daemon to begin syncing.")
			}
			return nil
		},
	}
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
