package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/db"
)

// Manager runs all telemetry collectors.
type Manager struct {
	chain  *client.Client
	store  *db.DB
	logger *slog.Logger

	collectors []Collector
	wg         sync.WaitGroup
}

// Collector defines a background data collector.
type Collector interface {
	Name() string
	Run(ctx context.Context) error
}

// NewManager creates a telemetry manager with all collectors.
func NewManager(chain *client.Client, store *db.DB, logger *slog.Logger, intervals Intervals) *Manager {
	m := &Manager{
		chain:  chain,
		store:  store,
		logger: logger,
	}

	m.collectors = []Collector{
		NewValidatorCollector(chain, store, logger, intervals.Validator),
		NewNetworkCollector(chain, store, logger, intervals.Network),
		NewBridgeCollector(chain, store, logger, intervals.Bridge),
		NewTokenomicsCollector(chain, store, logger, intervals.Tokenomics),
	}

	return m
}

// Intervals defines polling intervals for each collector.
type Intervals struct {
	Validator  time.Duration
	Network    time.Duration
	Bridge     time.Duration
	Tokenomics time.Duration
}

// Start launches all collectors.
func (m *Manager) Start(ctx context.Context) {
	for _, c := range m.collectors {
		c := c
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.logger.Info("starting telemetry collector", "name", c.Name())
			if err := c.Run(ctx); err != nil && ctx.Err() == nil {
				m.logger.Error("collector failed", "name", c.Name(), "error", err)
			}
		}()
	}
}

// Wait blocks until all collectors finish.
func (m *Manager) Wait() {
	m.wg.Wait()
}
