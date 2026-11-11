package client

import (
	"context"
	"fmt"
	"strconv"
)

// Validators returns all bonded validators.
func (c *Client) Validators(ctx context.Context) ([]ValidatorInfo, error) {
	var resp ValidatorsResponse
	if err := c.get(ctx, c.lcdURL+"/cosmos/staking/v1beta1/validators?status=BOND_STATUS_BONDED&pagination.limit=200", &resp); err != nil {
		return nil, err
	}
	return resp.Validators, nil
}

// Delegations returns all delegations for an address.
func (c *Client) Delegations(ctx context.Context, delegator string) ([]DelegationEntry, error) {
	var resp DelegationResponse
	url := fmt.Sprintf("%s/cosmos/staking/v1beta1/delegations/%s", c.lcdURL, delegator)
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, err
	}
	return resp.DelegationResponses, nil
}

// Validator returns one validator, whatever its status.
func (c *Client) Validator(ctx context.Context, valoper string) (*ValidatorInfo, error) {
	var resp struct {
		Validator ValidatorInfo `json:"validator"`
	}
	url := fmt.Sprintf("%s/cosmos/staking/v1beta1/validators/%s", c.lcdURL, valoper)
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, err
	}
	return &resp.Validator, nil
}

// SpendableUqor is the account's spendable uqor: its balance minus what is
// locked by vesting. Delegated stake is not in it. Fees are paid from it.
func (c *Client) SpendableUqor(ctx context.Context, addr string) (uint64, error) {
	var resp struct {
		Balance struct {
			Amount string `json:"amount"`
		} `json:"balance"`
	}
	url := fmt.Sprintf("%s/cosmos/bank/v1beta1/spendable_balances/%s/by_denom?denom=uqor", c.lcdURL, addr)
	if err := c.get(ctx, url, &resp); err != nil {
		return 0, err
	}
	if resp.Balance.Amount == "" {
		return 0, nil
	}
	return strconv.ParseUint(resp.Balance.Amount, 10, 64)
}
