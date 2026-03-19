package client

import (
	"context"
	"fmt"
)

// ReputationScore returns the reputation score for a validator.
func (c *Client) ReputationScore(ctx context.Context, validator string) (*ReputationResponse, error) {
	var resp ReputationResponse
	url := fmt.Sprintf("%s/qorechain/reputation/v1/score/%s", c.lcdURL, validator)
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
