package client

import "testing"

func del(val, amount string) DelegationEntry {
	var d DelegationEntry
	d.Delegation.ValidatorAddress = val
	d.Balance.Denom, d.Balance.Amount = "uqor", amount
	return d
}

// The chain (staking GetDelegatorBonded) counts only stake on bonded
// validators. Counting a jailed validator's delegation let register pass its
// own check and then be refused on chain.
func TestOnlyStakeOnBondedValidatorsCounts(t *testing.T) {
	vals := []ValidatorInfo{{OperatorAddress: "qorvaloper1bonded"}}
	got := BondedStake([]DelegationEntry{
		del("qorvaloper1bonded", "600000000"),
		del("qorvaloper1jailed", "900000000"),
	}, vals)
	if got.String() != "600000000" {
		t.Fatalf("counted %s uqor, want 600000000 (the jailed validator's stake does not count)", got)
	}
}
