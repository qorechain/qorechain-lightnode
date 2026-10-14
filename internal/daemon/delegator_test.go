package daemon

import (
	"errors"
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/config"
)

// The delegation manager reads staking rewards and delegations for ONE
// account, and that account is the operator's: operator_address is the funded
// chain account the node acts for. It used to be the address of the keyring
// key, which for a post-quantum key is not a chain account at all, so
// /api/rewards never had staking_rewards and /api/delegation came back null on
// a node whose operator held real stake (reported by the QoreX session against
// a 3.1.5 build: 9.77 QOR of staking rewards, none shown).
func TestDelegatorIsTheOperatorAddress(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OperatorAddress = "qor1operator"
	keyAddr := func() (string, error) { return "qor1keyringkey", nil }

	if got := delegatorAddress(cfg, keyAddr); got != "qor1operator" {
		t.Fatalf("delegator = %q, want the configured operator address", got)
	}
}

// Without an operator address the key's own address is the only candidate,
// which keeps a node configured the old way working as before.
func TestDelegatorFallsBackToTheKeyWithoutAnOperator(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OperatorAddress = ""

	if got := delegatorAddress(cfg, func() (string, error) { return "qor1keyringkey", nil }); got != "qor1keyringkey" {
		t.Errorf("delegator = %q, want the key address as fallback", got)
	}
	if got := delegatorAddress(cfg, func() (string, error) { return "", errors.New("no key") }); got != "" {
		t.Errorf("delegator = %q with neither available, want empty", got)
	}
}
