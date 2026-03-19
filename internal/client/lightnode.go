package client

import (
	"context"
	"fmt"
)

// LightNode queries a specific light node.
func (c *Client) LightNode(ctx context.Context, address string) (*LightNodeQueryResponse, error) {
	var resp LightNodeQueryResponse
	url := fmt.Sprintf("%s/qorechain/lightnode/v1/node/%s", c.lcdURL, address)
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LightNodeParams queries module parameters.
func (c *Client) LightNodeParams(ctx context.Context) (*LightNodeParamsResponse, error) {
	var resp LightNodeParamsResponse
	if err := c.get(ctx, c.lcdURL+"/qorechain/lightnode/v1/params", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LightNodeStats queries network statistics.
func (c *Client) LightNodeStats(ctx context.Context) (*LightNodeStatsResponse, error) {
	var resp LightNodeStatsResponse
	if err := c.get(ctx, c.lcdURL+"/qorechain/lightnode/v1/stats", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
