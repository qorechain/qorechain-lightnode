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
	signer      *cliSigner // nil when qorechaind is not available; signerNote says why
	signerNote  string
	lc          *lightclient.LightClient
	telem       *telemetry.Manager
	delegations *delegation.Manager
	autoClaim   *delegation.AutoClaimer
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

	lcdURL := cfg.APIAddr
	if lcdURL == "" {
		lcdURL = deriveLCDURL(cfg.RPCAddr)
	}

	// Chain client
	chain := client.New(cfg.RPCAddr, lcdURL)

	// Keyring backend
	keys, err := keyring.New(cfg.KeyringBackend, cfg.DataDir)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("initializing keyring: %w", err)
	}

	// The signer for on-chain messages: the qorechaind CLI pipeline that holds
	// the operator key and produces the hybrid post-quantum signature.
	signer, signerNote := newCLISigner(cfg)

	// Light client
	lc, err := lightclient.New(chain, store, logger, cfg.WitnessAddrs)
	if err != nil {
		return nil, fmt.Errorf("light client: %w", err)
	}

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

	// Auto-claim of light node rewards (opt-in; claims only, never re-delegates)
	claimInterval, err := time.ParseDuration(cfg.Delegation.CompoundInterval)
	if err != nil || claimInterval <= 0 {
		claimInterval = 1 * time.Hour
	}
	minReward, _ := strconv.ParseInt(cfg.Delegation.MinRewardClaim, 10, 64)
	if minReward <= 0 {
		minReward = 1000000 // 1 QOR
	}

	// Rebalancer
	rebalancer := delegation.NewRebalancer(chain, delMgr, cfg.Delegation.MinReputation, logger)

	d := &Daemon{
		cfg:         cfg,
		logger:      logger,
		store:       store,
		chain:       chain,
		keys:        keys,
		signer:      signer,
		signerNote:  signerNote,
		lc:          lc,
		telem:       telem,
		delegations: delMgr,
		rebalancer:  rebalancer,
	}
	if cfg.OperatorAddress != "" {
		d.autoClaim = delegation.NewAutoClaimer(chain, d, cfg.OperatorAddress, claimInterval, minReward, logger)
	}
	return d, nil
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

	// 3. Auto-claim of light node rewards, when asked for and possible
	if d.cfg.Delegation.AutoClaim || d.cfg.Delegation.AutoCompound {
		switch {
		case d.signer == nil:
			d.logger.Warn("auto-claim disabled: no signer", "reason", d.signerNote)
		case d.autoClaim == nil:
			d.logger.Warn("auto-claim disabled: operator_address is not set in config.toml")
		default:
			go func() {
				if err := d.autoClaim.Run(ctx); err != nil && ctx.Err() == nil {
					d.logger.Error("auto-claim failed", "error", err)
				}
			}()
		}
	}

	// 4. Periodic heartbeat submission
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

// SubmitLightNodeTx implements delegation.TxSubmitter on top of the signer.
func (d *Daemon) SubmitLightNodeTx(ctx context.Context, txArgs ...string) (string, error) {
	if d.signer == nil {
		return "", fmt.Errorf("no signer: %s", d.signerNote)
	}
	keyName := d.cfg.Heartbeat.KeyName
	if keyName == "" {
		keyName = d.cfg.KeyName
	}
	res, err := d.signer.Submit(ctx, keyName, d.cfg.KeyName, txArgs...)
	if err != nil {
		return "", err
	}
	return res.TxHash, nil
}

// Signer reports whether on-chain submission is possible, and why not.
func (d *Daemon) Signer() (ok bool, reason string) {
	return d.signer != nil, d.signerNote
}

// LicenceGate is what the node tells an operator whose account cannot register
// yet. The chain refuses a registration without an active lightnode_operator
// licence, so the node says so before anyone builds a transaction that would
// be refused, and says where the licence comes from.
func LicenceGate(operator string, lic client.LicenseStatus) (blocked bool, message string) {
	switch {
	case !lic.Found:
		return true, fmt.Sprintf("operator address %s has no lightnode_operator licence. "+
			"Buy one at https://dashboard.qorechain.io -> Tools -> Buy License, and enter this operator address there; "+
			"the on-chain grant follows and register works once it lands.", operator)
	case !lic.Active:
		return true, fmt.Sprintf("operator address %s holds a lightnode_operator licence that is suspended; "+
			"contact support through the dashboard before registering.", operator)
	}
	return false, ""
}

