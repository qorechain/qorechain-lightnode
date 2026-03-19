package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/db"
)

// BridgeCollector periodically fetches bridge connection status.
type BridgeCollector struct {
	chain    *client.Client
	store    *db.DB
	logger   *slog.Logger
	interval time.Duration
}

// NewBridgeCollector creates a new bridge collector.
func NewBridgeCollector(chain *client.Client, store *db.DB, logger *slog.Logger, interval time.Duration) *BridgeCollector {
	return &BridgeCollector{chain: chain, store: store, logger: logger, interval: interval}
}

func (c *BridgeCollector) Name() string { return "bridge" }

func (c *BridgeCollector) Run(ctx context.Context) error {
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

func (c *BridgeCollector) collect(ctx context.Context) {
	resp, err := c.chain.BridgeStatus(ctx)
	if err != nil {
		c.logger.Warn("failed to fetch bridge status", "error", err)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, conn := range resp.Connections {
		_, err := c.store.Conn().Exec(
			`INSERT OR REPLACE INTO telemetry_bridge (chain, chain_type, status, pending_transfers, updated_at)
			 VALUES (?, ?, ?, ?, ?)`,
			conn.ChainName, conn.ChainType, conn.Status, conn.PendingTransfers, now,
		)
		if err != nil {
			c.logger.Warn("failed to store bridge status", "chain", conn.ChainName, "error", err)
		}
	}
	c.logger.Debug("collected bridge status", "connections", len(resp.Connections))
}
