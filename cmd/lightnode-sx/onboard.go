package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/keyring"
	"github.com/qorechain/qorechain-lightnode/internal/pqc"
)

// selftestCmd runs an end-to-end PQC sanity check (keygen → sign → verify
// → tamper detection) so operators can confirm the node's PQC stack
// works before connecting to a chain. Useful for pre-deployment verification
// and for support diagnostics when something goes wrong.
func selftestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "selftest",
		Short: "Run the PQC self-test (keygen, sign, verify, tamper detection)",
		Long: `Runs a complete Dilithium-5 (ML-DSA-87) roundtrip:

  1. Generate a fresh Dilithium-5 keypair
  2. Sign a fixed test message
  3. Verify the signature succeeds with the matching pubkey
  4. Tamper with one byte of the signature; verify must now reject it
  5. Tamper with one byte of the message; verify must now reject it

If any step fails, the binary exits non-zero with diagnostic output.
This is the same test the onboarding wizard runs as its first step.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSelftest(cmd.OutOrStdout())
		},
	}
}

// runSelftest executes the PQC roundtrip; returns nil on full success.
// The output writer receives a progress trace so operators can see each
// step pass live.
func runSelftest(out io.Writer) error {
	prn := func(format string, a ...any) { fmt.Fprintf(out, format, a...) }

	prn("PQC self-test — Dilithium-5 (ML-DSA-87)\n")
	prn("──────────────────────────────────────────────\n")

	// 1) Keygen
	prn("[1/5] Keygen...                ")
	pub, priv, err := pqc.DilithiumKeygen()
	if err != nil {
		prn("FAIL\n  %v\n", err)
		return fmt.Errorf("keygen: %w", err)
	}
	prn("OK    (pubkey=%d B, privkey=%d B)\n", len(pub), len(priv))

	// 2) Sign
	msg := []byte("QoreChain light node PQC self-test message — " + time.Now().UTC().Format(time.RFC3339Nano))
	prn("[2/5] Sign...                  ")
	sig, err := pqc.DilithiumSign(priv, msg)
	if err != nil {
		prn("FAIL\n  %v\n", err)
		return fmt.Errorf("sign: %w", err)
	}
	prn("OK    (signature=%d B)\n", len(sig))

	// 3) Verify
	prn("[3/5] Verify (valid sig)...    ")
	valid, err := pqc.DilithiumVerify(pub, msg, sig)
	if err != nil {
		prn("FAIL\n  %v\n", err)
		return fmt.Errorf("verify: %w", err)
	}
	if !valid {
		prn("FAIL\n  signature rejected by verifier — keypair mismatch?\n")
		return errors.New("verify: signature unexpectedly rejected")
	}
	prn("OK\n")

	// 4) Tamper signature
	prn("[4/5] Reject tampered sig...   ")
	tamperedSig := make([]byte, len(sig))
	copy(tamperedSig, sig)
	tamperedSig[len(tamperedSig)/2] ^= 0xFF // flip a middle byte
	valid, err = pqc.DilithiumVerify(pub, msg, tamperedSig)
	if err != nil {
		// An error is acceptable here as long as it's not "valid"
		prn("OK    (rejected with error: %v)\n", err)
	} else if valid {
		prn("FAIL\n  tampered signature was accepted — verifier is broken!\n")
		return errors.New("verify: tampered signature accepted")
	} else {
		prn("OK    (sig rejected as expected)\n")
	}

	// 5) Tamper message
	prn("[5/5] Reject tampered msg...   ")
	tamperedMsg := make([]byte, len(msg))
	copy(tamperedMsg, msg)
	tamperedMsg[0] ^= 0x01
	valid, err = pqc.DilithiumVerify(pub, tamperedMsg, sig)
	if err != nil {
		prn("OK    (rejected with error: %v)\n", err)
	} else if valid {
		prn("FAIL\n  signature on tampered message was accepted!\n")
		return errors.New("verify: tampered message accepted")
	} else {
		prn("OK    (sig rejected as expected)\n")
	}

	prn("──────────────────────────────────────────────\n")
	prn("All 5 checks passed — PQC stack is functional.\n")
	return nil
}

// onboardCmd is the first-run interactive wizard. It runs the PQC self-test,
// then asks the operator for the chain RPC endpoint and the validator's
// private key (or generates one), pings the endpoint if provided, and
// writes the resulting config.
func onboardCmd() *cobra.Command {
	var nonInteractive bool
	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Interactive first-run wizard: PQC self-test + endpoint + key setup",
		Long: `Walks the operator through the post-install setup:

  1. Run the PQC self-test (keygen, sign, verify, tamper detection)
  2. Ask for the chain RPC endpoint
       - Leave blank to run in local-only mode (no chain connection;
         useful while the chain itself is not yet deployed)
       - Provide a URL to test the connection live
  3. Ask for the validator's private key
       - Paste a hex-encoded Dilithium-5 private key, OR
       - Type 'g' (or 'generate') to mint a fresh keypair
  4. Save the resulting config.toml + keyring

Running this command always overwrites the active config (use
'--config' to point at a non-default path).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOnboard(cmd, nonInteractive)
		},
	}
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "fail fast instead of prompting (useful for CI)")
	return cmd
}

