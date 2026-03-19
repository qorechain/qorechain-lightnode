package client

import "context"

// BurnStats queries burn module statistics.
func (c *Client) BurnStats(ctx context.Context) (*BurnStatsResponse, error) {
	var resp BurnStatsResponse
	if err := c.get(ctx, c.lcdURL+"/qorechain/burn/v1/stats", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BridgeStatus queries bridge connection statuses.
func (c *Client) BridgeStatus(ctx context.Context) (*BridgeStatusResponse, error) {
	var resp BridgeStatusResponse
	if err := c.get(ctx, c.lcdURL+"/qorechain/bridge/v1/connections", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
