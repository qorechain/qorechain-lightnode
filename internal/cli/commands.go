package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qorechain/qorechain-lightnode/internal/chaintx"
	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/daemon"
	"github.com/qorechain/qorechain-lightnode/internal/keyring"
)

// statusCmd prints node and light client status.
func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show node and light client sync status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
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
				fmt.Fprintf(w, "Signer:\t%s\n", reason)
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
				case lic.Expired():
					fmt.Fprintf(w, "Licence:\texpired at height %d - renew at https://dashboard.qorechain.io -> Tools -> Buy License\n", lic.ExpiresAt)
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
					fmt.Fprintf(w, "Registration:\tnot registered (run: %s register)\n", Binary)
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
	cmd.AddCommand(keysCreateCmd(), keysListCmd(), keysShowCmd(), keysImportCmd(), keysExportCmd(), keysAddOperatorCmd())
	return cmd
}

func keysCreateCmd() *cobra.Command {
	var keyType string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
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
			cfg, err := LoadConfig()
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
			cfg, err := LoadConfig()
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
			cfg, err := LoadConfig()
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
	var out string
	cmd := &cobra.Command{
		Use:   "export <name>",
		Short: "Export a private key in hex",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
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
			if out == "" {
				fmt.Println(hex.EncodeToString(privBytes))
				return nil
			}
			// --out writes the file qorechaind's `tx pqc cosign --pqc-key` reads:
			// bare hex, no trailing newline (qorechaind rejects one with
			// "encoding/hex: invalid byte: U+000A"), readable by the owner only.
			// Redirecting the stdout form produced exactly that broken file, and
			// register used to tell operators to do it.
			if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
				return err
			}
			return os.WriteFile(out, []byte(hex.EncodeToString(privBytes)), 0o600)
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "write the key to this file in the format qorechaind --pqc-key reads (mode 0600)")
	return cmd
}

