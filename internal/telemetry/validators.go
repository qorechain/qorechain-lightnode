package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/db"
)

// ValidatorCollector periodically fetches validator data.
type ValidatorCollector struct {
	chain    *client.Client
	store    *db.DB
	logger   *slog.Logger
	interval time.Duration
}

// NewValidatorCollector creates a new validator collector.
func NewValidatorCollector(chain *client.Client, store *db.DB, logger *slog.Logger, interval time.Duration) *ValidatorCollector {
	return &ValidatorCollector{chain: chain, store: store, logger: logger, interval: interval}
}

func (c *ValidatorCollector) Name() string { return "validators" }

func (c *ValidatorCollector) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Initial collection
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

func (c *ValidatorCollector) collect(ctx context.Context) {
	validators, err := c.chain.Validators(ctx)
	if err != nil {
		c.logger.Warn("failed to fetch validators", "error", err)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, v := range validators {
		_, err := c.store.Conn().Exec(
			`INSERT OR REPLACE INTO telemetry_validators
			 (address, moniker, pool, jailed, updated_at)
			 VALUES (?, ?, ?, ?, ?)`,
			v.OperatorAddress, v.Moniker, v.Status, v.Jailed, now,
		)
		if err != nil {
			c.logger.Warn("failed to store validator", "address", v.OperatorAddress, "error", err)
		}
	}
	c.logger.Debug("collected validators", "count", len(validators))
}
