package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/config"
)

// QorechaindPassphraseEnv holds the passphrase of the qorechaind keyring that
// keeps the operator key, for unattended use. qorechaind reads it from stdin
// when it is not attached to a terminal, so the daemon pipes it in and never
// puts it on a command line where every process on the host could read it.
const QorechaindPassphraseEnv = "QORE_LIGHTNODE_QORECHAIND_PASSPHRASE"

// cliSigner submits light-node transactions by driving the qorechaind CLI:
// `tx lightnode <cmd> --generate-only`, then `tx pqc cosign`, which attaches
// the hybrid Dilithium-5 signature the chain requires and broadcasts.
//
// Both the heartbeat and the reward claim go through here. Reusing the node
// binary's signer is deliberate: it is the reference implementation of the
// hybrid sign-bytes, and re-implementing protobuf transactions plus the
// hybrid framing inside this minimal-dependency client is how the previous
// claim path ended up signing a document the chain could never verify.
type cliSigner struct {
	binary  string
	home    string
	backend string
	chainID string
	node    string
	fees    string
	gas     string
}

// newCLISigner resolves the signer from configuration. A missing binary is
// not an error here: the caller decides whether that disables a feature or
// fails the start, and reports it once with the reason.
func newCLISigner(cfg config.Config) (*cliSigner, string) {
	hb := cfg.Heartbeat
	binary := hb.QorechaindPath
	if binary == "" {
		p, err := exec.LookPath("qorechaind")
		if err != nil {
			return nil, "qorechaind not found: set [heartbeat] qorechaind_path or put qorechaind on PATH " +
				"(the Docker image ships it at /usr/local/bin/qorechaind)"
		}
		binary = p
	} else if _, err := os.Stat(binary); err != nil {
		return nil, fmt.Sprintf("qorechaind_path %q: %v", binary, err)
	}

	home := hb.QorechaindHome
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".qorechaind")
		}
	}
	backend := hb.KeyringBackend
	if backend == "" {
		backend = cfg.KeyringBackend
	}
	return &cliSigner{
		binary:  binary,
		home:    home,
		backend: backend,
		chainID: cfg.ChainID,
		node:    toTCP(cfg.RPCAddr),
		fees:    hb.Fees,
		gas:     hb.Gas,
	}, ""
}

// txResult is the part of the broadcast answer the daemon acts on.
type txResult struct {
	TxHash string `json:"txhash"`
	Code   int    `json:"code"`
	RawLog string `json:"raw_log"`
}

// Submit builds, cosigns and broadcasts one light-node message. txArgs is the
// qorechaind subcommand after "tx", e.g. ["lightnode", "heartbeat"]. keyName
// is the qorechaind key holding the operator account; the post-quantum key
// under <home>/pqc/<pqcKey>.dilithium cosigns.
func (s *cliSigner) Submit(ctx context.Context, keyName, pqcKey string, txArgs ...string) (txResult, error) {
	common := []string{
		"--chain-id", s.chainID,
		"--node", s.node,
		"--keyring-backend", s.backend,
		"--home", s.home,
	}

	genArgs := append([]string{"tx"}, txArgs...)
	genArgs = append(genArgs, "--from", keyName, "--generate-only", "--gas", s.gas, "--fees", s.fees)
	genArgs = append(genArgs, common...)
	unsigned, err := s.run(ctx, genArgs)
	if err != nil {
		return txResult{}, fmt.Errorf("generate: %w", err)
	}

	tmp, err := os.CreateTemp("", "ln-tx-*.json")
	if err != nil {
		return txResult{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(unsigned); err != nil {
		tmp.Close()
		return txResult{}, err
	}
	tmp.Close()

	cosignArgs := append([]string{"tx", "pqc", "cosign", tmp.Name(), "--from", keyName,
		"--pqc-key", pqcKey, "-y", "-b", "sync", "-o", "json"}, common...)
	out, err := s.run(ctx, cosignArgs)
	if err != nil {
		return txResult{}, fmt.Errorf("cosign/broadcast: %w", err)
	}

	var res txResult
	if err := json.Unmarshal(out, &res); err != nil {
		return txResult{}, fmt.Errorf("parse result: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if res.Code != 0 {
		return res, fmt.Errorf("rejected by the chain: code %d: %s", res.Code, res.RawLog)
	}
	return res, nil
}

// run executes one qorechaind invocation with the keyring passphrase on stdin.
// Stderr is folded into the error so a refusal names its reason instead of
// "exit status 1".
func (s *cliSigner) run(ctx context.Context, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.binary, args...)
	if pass := os.Getenv(QorechaindPassphraseEnv); pass != "" {
		cmd.Stdin = strings.NewReader(pass + "\n")
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, lastLine(msg))
	}
	return out, nil
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