// registerCmd registers this node on chain.
//
// With the operator key in the keyring (`keys add-operator`) it signs and sends
// every step itself: the node's Dilithium-5 key attached as the account's
// post-quantum key, the missing stake when --delegate-to names a validator,
// then the registration. Without it, or with --print, it prints the same steps
// as qorechaind commands, the only path before v3.1.8.
func registerCmd() *cobra.Command {
	var nodeType, ver, delegateTo string
	var printOnly bool
	cmd := &cobra.Command{
		Use:          "register",
		Short:        "Register this node on chain (post-quantum key, stake, registration)",
		SilenceUsage: true, // the refusal is the answer; usage text buries it
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
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

			// The chain refuses a registration below min_delegated_stake (code 13,
			// "insufficient delegated stake"), after the operator has paid the fee
			// for a transaction that cannot pass. Say so first.
			stakeShort := ""
			have, need := new(big.Int), new(big.Int)
			stakeKnown := false
			if params, err := chain.LightNodeParams(ctx); err == nil {
				if _, ok := need.SetString(params.MinDelegatedStake, 10); ok {
					if dels, err := chain.Delegations(ctx, cfg.OperatorAddress); err == nil {
						stakeKnown = true
						for _, d := range dels {
							if a, ok := new(big.Int).SetString(d.Balance.Amount, 10); ok {
								have.Add(have, a)
							}
						}
						if have.Cmp(need) < 0 {
							stakeShort = fmt.Sprintf("%s uqor delegated, %s required", have, need)
						}
					}
				}
			}

			if len(info.PubKey) == 0 {
				return fmt.Errorf("node key %q has no public key; re-create it with: "+Binary+" keys create <name>", info.Name)
			}

			// Native signing: this binary sends the transactions itself, on any
			// platform. The printed qorechaind commands remain for operators
			// whose account key lives in a qorechaind keyring.
			if !printOnly {
				native, why := daemon.NativeSigner(cfg, keys, lcd)
				if native != nil {
					return registerNative(ctx, native, info.PubKey, nodeType, ver, have, need, stakeKnown, delegateTo)
				}
				fmt.Printf("Not signing here: %s.\n", why)
				fmt.Printf("To let this node sign for itself, run `%s keys add-operator --recover` with the operator account's words.\n", Binary)
				fmt.Println("Otherwise, run these with qorechaind:")
				fmt.Println()
			}
			pub := hex.EncodeToString(info.PubKey)
			// Every printed command names the node explicitly. qorechaind's
			// default is a full node on localhost, which an operator running
			// a light node usually does not have.
			node := "--node " + daemon.QorechaindNode(cfg.RPCAddr)
			fmt.Printf("Licence:          active (lightnode_operator)\n")
			fmt.Printf("Operator address: %s\n", cfg.OperatorAddress)
			fmt.Printf("Node key:         %s (%s)\n", info.Name, info.Type)
			fmt.Printf("Node type:        %s\n", nodeType)
			fmt.Printf("Version:          %s\n", ver)
			fmt.Println()
			fmt.Println("1. Once only: attach this node key to the operator account as its post-quantum key.")
			fmt.Println("   Skip if the account already has one (check: curl -s " + strings.TrimRight(lcd, "/") + "/qorechain/pqc/v1/account/" + cfg.OperatorAddress + ").")
			fmt.Println("   If it has a DIFFERENT key (for example one registered from a wallet), use a dedicated operator account instead.")
			fmt.Println("   Export the key into qorechaind's key directory and register it:")
			fmt.Printf("     "+Binary+" keys export %s --out ~/.qorechaind/pqc/%s.dilithium\n", info.Name, info.Name)
			fmt.Printf("     qorechaind tx pqc register-key-v2 dilithium5 %s hybrid --from operator --chain-id %s %s --gas 400000 --fees 40000uqor -y\n", pub, cfg.ChainID, node)
			fmt.Println()
			step := 2
			if stakeShort != "" {
				fmt.Printf("%d. Delegate stake first: the chain refuses a registration below its minimum (%s).\n", step, stakeShort)
				fmt.Printf("     qorechaind tx staking delegate <qorvaloper...> <amount>uqor --from operator --chain-id %s %s --gas 400000 --fees 40000uqor --generate-only > delegate.json\n", cfg.ChainID, node)
				fmt.Printf("     qorechaind tx pqc cosign delegate.json --from operator --pqc-key %s --chain-id %s %s\n", info.Name, cfg.ChainID, node)
				fmt.Println()
				step++
			}
			fmt.Printf("%d. Register the node (generate, then cosign with the post-quantum key):\n", step)
			fmt.Printf("     qorechaind tx lightnode register %s %s --from operator --chain-id %s %s --gas 300000 --fees 30000uqor --generate-only > register.json\n", nodeType, ver, cfg.ChainID, node)
			fmt.Printf("     qorechaind tx pqc cosign register.json --from operator --pqc-key %s --chain-id %s %s\n", info.Name, cfg.ChainID, node)
			fmt.Println()
			fmt.Println("\"operator\" is the qorechaind key name holding " + cfg.OperatorAddress + "; adjust if yours differs.")
			fmt.Println("Once the registration is in a block, `" + Binary + " start` heartbeats on its own through qorechaind.")
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeType, "type", NodeType, "node type: sx or ux")
	cmd.Flags().StringVar(&ver, "version", Version, "node version")
	cmd.Flags().BoolVar(&printOnly, "print", false, "print qorechaind commands instead of signing")
	cmd.Flags().StringVar(&delegateTo, "delegate-to", "", "qorvaloper... to delegate the missing stake to, if the account is below the minimum")
	return cmd
}

