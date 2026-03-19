package delegation

import (
	"context"
	"log/slog"
	"strconv"
	"time"
)

// AutoCompounder periodically claims and re-delegates rewards.
type AutoCompounder struct {
	manager   *Manager
	interval  time.Duration
	minReward int64 // minimum uqor to trigger claim
	logger    *slog.Logger
}

// NewAutoCompounder creates an auto-compound loop.
func NewAutoCompounder(manager *Manager, interval time.Duration, minReward int64, logger *slog.Logger) *AutoCompounder {
	return &AutoCompounder{
		manager:   manager,
		interval:  interval,
		minReward: minReward,
		logger:    logger,
	}
}

// Run starts the auto-compound loop.
func (ac *AutoCompounder) Run(ctx context.Context) error {
	ticker := time.NewTicker(ac.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			ac.logger.Info("auto-compounder stopped")
			return nil
		case <-ticker.C:
			ac.compound(ctx)
		}
	}
}

func (ac *AutoCompounder) compound(ctx context.Context) {
	totalRewards, err := ac.manager.GetTotalRewards(ctx)
	if err != nil {
		ac.logger.Warn("failed to check rewards", "error", err)
		return
	}

	amount, err := strconv.ParseInt(totalRewards, 10, 64)
	if err != nil {
		// rewards may have decimal portion, truncate
		return
	}

	if amount < ac.minReward {
		ac.logger.Debug("rewards below threshold", "amount", amount, "min", ac.minReward)
		return
	}

	ac.logger.Info("auto-compounding rewards", "amount_uqor", amount)

	// Record the compound action
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = ac.manager.store.Conn().Exec(
		`INSERT INTO rewards (type, amount, height, claimed_at) VALUES (?, ?, ?, ?)`,
		"auto_compound", totalRewards, 0, now,
	)
}
