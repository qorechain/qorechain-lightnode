package client

import (
	"context"
	"fmt"
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
