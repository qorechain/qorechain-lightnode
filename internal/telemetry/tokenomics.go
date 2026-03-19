package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/db"
)

// TokenomicsCollector periodically fetches burn and supply data.
type TokenomicsCollector struct {
	chain    *client.Client
	store    *db.DB
	logger   *slog.Logger
	interval time.Duration
}

// NewTokenomicsCollector creates a new tokenomics collector.
func NewTokenomicsCollector(chain *client.Client, store *db.DB, logger *slog.Logger, interval time.Duration) *TokenomicsCollector {
	return &TokenomicsCollector{chain: chain, store: store, logger: logger, interval: interval}
}

func (c *TokenomicsCollector) Name() string { return "tokenomics" }

func (c *TokenomicsCollector) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	c.collect(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.collect(ctx)
		}
	}
}

func (c *TokenomicsCollector) collect(ctx context.Context) {
	burnStats, err := c.chain.BurnStats(ctx)
	if err != nil {
		c.logger.Warn("failed to fetch burn stats", "error", err)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = c.store.Conn().Exec(
		`INSERT OR REPLACE INTO telemetry_tokenomics (height, total_burned, updated_at) VALUES (0, ?, ?)`,
		burnStats.Stats.TotalBurned, now,
	)
	if err != nil {
		c.logger.Warn("failed to store tokenomics", "error", err)
	}
	c.logger.Debug("collected tokenomics")
}
