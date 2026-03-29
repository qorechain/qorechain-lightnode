package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/db"
	"github.com/qorechain/qorechain-lightnode/internal/delegation"
	"github.com/qorechain/qorechain-lightnode/internal/keyring"
	"github.com/qorechain/qorechain-lightnode/internal/lightclient"
	"github.com/qorechain/qorechain-lightnode/internal/telemetry"
)

const (
	lightnodeMsgTypeHeartbeat    = "/qorechain.lightnode.v1.MsgHeartbeat"
	lightnodeMsgTypeClaimRewards = "/qorechain.lightnode.v1.MsgClaimLightNodeRewards"
)

// Daemon orchestrates the light node subsystems.
type Daemon struct {
	cfg    config.Config
	logger *slog.Logger

	store       *db.DB
	chain       *client.Client
	keys        keyring.Backend
	txBuilder   *client.TxBuilder
	lc          *lightclient.LightClient
	telem       *telemetry.Manager
	delegations *delegation.Manager
	autoComp    *delegation.AutoCompounder
	rebalancer  *delegation.Rebalancer
}

// New initializes all subsystems and returns a ready daemon.
func New(cfg config.Config) (*Daemon, error) {
	logger := buildLogger(cfg.LogLevel, cfg.LogFormat)

	// Open local database
	store, err := db.Open(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Derive LCD URL from RPC address (replace RPC port with REST port)
	lcdURL := deriveLCDURL(cfg.RPCAddr)

	// Chain client
	chain := client.New(cfg.RPCAddr, lcdURL)

	// Keyring backend
	keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("initializing keyring: %w", err)
	}

	// Transaction builder for submitting TXs
	txBuilder := client.NewTxBuilder(chain, keys, cfg.KeyName, cfg.ChainID)

	// Light client
	lc := lightclient.New(chain, store, logger)

	// Telemetry intervals
	intervals, err := parseIntervals(cfg.Telemetry)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("parsing telemetry intervals: %w", err)
	}
	telem := telemetry.NewManager(chain, store, logger, intervals)

	// Delegation manager — resolve operator address from keyring
	operatorAddr := ""
	if info, err := keys.Get(cfg.KeyName); err == nil {
		operatorAddr = info.Address
	}
	delMgr := delegation.New(chain, store, logger, operatorAddr)

	// Configure multi-validator split if provided
	if len(cfg.Delegation.Validators) > 0 {
		weights := cfg.Delegation.SplitWeights
		if len(weights) == 0 {
			// Equal weight for all validators
			weights = make([]int, len(cfg.Delegation.Validators))
			for i := range weights {
				weights[i] = 1
			}
		}
		_ = delMgr.SetSplit(cfg.Delegation.Validators, weights)
	}

	// Auto-compounder
	compoundInterval, err := time.ParseDuration(cfg.Delegation.CompoundInterval)
	if err != nil {
		compoundInterval = 1 * time.Hour
	}
	minReward, _ := strconv.ParseInt(cfg.Delegation.MinRewardClaim, 10, 64)
	if minReward <= 0 {
		minReward = 1000000 // 1 QOR
	}
	autoComp := delegation.NewAutoCompounder(delMgr, txBuilder, compoundInterval, minReward, logger)

	// Rebalancer
	rebalancer := delegation.NewRebalancer(chain, delMgr, cfg.Delegation.MinReputation, logger)

	return &Daemon{
		cfg:         cfg,
		logger:      logger,
		store:       store,
		chain:       chain,
		keys:        keys,
		txBuilder:   txBuilder,
		lc:          lc,
		telem:       telem,
		delegations: delMgr,
		autoComp:    autoComp,
		rebalancer:  rebalancer,
	}, nil
}

// Run starts all subsystems and blocks until the context is cancelled or a
// termination signal is received.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-sigCh:
			d.logger.Info("received signal, shutting down", "signal", sig)
			cancel()
		case <-ctx.Done():
		}
	}()

	d.logger.Info("starting QoreChain light node daemon",
		"node_type", d.cfg.NodeType,
		"version", d.cfg.Version,
		"chain_id", d.cfg.ChainID,
		"rpc", d.cfg.RPCAddr,
	)

	// 1. Start light client header sync
	go func() {
		if err := d.lc.Start(ctx); err != nil && ctx.Err() == nil {
			d.logger.Error("light client sync failed", "error", err)
		}
	}()

	// 2. Start telemetry collectors
	if d.cfg.Telemetry.Enabled {
		d.telem.Start(ctx)
	}

	// 3. Start auto-compounder
	if d.cfg.Delegation.AutoCompound {
		go func() {
			if err := d.autoComp.Run(ctx); err != nil && ctx.Err() == nil {
				d.logger.Error("auto-compounder failed", "error", err)
			}
		}()
	}

	// 4. Periodic heartbeat submission (log-only placeholder)
	go d.heartbeatLoop(ctx)

	// 5. Periodic delegation sync
	go d.delegationSyncLoop(ctx)

	// Block until shutdown
	<-ctx.Done()
	d.logger.Info("daemon shutting down")

	// Wait for telemetry collectors to finish
	if d.cfg.Telemetry.Enabled {
		d.telem.Wait()
	}

	return nil
}

