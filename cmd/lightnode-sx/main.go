package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/daemon"
	"github.com/qorechain/qorechain-lightnode/internal/keyring"
)

const version = "3.1.2"

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
		selftestCmd(),
		onboardCmd(),
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
		// A missing file means "not onboarded yet": run on defaults so the
		// onboarding pointer and the key commands still work. Any other
		// failure (unparseable TOML, an invalid operator_address) is an error
		// the operator must see; it used to be swallowed here, and the daemon
		// then ran silently on defaults, on the wrong chain, with the wrong
		// keyring backend.
		if !errors.Is(err, os.ErrNotExist) {
			return cfg, err
		}
		cfg = config.DefaultConfig()
	}
	if homeDir != "" {
		cfg.DataDir = homeDir
	}
	return cfg, nil
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
				if _, err := os.Stat(cfgFile); errors.Is(err, os.ErrNotExist) {
					fmt.Fprintf(os.Stderr, "No config file found at %s.\n\n", cfgFile)
					fmt.Fprintf(os.Stderr, "Run 'lightnode-sx onboard' to set up the node interactively\n")
					fmt.Fprintf(os.Stderr, "(PQC self-test + chain endpoint + private key).\n\n")
					fmt.Fprintf(os.Stderr, "Or pass --skip-onboarding-check to start with defaults\n")
					fmt.Fprintf(os.Stderr, "(local-only mode — no chain RPC connection).\n")
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

			fmt.Fprintf(os.Stderr, "QoreChain SX Light Node v%s starting...\n", version)
			if cfg.RPCAddr == "" {
				fmt.Fprintf(os.Stderr, "Running in LOCAL-ONLY mode (no chain RPC configured).\n")
				fmt.Fprintf(os.Stderr, "Re-run 'lightnode-sx onboard' to connect to a chain.\n")
			}
			return d.Run(context.Background())
		},
	}
	cmd.Flags().BoolVar(&skipOnboardingCheck, "skip-onboarding-check", false, "do not require config.toml at startup (allows local-only start)")
	return cmd
}

// statusCmd prints node and light client status.
func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show node and light client sync status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
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
			if ok, reason := d.Signer(); ok {
				fmt.Fprintf(w, "Signer:\tqorechaind available\n")
			} else {
				fmt.Fprintf(w, "Signer:\tunavailable (%s)\n", reason)
			}
			if cfg.OperatorAddress == "" {
				fmt.Fprintf(w, "Operator:\tnot set (operator_address in config.toml)\n")
			} else {
				fmt.Fprintf(w, "Operator:\t%s\n", cfg.OperatorAddress)
				lic, err := d.Chain().LicenseCheck(ctx, cfg.OperatorAddress, client.FeatureLightNodeOperator)
				switch {
				case err != nil:
					fmt.Fprintf(w, "Licence:\tunknown (%v)\n", err)
				case !lic.Found:
					fmt.Fprintf(w, "Licence:\tnone - buy one at https://dashboard.qorechain.io -> Tools -> Buy License\n")
				case !lic.Active:
					fmt.Fprintf(w, "Licence:\tsuspended\n")
				default:
					fmt.Fprintf(w, "Licence:\tactive\n")
				}
				registered, node, err := d.Chain().LightNodeRegistration(ctx, cfg.OperatorAddress)
				switch {
				case err != nil:
					fmt.Fprintf(w, "Registration:\tunknown (%v)\n", err)
				case !registered:
					fmt.Fprintf(w, "Registration:\tnot registered (run: lightnode-sx register)\n")
				default:
					fmt.Fprintf(w, "Registration:\t%s, last heartbeat at height %s\n", node.LightNode.Status, node.LightNode.LastHeartbeat)
				}
			}
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
	cmd.AddCommand(keysCreateCmd(), keysListCmd(), keysShowCmd(), keysImportCmd(), keysExportCmd())
	return cmd
}

func keysCreateCmd() *cobra.Command {
	var keyType string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
			if err != nil {
				return err
			}
			info, err := keys.Create(args[0], keyring.KeyType(keyType))
			if err != nil {
				return fmt.Errorf("creating key: %w", err)
			}
			fmt.Printf("Name:       %s\n", info.Name)
			fmt.Printf("Type:       %s\n", info.Type)
			fmt.Printf("Public key: %s\n", hex.EncodeToString(info.PubKey))
			fmt.Println()
			fmt.Println(noAddressNote)
			return nil
		},
	}
	cmd.Flags().StringVar(&keyType, "type", "dilithium5", "key type (currently supported: dilithium5)")
	return cmd
}

func keysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
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
			fmt.Fprintf(w, "NAME\tTYPE\tADDRESS\tPUBLIC KEY\n")
			for _, k := range list {
				addr := k.Address
				if addr == "" {
					addr = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", k.Name, k.Type, addr, abbreviateHex(k.PubKey))
			}
			w.Flush()
			fmt.Println()
			fmt.Println(noAddressNote)
			return nil
		},
	}
}

// noAddressNote explains the one thing every new operator trips over: the
// node's post-quantum key is not an account, so it has no address.
const noAddressNote = `A Dilithium-5 key has no chain address of its own. On QoreChain a post-quantum
key is attached to an account, it does not make one. Your operator address is the
funded qor1... account you register the node from: create it with qorechaind
("qorechaind keys add operator"), fund it, attach this key to it (see "register"),
and set operator_address in config.toml.`

