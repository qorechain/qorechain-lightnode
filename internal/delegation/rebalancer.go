package delegation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
)

// Rebalancer watches the validators the operator delegates to and says when the
// stake should move.
//
// The chain pays a light node only while the operator's stake on BONDED
// validators reaches min_delegated_stake, and it says nothing when that stops
// being true: a validator that is jailed or leaves the active set takes its
// delegations out of the count, and the node keeps heartbeating for nothing.
// That is the main thing this checks.
type Rebalancer struct {
	chain         *client.Client
	manager       *Manager
	minReputation float64
	logger        *slog.Logger

	// reputationOff is set once the chain answers 501 for the reputation
	// score: no network publishes a per-validator score yet, and asking every
	// five minutes only filled the log with the same warning.
	reputationOff bool
}

// NewRebalancer creates a rebalancer.
func NewRebalancer(chain *client.Client, manager *Manager, minReputation float64, logger *slog.Logger) *Rebalancer {
	return &Rebalancer{
		chain:         chain,
		manager:       manager,
		minReputation: minReputation,
		logger:        logger,
	}
}

// Alert reasons. Each is a stable key, so the daemon can tell a new alert from
// one it already reported.
const (
	ReasonStakeBelowMinimum = "stake below the light node minimum"
	ReasonValidatorJailed   = "validator jailed"
	ReasonValidatorUnbonded = "validator not in the active set"
	ReasonLowReputation     = "reputation below threshold"
)

// RebalanceAlert describes something the operator should act on.
type RebalanceAlert struct {
	Validator  string // empty for the stake alert, which concerns the account
	Reputation float64
	Reason     string
	Detail     string
	Time       time.Time
}

// Key identifies the alert across checks.
func (a RebalanceAlert) Key() string { return a.Reason + "|" + a.Validator }

// Check reads the operator's delegations from the chain and returns what needs
// attention.
func (r *Rebalancer) Check(ctx context.Context) ([]RebalanceAlert, error) {
	// Live delegations, not the local table: SyncDelegations never deletes a
	// row, so a delegation that was fully undelegated would still show there.
	dels, err := r.chain.Delegations(ctx, r.manager.address)
	if err != nil {
		return nil, fmt.Errorf("reading delegations: %w", err)
	}
	bonded, err := r.chain.Validators(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the bonded validator set: %w", err)
	}
	isBonded := make(map[string]bool, len(bonded))
	for _, v := range bonded {
		isBonded[v.OperatorAddress] = true
	}

	now := time.Now()
	var alerts []RebalanceAlert

	for _, d := range dels {
		val := d.Delegation.ValidatorAddress
		if isBonded[val] {
			continue
		}
		reason, detail := ReasonValidatorUnbonded, d.Balance.Amount+" uqor delegated here does not count toward the light node stake"
		if v, err := r.chain.Validator(ctx, val); err == nil && v.Jailed {
			reason = ReasonValidatorJailed
		} else if err == nil {
			detail += " (status " + v.Status + ")"
		}
		alerts = append(alerts, RebalanceAlert{Validator: val, Reason: reason, Detail: detail + "; redelegate to a bonded validator", Time: now})
	}

	if params, err := r.chain.LightNodeParams(ctx); err == nil {
		if need, ok := new(big.Int).SetString(params.MinDelegatedStake, 10); ok {
			if have := client.BondedStake(dels, bonded); have.Cmp(need) < 0 {
				alerts = append(alerts, RebalanceAlert{
					Reason: ReasonStakeBelowMinimum,
					Detail: fmt.Sprintf("%s uqor on bonded validators, %s required: the chain pays this node nothing until it is back above", have, need),
					Time:   now,
				})
			}
		}
	}

	if !r.reputationOff {
		alerts = append(alerts, r.checkReputation(ctx, dels, now)...)
	}
	return alerts, nil
}

func (r *Rebalancer) checkReputation(ctx context.Context, dels []client.DelegationEntry, now time.Time) []RebalanceAlert {
	var alerts []RebalanceAlert
	for _, d := range dels {
		val := d.Delegation.ValidatorAddress
		rep, err := r.chain.ReputationScore(ctx, val)
		if errors.Is(err, client.ErrEndpointUnavailable) {
			r.reputationOff = true
			r.logger.Info("this network does not publish validator reputation scores; reputation alerts are off, validator status and stake alerts stay on")
			return nil
		}
		if err != nil {
			r.logger.Warn("failed to check reputation", "validator", val, "error", err)
			continue
		}
		if rep.Score.Composite < r.minReputation {
			alerts = append(alerts, RebalanceAlert{
				Validator:  val,
				Reputation: rep.Score.Composite,
				Reason:     ReasonLowReputation,
				Detail:     fmt.Sprintf("reputation %.3f, threshold %.3f", rep.Score.Composite, r.minReputation),
				Time:       now,
			})
		}
	}
	return alerts
}
