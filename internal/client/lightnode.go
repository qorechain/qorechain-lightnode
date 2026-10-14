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

// MonthlyCapUpgrades lists every upgrade name that turns on the chain's
// lightnode-monthly-cap switch. From chain v3.2.3 the switch is enabled at the
// plan height of the upgrade named "v3.2.3", on mainnet and testnet alike, and
// it is one-way: no later release turns it off.
//
// It is a list, not a single name, on purpose. Asking the chain about one plan
// name is the mistake that silently locked every wallet out when sign-bytes
// switched (see SignBytesV2Upgrades in qorechain-core): a coordinated rename
// must be an edit to this list, not a client that quietly answers "off".
var MonthlyCapUpgrades = []string{"v3.2.3"}

// MonthlyCapActive reports whether the chain applies the monthly per-node
// bound. known is false when the chain could not be asked, in which case the
// caller must not pick one bound over the other.
//
// A chain started from genesis on v3.2.3 or later enables the switch at height
// 0 and has no "v3.2.3" plan, so it reads as inactive here. Neither public
// network is such a chain; on a fresh devnet the caller's two-figure display
// is the honest fallback.
//
// The first settlement after the switch still uses the per-block bound once,
// because there is no previous settlement time to prorate from. That is one
// 100-block window and is not worth modelling.
func (c *Client) MonthlyCapActive(ctx context.Context) (active bool, known bool) {
	for _, name := range MonthlyCapUpgrades {
		var resp struct {
			Height string `json:"height"`
		}
		url := fmt.Sprintf("%s/cosmos/upgrade/v1beta1/applied_plan/%s", c.lcdURL, name)
		if err := c.get(ctx, url, &resp); err != nil {
			return false, false
		}
		h, err := strconv.ParseInt(resp.Height, 10, 64)
		if err != nil {
			return false, false
		}
		if h > 0 {
			return true, true
		}
	}
	return false, true
}
