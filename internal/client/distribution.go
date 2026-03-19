package client

import (
	"context"
	"fmt"
)

// Rewards returns delegation rewards for an address.
func (c *Client) Rewards(ctx context.Context, delegator string) (*RewardsResponse, error) {
	var resp RewardsResponse
	url := fmt.Sprintf("%s/cosmos/distribution/v1beta1/delegators/%s/rewards", c.lcdURL, delegator)
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
