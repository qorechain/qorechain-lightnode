package cli

import (
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/client"
)

func del(val, amount string) client.DelegationEntry {
	var d client.DelegationEntry
	d.Delegation.ValidatorAddress = val
	d.Balance.Denom, d.Balance.Amount = "uqor", amount
	return d
}

// The chain (staking GetDelegatorBonded) counts only stake on bonded
// validators. Counting a jailed validator's delegation let register pass its
// own check and then be refused on chain.
func TestOnlyStakeOnBondedValidatorsCounts(t *testing.T) {
	vals := []client.ValidatorInfo{{OperatorAddress: "qorvaloper1bonded"}}
	got := bondedStake([]client.DelegationEntry{
		del("qorvaloper1bonded", "600000000"),
		del("qorvaloper1jailed", "900000000"),
	}, vals)
	if got.String() != "600000000" {
		t.Fatalf("counted %s uqor, want 600000000 (the jailed validator's stake does not count)", got)
	}
}