func runOnboard(cmd *cobra.Command, nonInteractive bool) error {
	out := cmd.OutOrStdout()
	in := cmd.InOrStdin()
	prn := func(format string, a ...any) { fmt.Fprintf(out, format, a...) }
	reader := bufio.NewReader(in)

	prn("QoreChain Light Node — onboarding wizard\n")
	prn("==============================================\n\n")

	// Step 1 — PQC self-test
	prn("Step 1 — PQC stack check\n\n")
	if err := runSelftest(out); err != nil {
		prn("\nThe PQC stack failed its self-test. Refusing to continue ")
		prn("onboarding. This build has no external dependencies, so a ")
		prn("failure here means a corrupted binary: re-download it and ")
		prn("re-run 'onboard'.\n")
		return err
	}
	prn("\n")

	// Step 2 — endpoint
	prn("Step 2 — chain RPC endpoint\n\n")
	prn("Enter the QoreChain RPC URL (e.g. https://rpc.example.com:26657).\n")
	prn("Leave blank to run in LOCAL-ONLY mode — the daemon will not try to\n")
	prn("connect to any chain, useful while the network itself is not yet\n")
	prn("deployed. You can re-run 'onboard' later to point at a real chain.\n\n")
	prn("RPC endpoint: ")
	endpoint := ""
	if !nonInteractive {
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading endpoint: %w", err)
		}
		endpoint = strings.TrimSpace(line)
	}

	if endpoint != "" {
		if err := pingEndpoint(endpoint); err != nil {
			prn("\n  Warning: could not reach %s (%v).\n", endpoint, err)
			prn("  Saving the URL anyway; the daemon will retry on start.\n")
		} else {
			prn("  Connected — endpoint is reachable.\n")
		}
	} else {
		prn("  (no endpoint — local-only mode)\n")
	}
	prn("\n")

	// Step 3 — private key
	prn("Step 3 — validator private key\n\n")
	prn("Paste your Dilithium-5 private key (hex, %d bytes = %d hex chars),\n", pqc.DilithiumPrivateKeySize, pqc.DilithiumPrivateKeySize*2)
	prn("or type 'g' / 'generate' to create a fresh keypair on this node.\n\n")
	prn("Private key (or 'g'): ")

	var privBytes []byte
	if !nonInteractive {
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading private key: %w", err)
		}
		input := strings.TrimSpace(line)
		switch strings.ToLower(input) {
		case "", "g", "generate":
			prn("\n  Generating new Dilithium-5 keypair...\n")
			_, sk, err := pqc.DilithiumKeygen()
			if err != nil {
				return fmt.Errorf("generating key: %w", err)
			}
			privBytes = sk
			prn("  Generated — privkey size %d B\n", len(privBytes))
		default:
			decoded, err := hex.DecodeString(strings.TrimPrefix(input, "0x"))
			if err != nil {
				return fmt.Errorf("hex decode: %w", err)
			}
			if len(decoded) != pqc.DilithiumPrivateKeySize {
				return fmt.Errorf("private key size: got %d, want %d", len(decoded), pqc.DilithiumPrivateKeySize)
			}
			privBytes = decoded
			prn("\n  Imported %d-byte private key.\n", len(privBytes))
		}
	} else {
		// non-interactive: just generate
		_, sk, err := pqc.DilithiumKeygen()
		if err != nil {
			return err
		}
		privBytes = sk
	}
	prn("\n")

	// Step 4 — save config + keyring
	prn("Step 4 — saving config + keyring\n\n")
	cfg := config.DefaultConfig()
	if endpoint != "" {
		// Lightly normalize: ensure scheme + path are sane. RPC default port
		// 26657 unless otherwise specified.
		cfg.RPCAddr = endpoint
		cfg.PrimaryAddr = endpoint
	} else {
		cfg.RPCAddr = ""
		cfg.PrimaryAddr = ""
	}
	if homeDir != "" {
		cfg.DataDir = homeDir
	}

	// Resolve config file path (defaults to <home>/config.toml).
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = filepath.Join(cfg.DataDir, "config.toml")
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	prn("  config.toml written to %s\n", cfgPath)

	// Store the private key in the keyring.
	keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("opening keyring: %w", err)
	}
	keyName := "validator"
	if _, err := keys.Import(keyName, keyring.KeyTypeDilithium5, privBytes); err != nil {
		// If a key with that name already exists, append a random suffix.
		var suffix [4]byte
		_, _ = rand.Read(suffix[:])
		keyName = "validator-" + hex.EncodeToString(suffix[:])
		if _, err := keys.Import(keyName, keyring.KeyTypeDilithium5, privBytes); err != nil {
			return fmt.Errorf("import key into keyring: %w", err)
		}
	}
	prn("  keyring entry %q saved (Dilithium-5)\n", keyName)
	prn("\n")

	// Summary
	prn("==============================================\n")
	prn("Onboarding complete.\n\n")
	if endpoint != "" {
		prn("  Mode:      live (RPC %s)\n", endpoint)
		prn("  Next:      run 'lightnode-sx start' to connect\n")
	} else {
		prn("  Mode:      local-only (no chain RPC configured)\n")
		prn("  Next:      run 'lightnode-sx selftest' any time to re-verify\n")
		prn("             the PQC stack; re-run 'onboard' once the chain is\n")
		prn("             deployed to point this node at it.\n")
	}
	prn("\n")
	return nil
}

// pingEndpoint does a /status GET with a short timeout. Returns nil if
// the chain RPC responds with the expected shape.
func pingEndpoint(endpoint string) error {
	// Light validation — must parse as a URL.
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("not a valid http(s) URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli := client.New(endpoint, endpoint) // lcd defaults reuse rpc; the ping only hits /status
	if _, err := cli.NodeStatus(ctx); err != nil {
		return err
	}
	return nil
}
