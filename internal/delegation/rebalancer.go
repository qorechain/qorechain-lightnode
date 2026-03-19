package delegation

import (
	"context"
	"log/slog"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
)

// Rebalancer monitors validator reputation and suggests redelegation.
type Rebalancer struct {
	chain         *client.Client
	manager       *Manager
	minReputation float64
	logger        *slog.Logger
}

// NewRebalancer creates a reputation-based rebalancer.
func NewRebalancer(chain *client.Client, manager *Manager, minReputation float64, logger *slog.Logger) *Rebalancer {
	return &Rebalancer{
		chain:         chain,
		manager:       manager,
		minReputation: minReputation,
		logger:        logger,
	}
}

// RebalanceAlert describes a validator that needs redelegation.
type RebalanceAlert struct {
	Validator  string
	Reputation float64
	Reason     string
	Time       time.Time
}

// Check evaluates all delegated validators and returns alerts for any below threshold.
func (r *Rebalancer) Check(ctx context.Context) ([]RebalanceAlert, error) {
	delegations, err := r.manager.GetDelegations(ctx)
	if err != nil {
		return nil, err
	}

	var alerts []RebalanceAlert
	for _, d := range delegations {
		rep, err := r.chain.ReputationScore(ctx, d.Validator)
		if err != nil {
			r.logger.Warn("failed to check reputation", "validator", d.Validator, "error", err)
			continue
		}

		if rep.Score.Composite < r.minReputation {
			alerts = append(alerts, RebalanceAlert{
				Validator:  d.Validator,
				Reputation: rep.Score.Composite,
				Reason:     "reputation below threshold",
				Time:       time.Now(),
			})
			r.logger.Warn("validator below reputation threshold",
				"validator", d.Validator,
				"reputation", rep.Score.Composite,
				"threshold", r.minReputation,
			)
		}
	}
	return alerts, nil
}
