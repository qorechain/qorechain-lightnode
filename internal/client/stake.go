package client

import "math/big"

// BondedStake is the stake the chain counts against min_delegated_stake: what
// the account has delegated to validators that are bonded now (staking
// GetDelegatorBonded). Delegations to jailed or unbonded validators are left out.
func BondedStake(dels []DelegationEntry, bondedValidators []ValidatorInfo) *big.Int {
	bonded := make(map[string]bool, len(bondedValidators))
	for _, v := range bondedValidators {
		bonded[v.OperatorAddress] = true
	}
	total := new(big.Int)
	for _, d := range dels {
		if !bonded[d.Delegation.ValidatorAddress] {
			continue
		}
		if a, ok := new(big.Int).SetString(d.Balance.Amount, 10); ok {
			total.Add(total, a)
		}
	}
	return total
}
