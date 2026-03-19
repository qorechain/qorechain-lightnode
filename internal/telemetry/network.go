package telemetry

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/db"
)

// NetworkCollector periodically fetches network status.
type NetworkCollector struct {
	chain    *client.Client
	store    *db.DB
	logger   *slog.Logger
	interval time.Duration
}

// NewNetworkCollector creates a new network collector.
func NewNetworkCollector(chain *client.Client, store *db.DB, logger *slog.Logger, interval time.Duration) *NetworkCollector {
	return &NetworkCollector{chain: chain, store: store, logger: logger, interval: interval}
}

func (c *NetworkCollector) Name() string { return "network" }

func (c *NetworkCollector) Run(ctx context.Context) error {
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

func (c *NetworkCollector) collect(ctx context.Context) {
	status, err := c.chain.NodeStatus(ctx)
	if err != nil {
		c.logger.Warn("failed to fetch network status", "error", err)
		return
	}

	height, _ := strconv.ParseInt(status.Result.SyncInfo.LatestBlockHeight, 10, 64)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = c.store.Conn().Exec(
		`INSERT OR REPLACE INTO telemetry_network (height, timestamp) VALUES (?, ?)`,
		height, now,
	)
	if err != nil {
		c.logger.Warn("failed to store network telemetry", "error", err)
	}
	c.logger.Debug("collected network telemetry", "height", height)
}