func abbreviateHex(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	h := hex.EncodeToString(b)
	if len(h) <= 24 {
		return h
	}
	return h[:12] + "..." + h[len(h)-8:]
}

func keysShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show a key's type and full public key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
			if err != nil {
				return err
			}
			info, err := keys.Get(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Name:       %s\n", info.Name)
			fmt.Printf("Type:       %s\n", info.Type)
			if info.Address != "" {
				fmt.Printf("Address:    %s\n", info.Address)
			}
			fmt.Printf("Public key: %s\n", hex.EncodeToString(info.PubKey))
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
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
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
	cmd.Flags().StringVar(&keyType, "type", "dilithium5", "key type (currently supported: dilithium5)")
	return cmd
}

func keysExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a private key in hex",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
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

// registerCmd prints the exact chain commands that register this node.
//
// The chain requires a post-quantum hybrid signature on every transaction, so
// registration is a generate-then-cosign pair run with qorechaind against the
// operator's funded account. The node's Dilithium-5 key is that account's
// post-quantum key; if it is not attached yet, the first command attaches it.
func registerCmd() *cobra.Command {
	var nodeType, ver string
	cmd := &cobra.Command{
		Use:          "register",
		Short:        "Print the chain commands that register this node",
		SilenceUsage: true, // the refusal is the answer; usage text buries it
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
			if err != nil {
				return err
			}
			info, err := keys.Get(cfg.KeyName)
			if err != nil {
				return fmt.Errorf("node key %q not found: %w", cfg.KeyName, err)
			}
			if cfg.OperatorAddress == "" {
				return fmt.Errorf("operator_address is not set in config.toml.\n\n%s\n\nThe address is printed by: qorechaind keys show operator -a", noAddressNote)
			}
			if err := config.ValidateOperatorAddress(cfg.OperatorAddress); err != nil {
				return err
			}

			// The chain refuses a registration without an active licence, so ask
			// it first and say plainly where the licence comes from, instead of
			// printing commands the chain will reject.
			ctx := context.Background()
			lcd := cfg.APIAddr
			if lcd == "" {
				lcd = client.DeriveLCDURL(cfg.RPCAddr)
			}
			chain := client.New(cfg.RPCAddr, lcd)
			lic, err := chain.LicenseCheck(ctx, cfg.OperatorAddress, client.FeatureLightNodeOperator)
			if err != nil {
				return fmt.Errorf("checking the licence on %s: %w", cfg.RPCAddr, err)
			}
			if blocked, msg := daemon.LicenceGate(cfg.OperatorAddress, lic); blocked {
				return errors.New(msg)
			}
			if registered, node, err := chain.LightNodeRegistration(ctx, cfg.OperatorAddress); err != nil {
				return fmt.Errorf("checking the registration on %s: %w", cfg.RPCAddr, err)
			} else if registered {
				fmt.Printf("Already registered: %s is a %s node, status %s, last heartbeat at height %s. Nothing to do.\n",
					cfg.OperatorAddress, node.LightNode.NodeType, node.LightNode.Status, node.LightNode.LastHeartbeat)
				return nil
			}

			pub := hex.EncodeToString(info.PubKey)
			fmt.Printf("Licence:          active (lightnode_operator)\n")
			fmt.Printf("Operator address: %s\n", cfg.OperatorAddress)
			fmt.Printf("Node key:         %s (%s)\n", info.Name, info.Type)
			fmt.Printf("Node type:        %s\n", nodeType)
			fmt.Printf("Version:          %s\n", ver)
			fmt.Println()
			fmt.Println("1. Once only: attach this node key to the operator account as its post-quantum key.")
			fmt.Println("   Skip if the account already has one (check: curl -s https://api.qore.host/qorechain/pqc/v1/account/" + cfg.OperatorAddress + ").")
			fmt.Println("   Export the key into qorechaind's key directory and register it:")
			fmt.Printf("     lightnode-sx keys export %s > ~/.qorechaind/pqc/%s.dilithium && chmod 600 ~/.qorechaind/pqc/%s.dilithium\n", info.Name, info.Name, info.Name)
			fmt.Printf("     qorechaind tx pqc register-key-v2 dilithium5 %s hybrid --from operator --chain-id %s --gas 400000 --fees 40000uqor -y\n", pub, cfg.ChainID)
			fmt.Println()
			fmt.Println("2. Register the node (generate, then cosign with the post-quantum key):")
			fmt.Printf("     qorechaind tx lightnode register %s %s --from operator --chain-id %s --gas 300000 --fees 30000uqor --generate-only > register.json\n", nodeType, ver, cfg.ChainID)
			fmt.Printf("     qorechaind tx pqc cosign register.json --from operator --pqc-key %s --chain-id %s\n", info.Name, cfg.ChainID)
			fmt.Println()
			fmt.Println("\"operator\" is the qorechaind key name holding " + cfg.OperatorAddress + "; adjust if yours differs.")
			fmt.Println("Once the registration is in a block, `lightnode-sx start` heartbeats on its own through qorechaind.")
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
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
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
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
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
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
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
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
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