// Close releases daemon resources.
func (d *Daemon) Close() error {
	return d.store.Close()
}

// Store returns the local database.
func (d *Daemon) Store() *db.DB {
	return d.store
}

// Chain returns the chain client.
func (d *Daemon) Chain() *client.Client {
	return d.chain
}

// Keys returns the keyring backend.
func (d *Daemon) Keys() keyring.Backend {
	return d.keys
}

// LightClient returns the light client instance.
func (d *Daemon) LightClient() *lightclient.LightClient {
	return d.lc
}

// Delegations returns the delegation manager.
func (d *Daemon) Delegations() *delegation.Manager {
	return d.delegations
}

// Cfg returns the daemon configuration.
func (d *Daemon) Cfg() config.Config {
	return d.cfg
}

// Logger returns the daemon logger.
func (d *Daemon) Logger() *slog.Logger {
	return d.logger
}

// heartbeatLoop submits periodic heartbeat transactions to prove node liveness.
func (d *Daemon) heartbeatLoop(ctx context.Context) {
	// Default heartbeat every 100 blocks (~10 min at 6s blocks)
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	// Get operator address
	keyInfo, err := d.keys.Get(d.cfg.KeyName)
	if err != nil {
		d.logger.Error("heartbeat loop: cannot get operator key", "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			height := d.lc.LatestHeight()
			d.logger.Info("submitting heartbeat", "latest_height", height)

			msg := client.MsgHeartbeat{
				Type:     lightnodeMsgTypeHeartbeat,
				Operator: keyInfo.Address,
			}

			resp, err := d.txBuilder.BuildAndBroadcast(ctx, msg)
			if err != nil {
				d.logger.Warn("heartbeat tx failed", "error", err)
				continue
			}

			if resp.TxResponse.Code != 0 {
				d.logger.Warn("heartbeat tx rejected",
					"code", resp.TxResponse.Code,
					"log", resp.TxResponse.RawLog,
				)
				continue
			}

			d.logger.Info("heartbeat submitted",
				"tx_hash", resp.TxResponse.TxHash,
				"height", resp.TxResponse.Height,
			)
		}
	}
}

// delegationSyncLoop periodically syncs delegation state from chain.
func (d *Daemon) delegationSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.delegations.SyncDelegations(ctx); err != nil {
				d.logger.Warn("delegation sync failed", "error", err)
			}
			// Check rebalance alerts if enabled
			if d.cfg.Delegation.RebalanceEnabled {
				alerts, err := d.rebalancer.Check(ctx)
				if err != nil {
					d.logger.Warn("rebalance check failed", "error", err)
				}
				for _, a := range alerts {
					d.logger.Warn("rebalance alert",
						"validator", a.Validator,
						"reputation", a.Reputation,
						"reason", a.Reason,
					)
				}
			}
		}
	}
}

// deriveLCDURL converts an RPC URL to a REST/LCD URL by replacing the
// RPC port (26657) with the LCD port (1317).
func deriveLCDURL(rpcAddr string) string {
	return strings.Replace(rpcAddr, "26657", "1317", 1)
}

// parseIntervals converts config string durations to telemetry intervals.
func parseIntervals(cfg config.TelemetryConfig) (telemetry.Intervals, error) {
	valInt, err := time.ParseDuration(cfg.ValidatorInterval)
	if err != nil {
		return telemetry.Intervals{}, fmt.Errorf("validator_interval: %w", err)
	}
	netInt, err := time.ParseDuration(cfg.NetworkInterval)
	if err != nil {
		return telemetry.Intervals{}, fmt.Errorf("network_interval: %w", err)
	}
	brInt, err := time.ParseDuration(cfg.BridgeInterval)
	if err != nil {
		return telemetry.Intervals{}, fmt.Errorf("bridge_interval: %w", err)
	}
	tokInt, err := time.ParseDuration(cfg.TokenomicsInterval)
	if err != nil {
		return telemetry.Intervals{}, fmt.Errorf("tokenomics_interval: %w", err)
	}
	return telemetry.Intervals{
		Validator:  valInt,
		Network:    netInt,
		Bridge:     brInt,
		Tokenomics: tokInt,
	}, nil
}

// buildLogger creates a structured logger based on config.
func buildLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	if strings.ToLower(format) == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}
