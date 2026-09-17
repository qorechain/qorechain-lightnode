package daemon

import (
	"strings"
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/config"
)

// Liviu's rule: a node registers only with a bought licence, and an operator
// without one is told so, and told where it comes from. The gate is the one
// place that wording lives, so this pins it.
func TestLicenceGateNamesTheDashboardAndTheOperatorAddress(t *testing.T) {
	blocked, msg := LicenceGate("qor1abc", client.LicenseStatus{})
	if !blocked {
		t.Fatal("an operator without a licence was let through")
	}
	for _, want := range []string{"qor1abc", "no lightnode_operator licence", "dashboard.qorechain.io", "Buy License", "operator address"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message lost %q: %q", want, msg)
		}
	}
	if blocked, msg = LicenceGate("qor1abc", client.LicenseStatus{Found: true, Active: false}); !blocked || !strings.Contains(msg, "suspended") {
		t.Fatalf("a suspended licence must block with its reason: %v %q", blocked, msg)
	}
	if blocked, _ = LicenceGate("qor1abc", client.LicenseStatus{Found: true, Active: true}); blocked {
		t.Fatal("an active licence was refused")
	}
}

// The signer shells out to qorechaind; the arguments are the contract with it.
func TestCLISignerResolvesFromConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Heartbeat.QorechaindPath = "/definitely/not/here/qorechaind"
	if s, note := newCLISigner(cfg); s != nil || !strings.Contains(note, "qorechaind_path") {
		t.Fatalf("a missing configured binary must be reported, got signer=%v note=%q", s, note)
	}

	cfg.Heartbeat.QorechaindPath = "/bin/sh" // exists; never executed here
	cfg.Heartbeat.QorechaindHome = "/tmp/qhome"
	cfg.Heartbeat.KeyringBackend = ""
	cfg.KeyringBackend = "file"
	cfg.RPCAddr = "http://localhost:26657"
	s, note := newCLISigner(cfg)
	if s == nil {
		t.Fatalf("signer not built: %s", note)
	}
	if s.home != "/tmp/qhome" || s.backend != "file" || s.node != "tcp://localhost:26657" || s.chainID != cfg.ChainID {
		t.Fatalf("signer fields wrong: %+v", s)
	}
}

// Heartbeats are on by default now: a registered node that is silent is marked
// inactive by the chain, so silence must be an explicit choice, not the default.
func TestHeartbeatsAreOnByDefaultAndAutoClaimIsOff(t *testing.T) {
	cfg := config.DefaultConfig()
	if !cfg.Heartbeat.Enabled {
		t.Fatal("heartbeats default off; a registered node would go inactive")
	}
	if cfg.Delegation.AutoClaim || cfg.Delegation.AutoCompound {
		t.Fatal("auto-claim must be opt-in: it spends the operator's fee balance")
	}
}