// registerNative attaches the post-quantum key if the account has none,
// delegates the missing stake when asked to, and registers the node, each
// step confirmed in a block before the next.
func registerNative(ctx context.Context, s *chaintx.Signer, nodePub []byte, nodeType, ver string,
	have, need *big.Int, stakeKnown bool, delegateTo string) error {
	op := s.Address()
	done := func(what string, r chaintx.Result) {
		fmt.Printf("  %-22s in block %d (%s)\n", what, r.Height, r.TxHash)
	}
	fmt.Printf("Operator %s, licence active. Signing here.\n\n", op)

	exists, err := s.AccountExists(ctx)
	if err != nil {
		return fmt.Errorf("reading the operator account: %w", err)
	}
	if !exists {
		return chaintx.ErrNoAccount
	}

	registered, found, err := s.RegisteredPQCKey(ctx)
	if err != nil {
		return fmt.Errorf("reading the account's post-quantum key: %w", err)
	}
	switch {
	case found && !bytes.Equal(registered, nodePub):
		return errors.New("the operator account already has a different post-quantum key (registered elsewhere, " +
			"for example from a wallet). This node cannot sign for it. Use a dedicated operator account")
	case found:
		fmt.Println("  post-quantum key       already registered")
	default:
		pop, err := s.SupportsProofOfPossession(ctx)
		if err != nil {
			return err
		}
		msg, err := chaintx.MsgRegisterPQCKeyV2(s.ChainID, op, s.PQCPub, s.PQCPriv, pop)
		if err != nil {
			return err
		}
		r, err := s.SubmitClassical(ctx, 400000, msg)
		if err != nil {
			return fmt.Errorf("registering the post-quantum key: %w", err)
		}
		done("post-quantum key", r)
	}

	if stakeKnown && have.Cmp(need) < 0 {
		short := new(big.Int).Sub(need, have)
		if delegateTo == "" {
			return fmt.Errorf("the account has %s uqor delegated and the chain requires %s. "+
				"Run again with --delegate-to <qorvaloper...> to delegate the missing %s uqor "+
				"(any bonded validator counts)", have, need, short)
		}
		r, err := s.Submit(ctx, 400000, chaintx.MsgDelegate(op, delegateTo, chaintx.Coin{Denom: "uqor", Amount: short.String()}))
		if err != nil {
			return fmt.Errorf("delegating: %w", err)
		}
		done("delegated "+short.String()+" uqor", r)
	}

	r, err := s.Submit(ctx, 300000, chaintx.MsgRegisterLightNode(op, nodeType, ver))
	if err != nil {
		return fmt.Errorf("registering the node: %w", err)
	}
	done("light node registered", r)
	fmt.Printf("\nRun `%s start`: it heartbeats on its own from here.\n", Binary)
	return nil
}

// validatorsCmd queries and displays bonded validators.
func validatorsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validators",
		Short: "List bonded validators",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
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
			cfg, err := LoadConfig()
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
			cfg, err := LoadConfig()
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
			cfg, err := LoadConfig()
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

// keysAddOperatorCmd stores the operator account's secp256k1 key, so the node
// signs its own transactions instead of driving qorechaind.
func keysAddOperatorCmd() *cobra.Command {
	var recover bool
	cmd := &cobra.Command{
		Use:   "add-operator",
		Short: "Store the operator account key so this node can sign (new, or --recover from its words)",
		Long: `Stores the operator account's key in this node's keyring.

Without --recover a new account is created and its 24 words are shown once:
write them down, they are the only backup. Send it QOR, then buy the licence
for its address.

With --recover the words of an existing account are read from standard input,
for example the account a licence was already bought for. The words are
derived exactly as qorechaind and Keplr derive them (m/44'/118'/0'/0/0).`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
			if err != nil {
				return err
			}
			if _, err := keys.Get(daemon.OperatorKeyName); err == nil {
				return fmt.Errorf("the keyring already holds an operator key; remove it first if you mean to replace it")
			}

			var key chaintx.OperatorKey
			var words string
			if recover {
				fmt.Fprintln(cmd.ErrOrStderr(), "Enter the operator account's words, then Enter:")
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil && line == "" {
					return fmt.Errorf("reading the words: %w", err)
				}
				if key, err = chaintx.OperatorKeyFromMnemonic(line); err != nil {
					return err
				}
			} else {
				if key, words, err = chaintx.NewOperatorKey(); err != nil {
					return err
				}
			}

			addr := key.Address()
			if cfg.OperatorAddress != "" && cfg.OperatorAddress != addr {
				return fmt.Errorf("these words are account %s, but operator_address is %s. "+
					"The node can only sign for the account in operator_address", addr, cfg.OperatorAddress)
			}
			if _, err := keys.Import(daemon.OperatorKeyName, keyring.KeyTypeSecp256k1, key.Bytes()); err != nil {
				return fmt.Errorf("storing the key: %w", err)
			}
			if cfg.OperatorAddress == "" {
				cfg.OperatorAddress = addr
				if err := config.Save(CfgFile, cfg); err != nil {
					return fmt.Errorf("key stored, but writing operator_address to %s failed: %w", CfgFile, err)
				}
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Operator account: %s (operator_address in %s)\n", addr, CfgFile)
			if words != "" {
				fmt.Fprintf(out, "\nWrite these 24 words down. They are shown once and are the only backup of this account:\n\n  %s\n\n", words)
				fmt.Fprintln(out, "Next: send this address some QOR for fees and the 1,000 QOR stake, and buy the licence for it.")
			}
			fmt.Fprintf(out, "Then run `%s register`.\n", Binary)
			return nil
		},
	}
	cmd.Flags().BoolVar(&recover, "recover", false, "read an existing account's words from standard input")
	return cmd
}
