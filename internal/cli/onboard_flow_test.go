package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/keyring"
	"github.com/spf13/cobra"
)

// fakeRPC answers /status the way a mainnet node does.
func fakeRPC(t *testing.T, network string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":{"node_info":{"network":"` + network + `","version":"0.38.25"},` + //nolint:errcheck
			`"sync_info":{"latest_block_height":"100","latest_block_time":"2026-10-16T00:00:00Z","catching_up":false}}}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func onboardIn(t *testing.T, input string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	HomeDir, CfgFile = dir, filepath.Join(dir, "config.toml")
	t.Cleanup(func() { HomeDir, CfgFile = "", "" })
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&out)
	return dir, runOnboard(cmd, false)
}

// Found on the v3.2.3 devnet rehearsal, 2026-10-16. A node onboarded with the
// wizard could not register: the config said chain qorechain-diana whatever
// endpoint was given, named the key "operator" while the key was saved as
// "validator", and the key had no public key.
func TestOnboardWritesAConfigThatCanRegister(t *testing.T) {
	t.Setenv(keyring.PassphraseEnv, "onboard-test")
	rpc := fakeRPC(t, "qorechain-vladi")

	dir, err := onboardIn(t, rpc+"\ng\n")
	if err != nil {
		t.Fatalf("onboard: %v", err)
	}
	cfg, err := config.Load(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChainID != "qorechain-vladi" {
		t.Errorf("chain_id = %q, want the endpoint's network qorechain-vladi", cfg.ChainID)
	}
	keys, err := keyring.New(cfg.KeyringBackend, dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := keys.Get(cfg.KeyName)
	if err != nil {
		t.Fatalf("config key_name %q is not the key onboard saved: %v", cfg.KeyName, err)
	}
	if len(info.PubKey) == 0 {
		t.Error("the saved key has no public key; register would print an empty register-key-v2")
	}
}

// Without the passphrase the wizard used to write config.toml, generate the
// key, show it as generated and then fail to store it.
func TestOnboardRefusesBeforeGeneratingWhenThePassphraseIsMissing(t *testing.T) {
	t.Setenv(keyring.PassphraseEnv, "")
	dir, err := onboardIn(t, "\ng\n")
	if err == nil || !strings.Contains(err.Error(), keyring.PassphraseEnv) {
		t.Fatalf("err = %v, want a refusal naming %s", err, keyring.PassphraseEnv)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "config.toml")); statErr == nil {
		t.Error("config.toml was written although onboarding could not store the key")
	}
}

// qorechaind's --pqc-key reads bare hex and rejects a trailing newline
// ("encoding/hex: invalid byte: U+000A"). Redirecting `keys export` gave
// exactly that file.
func TestKeysExportOutWritesWhatQorechaindReads(t *testing.T) {
	t.Setenv(keyring.PassphraseEnv, "onboard-test")
	dir, err := onboardIn(t, "\ng\n")
	if err != nil {
		t.Fatal(err)
	}
	HomeDir, CfgFile = dir, filepath.Join(dir, "config.toml")
	cfg, _ := config.Load(CfgFile)

	out := filepath.Join(dir, "qorechaind", "pqc", "node.dilithium")
	cmd := keysExportCmd()
	cmd.SetArgs([]string{cfg.KeyName, "--out", out})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 9792 || strings.ContainsAny(string(b), "\r\n ") {
		t.Errorf("key file is %d bytes, want 9792 bare hex characters", len(b))
	}
	if st, _ := os.Stat(out); st.Mode().Perm() != 0o600 {
		t.Errorf("key file mode %v, want 0600", st.Mode().Perm())
	}
}
