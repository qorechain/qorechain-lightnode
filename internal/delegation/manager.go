package delegation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/db"
)

// Manager handles delegation lifecycle operations.
type Manager struct {
	chain  *client.Client
	store  *db.DB
	logger *slog.Logger

	address    string // operator bech32 address
	validators []string
	weights    []int
}

// New creates a delegation manager.
func New(chain *client.Client, store *db.DB, logger *slog.Logger, address string) *Manager {
	return &Manager{
		chain:   chain,
		store:   store,
		logger:  logger,
		address: address,
	}
}

// SetSplit configures multi-validator delegation split.
func (m *Manager) SetSplit(validators []string, weights []int) error {
	if len(validators) != len(weights) {
		return fmt.Errorf("validators and weights must have same length")
	}
	total := 0
	for _, w := range weights {
		total += w
	}
	if total == 0 {
		return fmt.Errorf("weights must sum to > 0")
	}
	m.validators = validators
	m.weights = weights
	return nil
}

// SyncDelegations fetches current delegations from chain and stores locally.
func (m *Manager) SyncDelegations(ctx context.Context) error {
	delegations, err := m.chain.Delegations(ctx, m.address)
	if err != nil {
		return fmt.Errorf("fetching delegations: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, d := range delegations {
		_, err := m.store.Conn().Exec(
			`INSERT OR REPLACE INTO delegations (validator, amount, updated_at) VALUES (?, ?, ?)`,
			d.Delegation.ValidatorAddress, d.Balance.Amount, now,
		)
		if err != nil {
			m.logger.Warn("failed to store delegation", "validator", d.Delegation.ValidatorAddress, "error", err)
		}
	}
	m.logger.Debug("synced delegations", "count", len(delegations))
	return nil
}

// GetDelegations returns locally stored delegations.
func (m *Manager) GetDelegations(ctx context.Context) ([]DelegationInfo, error) {
	rows, err := m.store.Conn().QueryContext(ctx,
		`SELECT validator, amount, updated_at FROM delegations ORDER BY amount DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var delegations []DelegationInfo
	for rows.Next() {
		var d DelegationInfo
		if err := rows.Scan(&d.Validator, &d.Amount, &d.UpdatedAt); err != nil {
			return nil, err
		}
		delegations = append(delegations, d)
	}
	return delegations, nil
}

// DelegationInfo represents a delegation entry.
type DelegationInfo struct {
	Validator string
	Amount    string
	UpdatedAt string
}

// GetTotalRewards returns total pending rewards from chain.
func (m *Manager) GetTotalRewards(ctx context.Context) (string, error) {
	rewards, err := m.chain.Rewards(ctx, m.address)
	if err != nil {
		return "0", fmt.Errorf("fetching rewards: %w", err)
	}
	for _, t := range rewards.Total {
		if t.Denom == "uqor" {
			return t.Amount, nil
		}
	}
	return "0", nil
}