// heartbeatLoop keeps a registered node alive on chain.
//
// The chain marks a node inactive after heartbeat_interval + grace blocks
// without a heartbeat and refuses one sent sooner than heartbeat_interval
// blocks after the previous. Pacing therefore follows the chain's own record
// of the last heartbeat rather than a counter in this process, so a restart
// neither doubles up nor waits a whole interval.
//
// An unregistered node does not heartbeat; it reports, at each check, why it
// cannot: no licence (with where to get one), or licensed but not registered.
func (d *Daemon) heartbeatLoop(ctx context.Context) {
	hb := d.cfg.Heartbeat
	if !hb.Enabled {
		d.logger.Info("on-chain heartbeats disabled in config ([heartbeat] enabled = false)")
		return
	}
	if d.signer == nil {
		d.logger.Warn("on-chain heartbeats disabled: no signer", "reason", d.signerNote,
			"note", "a registered node is marked inactive without heartbeats")
		return
	}
	if d.cfg.OperatorAddress == "" {
		d.logger.Warn("on-chain heartbeats disabled: operator_address is not set in config.toml")
		return
	}

	check, err := time.ParseDuration(hb.CheckInterval)
	if err != nil || check <= 0 {
		check = 60 * time.Second
	}
	intervalBlocks := hb.IntervalBlocks
	if intervalBlocks <= 0 {
		intervalBlocks = 1000
	}

	ticker := time.NewTicker(check)
	defer ticker.Stop()

	var lastState string // last reason logged, so a steady state is not repeated every tick
	report := func(state, msg string, args ...any) {
		if state == lastState {
			return
		}
		lastState = state
		d.logger.Warn(msg, args...)
	}

	tick := func() {
		h := d.lc.LatestHeight()
		if h == 0 {
			return
		}
		registered, node, err := d.chain.LightNodeRegistration(ctx, d.cfg.OperatorAddress)
		if err != nil {
			report("query-error", "heartbeat: cannot read the node record", "error", err)
			return
		}
		if !registered {
			lic, err := d.chain.LicenseCheck(ctx, d.cfg.OperatorAddress, client.FeatureLightNodeOperator)
			if err != nil {
				report("licence-error", "heartbeat: cannot check the licence", "error", err)
				return
			}
			if blocked, msg := LicenceGate(d.cfg.OperatorAddress, lic); blocked {
				report("no-licence", "node not registered: "+msg)
				return
			}
			report("unregistered", "node not registered: licence is active, run `lightnode-sx register` and submit the printed commands",
				"operator", d.cfg.OperatorAddress)
			return
		}
		lastState = ""

		last, _ := strconv.ParseInt(node.LightNode.LastHeartbeat, 10, 64)
		if h-last < intervalBlocks {
			return
		}
		res, err := d.signer.Submit(ctx, d.heartbeatKey(), d.cfg.KeyName, "lightnode", "heartbeat")
		if err != nil {
			d.logger.Warn("heartbeat failed", "error", err)
			return
		}
		d.logger.Info("heartbeat submitted", "tx_hash", res.TxHash, "height", h)
	}

	tick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}

func (d *Daemon) heartbeatKey() string {
	if d.cfg.Heartbeat.KeyName != "" {
		return d.cfg.Heartbeat.KeyName
	}
	return d.cfg.KeyName
}

// toTCP normalizes an http(s):// RPC address to the tcp:// scheme the cosmos
// CLI expects for its --node flag.
func toTCP(addr string) string {
	if strings.HasPrefix(addr, "http://") {
		return "tcp://" + strings.TrimPrefix(addr, "http://")
	}
	if strings.HasPrefix(addr, "https://") {
		return "tcp://" + strings.TrimPrefix(addr, "https://")
	}
	return addr
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

func deriveLCDURL(rpcAddr string) string { return client.DeriveLCDURL(rpcAddr) }

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
