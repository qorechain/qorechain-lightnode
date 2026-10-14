package client

import (
	"context"
	"fmt"
	"strconv"
	"time"
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

// RewardPool queries how the light-node reward pool divides up: what is
// reserved, what is already owed to operators, and what the next settlement may
// actually pay out. Added to the chain by the v3.1.98 upgrade; a chain without
// it answers 501 and get() turns that into ErrEndpointUnavailable.
func (c *Client) RewardPool(ctx context.Context) (*RewardPoolResponse, error) {
	var resp RewardPoolResponse
	url := fmt.Sprintf("%s/qorechain/lightnode/v1/reward_pool", c.lcdURL)
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BlockTimeSeconds measures the live block cadence by comparing the latest
// block with one `window` blocks earlier, and returns the mean seconds per
// block.
//
// It exists because the chain's per-node reward ceiling is denominated per
// BLOCK, while the decision behind it, and the only figure an operator can act
// on, is per month. Converting between the two needs the real cadence, and the
// real cadence moves: it was 2.32 s/block in August 2026 and 3.22 s/block in
// October, which is a 28% change in what the same ceiling is worth per month.
// Anything that prints a monthly figure from a fixed assumed cadence is telling
// the operator a number the chain will not pay.
func (c *Client) BlockTimeSeconds(ctx context.Context, window int64) (float64, error) {
	if window <= 0 {
		return 0, fmt.Errorf("window must be positive, got %d", window)
	}
	latest, err := c.LatestBlock(ctx)
	if err != nil {
		return 0, fmt.Errorf("latest block: %w", err)
	}
	h, err := strconv.ParseInt(latest.Block.Header.Height, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("latest height %q: %w", latest.Block.Header.Height, err)
	}
	tLatest, err := time.Parse(time.RFC3339, latest.Block.Header.Time)
	if err != nil {
		return 0, fmt.Errorf("latest block time %q: %w", latest.Block.Header.Time, err)
	}

	from := h - window
	if from < 1 {
		from = 1
	}
	if from >= h {
		return 0, fmt.Errorf("chain too short to measure cadence (height %d)", h)
	}
	earlier, err := c.BlockAt(ctx, from)
	if err != nil {
		return 0, fmt.Errorf("block %d: %w", from, err)
	}
	tFrom, err := time.Parse(time.RFC3339, earlier.Result.Block.Header.Time)
	if err != nil {
		return 0, fmt.Errorf("block %d time %q: %w", from, earlier.Result.Block.Header.Time, err)
	}

	elapsed := tLatest.Sub(tFrom).Seconds()
	blocks := float64(h - from)
	if elapsed <= 0 {
		return 0, fmt.Errorf("non-positive elapsed time over %0.f blocks", blocks)
	}
	return elapsed / blocks, nil
}
