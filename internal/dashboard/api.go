package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/db"
	"github.com/qorechain/qorechain-lightnode/internal/delegation"
	"github.com/qorechain/qorechain-lightnode/internal/lightclient"
)

// API provides JSON handlers for the dashboard REST endpoints.
type API struct {
	chain  *client.Client
	store  *db.DB
	lc     *lightclient.LightClient
	deleg  *delegation.Manager
	cfg    config.Config
	logger *slog.Logger
}

// NewAPI creates a new API handler set.
func NewAPI(
	chain *client.Client,
	store *db.DB,
	lc *lightclient.LightClient,
	deleg *delegation.Manager,
	cfg config.Config,
	logger *slog.Logger,
) *API {
	return &API{
		chain:  chain,
		store:  store,
		lc:     lc,
		deleg:  deleg,
		cfg:    cfg,
		logger: logger,
	}
}

// writeJSON encodes v as JSON and writes it to w.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// HandleStatus returns the light node sync status.
func (a *API) HandleStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"syncing":       a.lc.IsSyncing(),
		"latest_height": a.lc.LatestHeight(),
		"node_type":     a.cfg.NodeType,
		"version":       a.cfg.Version,
		"chain_id":      a.cfg.ChainID,
	}

	// Attempt to enrich with live node status
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if status, err := a.chain.NodeStatus(ctx); err == nil {
		resp["network"] = status.Result.NodeInfo.Network
		resp["node_version"] = status.Result.NodeInfo.Version
		resp["catching_up"] = status.Result.SyncInfo.CatchingUp
		resp["latest_block_time"] = status.Result.SyncInfo.LatestBlockTime
	}

	// heartbeat_age_blocks is the single number that decides whether this node
	// is about to stop earning: the chain marks a node inactive once it exceeds
	// heartbeat_interval + heartbeat_grace_period without a beat, and an
	// inactive node falls under min_uptime_for_rewards and earns nothing.
	//
	// QoreX's Light Node screen declares this field (NodeStatus.heartbeatAgeBlocks)
	// and has never received it, so the companion app could not show an operator
	// the one thing they need to watch.
	a.addHeartbeatAge(ctx, resp)

	writeJSON(w, http.StatusOK, resp)
}

// addHeartbeatAge fills in heartbeat_age_blocks and the deadline it is measured
// against. Every part is best-effort: a node whose operator address is not set,
// or which is not registered, simply has no heartbeat age, and the rest of the
// status response must still be served.
func (a *API) addHeartbeatAge(ctx context.Context, resp map[string]interface{}) {
	if a.cfg.OperatorAddress == "" {
		return
	}
	registered, node, err := a.chain.LightNodeRegistration(ctx, a.cfg.OperatorAddress)
	if err != nil || !registered {
		return
	}
	resp["registered"] = true
	resp["node_status"] = node.LightNode.Status

	last, err := strconv.ParseInt(node.LightNode.LastHeartbeat, 10, 64)
	if err != nil || last <= 0 {
		return
	}
	height := a.lc.LatestHeight()
	if height <= 0 {
		if st, err := a.chain.NodeStatus(ctx); err == nil {
			height, _ = strconv.ParseInt(st.Result.SyncInfo.LatestBlockHeight, 10, 64)
		}
	}
	if height < last {
		return
	}
	resp["heartbeat_age_blocks"] = height - last

	// The deadline comes from the chain, not from a constant here, because both
	// params are governable.
	if p, err := a.chain.LightNodeParams(ctx); err == nil {
		interval, e1 := strconv.ParseInt(p.HeartbeatInterval, 10, 64)
		grace, e2 := strconv.ParseInt(p.HeartbeatGracePeriod, 10, 64)
		if e1 == nil && e2 == nil && interval > 0 {
			resp["heartbeat_interval_blocks"] = interval
			resp["heartbeat_deadline_blocks"] = interval + grace
			resp["heartbeat_overdue"] = height-last > interval+grace
		}
	}

	// Uptime is total/expected, and it is what the reward gate compares against
	// min_uptime_for_rewards.
	total, e1 := strconv.ParseFloat(node.LightNode.TotalHeartbeats, 64)
	expected, e2 := strconv.ParseFloat(node.LightNode.ExpectedHeartbeats, 64)
	if e1 == nil && e2 == nil && expected > 0 {
		resp["uptime"] = total / expected
	}
}

// HandleValidators returns the active validator set.
func (a *API) HandleValidators(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	validators, err := a.chain.Validators(ctx)
	if err != nil {
		// Fall back to cached telemetry data
		a.logger.Debug("live validator fetch failed, using cache", "error", err)
		cached, dbErr := a.cachedValidators()
		if dbErr != nil {
			writeError(w, http.StatusServiceUnavailable, "validator data unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"validators": cached,
			"cached":     true,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"validators": validators,
		"cached":     false,
	})
}

// HandleDelegation returns the operator's delegations.
func (a *API) HandleDelegation(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	delegations, err := a.deleg.GetDelegations(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch delegations")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"delegations": delegations,
	})
}

// HandleRewards reports what this node has earned, with the light-node fee
// share and the staking yield kept apart.
//
// They were conflated until 2026-10-14, and it mattered: this handler returned
// the delegator's STAKING rewards from /cosmos/distribution, while QoreX reads
// the same field as "Accrued 3% fee-share rewards" and renders it as light-node
// earnings (NodeRewards.totalUqor). An operator was shown their staking yield
// labelled as light-node income - a number that moves for reasons that have
// nothing to do with running a node, and which is non-zero even when the node
// has earned nothing at all.
//
// total_rewards is therefore now the light-node accrual, matching the meaning
// QoreX documents, and staking_rewards carries the distribution figure under
// its own name.
func (a *API) HandleRewards(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp := map[string]interface{}{
		"denom": "uqor",
		// auto_compound is the historical name of the setting QoreX reads; the
		// node claims rewards into the operator wallet and has never
		// re-delegated them, so auto_claim is the truthful name and both are
		// reported rather than leaving QoreX to default it to false.
		"auto_claim":    a.cfg.Delegation.AutoClaim,
		"auto_compound": a.cfg.Delegation.AutoClaim,
	}
	if ts := a.lastClaimedAt(); ts != "" {
		resp["last_claimed_at"] = ts
	}

	// The light-node fee share, from the lightnode module.
	feeShare, feeErr := a.lightNodeAccrued(ctx)
	if feeErr == nil {
		resp["total_rewards"] = feeShare
		resp["cached"] = false
	} else {
		cached := a.cachedRewards()
		resp["total_rewards"] = cached
		resp["cached"] = true
		a.logger.Debug("light-node accrual unavailable, using cache", "error", feeErr)
	}

	// The staking yield on the operator's bonded stake, under its own name.
	if staking, err := a.deleg.GetTotalRewards(ctx); err == nil {
		resp["staking_rewards"] = staking
	} else {
		a.logger.Debug("staking rewards fetch failed", "error", err)
	}

	writeJSON(w, http.StatusOK, resp)
}

// lightNodeAccrued returns the uqor this node has accrued from the light-node
// fee share and not yet claimed. An unregistered node has accrued nothing,
// which is an answer rather than an error.
func (a *API) lightNodeAccrued(ctx context.Context) (string, error) {
	if a.cfg.OperatorAddress == "" {
		return "0", nil
	}
	registered, node, err := a.chain.LightNodeRegistration(ctx, a.cfg.OperatorAddress)
	if err != nil {
		return "", err
	}
	if !registered || node.LightNode.AccumulatedRewards == "" {
		return "0", nil
	}
	return node.LightNode.AccumulatedRewards, nil
}

// lastClaimedAt is the timestamp of the most recent recorded claim, or "".
func (a *API) lastClaimedAt() string {
	var ts sql.NullString
	_ = a.store.Conn().QueryRow(
		`SELECT claimed_at FROM rewards WHERE claimed_at IS NOT NULL ORDER BY id DESC LIMIT 1`,
	).Scan(&ts)
	if ts.Valid {
		return ts.String
	}
	return ""
}

// HandleRewardPool reports how the chain's light-node reward pool divides up,
// and what the per-node ceiling is actually worth per month at the live block
// cadence.
//
// Without this an operator cannot tell earnable rewards from the module
// account's balance. Most of that balance is Reserved - it accumulated while no
// node was eligible, nobody earned it, and only governance can move it - so
// reading the balance as "rewards waiting for me" overstates it by orders of
// magnitude. On mainnet today the whole 3.16M QOR balance is reserved and
// distributable is zero.
//
// The monthly figure is derived, not quoted: the chain's ceiling is per block,
// so its value per month depends on a cadence that drifts.
func (a *API) HandleRewardPool(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pool, err := a.chain.RewardPool(ctx)
	if err != nil {
		if errors.Is(err, client.ErrEndpointUnavailable) {
			writeError(w, http.StatusServiceUnavailable,
				"this chain predates the light-node reward pool (upgrade v3.1.98)")
			return
		}
		writeError(w, http.StatusServiceUnavailable, "reward pool unavailable")
		return
	}

	resp := map[string]interface{}{
		"balance":                       pool.Balance,
		"reserved":                      pool.Reserved,
		"payable":                       pool.Payable,
		"outstanding":                   pool.Outstanding,
		"distributable":                 pool.Distributable,
		"initialized":                   pool.Initialized,
		"max_reward_per_block_per_node": pool.MaxRewardPerBlockPerNode,
		"denom":                         "uqor",
	}

	// Both bounds the chain may be applying. From v3.2.3 the chain states a
	// monthly bound, but fills it even while the switch that makes it the one
	// in force is off, and the response does not say which applies. So
	// neither is presented as "the" ceiling: each comes with its condition.
	const window = int64(20000)
	bt, btErr := a.chain.BlockTimeSeconds(ctx, window)
	if btErr != nil {
		a.logger.Debug("cadence measurement failed; per-block bound not converted", "error", btErr)
	}
	stated, hasStated, perBlock, hasPerBlock := pool.Ceilings(bt)
	if hasStated {
		resp["max_reward_per_month_per_node"] = strconv.FormatFloat(stated, 'f', 0, 64)
		resp["monthly_bound_applies"] = "from the lightnode-monthly-cap switch (chain v3.2.3); " +
			"until then, and for the first settlement after it, the per-block bound applies"
	}
	if hasPerBlock {
		resp["per_block_bound_as_monthly"] = strconv.FormatFloat(perBlock, 'f', 0, 64)
		resp["block_time_seconds"] = bt
		resp["block_time_sample_blocks"] = window
	}

	// Which of the two is in force, when the chain can tell us. The switch
	// turns on with the "v3.2.3" upgrade and never turns off, so an applied
	// plan is definitive. When it cannot be asked, both figures stand with
	// their conditions and neither is singled out.
	if active, known := a.chain.MonthlyCapActive(ctx); known {
		resp["monthly_cap_active"] = active
		switch {
		case active && hasStated:
			resp["bound_in_force_per_month"] = strconv.FormatFloat(stated, 'f', 0, 64)
		case !active && hasPerBlock:
			resp["bound_in_force_per_month"] = strconv.FormatFloat(perBlock, 'f', 0, 64)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleNetwork returns recent headers and network telemetry.
func (a *API) HandleNetwork(w http.ResponseWriter, r *http.Request) {
	headers, err := a.lc.RecentHeaders(50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch headers")
		return
	}

	telemetry, _ := a.cachedNetworkTelemetry()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"headers":   headers,
		"telemetry": telemetry,
	})
}

// HandleBridge returns bridge connection statuses from cached telemetry.
func (a *API) HandleBridge(w http.ResponseWriter, r *http.Request) {
	bridges, err := a.cachedBridgeTelemetry()
	if err != nil {
		// Try live query
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		live, liveErr := a.chain.BridgeStatus(ctx)
		if liveErr != nil {
			writeError(w, http.StatusServiceUnavailable, "bridge data unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"connections": live.Connections,
			"cached":      false,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"connections": bridges,
		"cached":      true,
	})
}

// HandleTokenomics returns tokenomics data from cached telemetry.
func (a *API) HandleTokenomics(w http.ResponseWriter, r *http.Request) {
	tok, err := a.cachedTokenomics()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "tokenomics data unavailable")
		return
	}
	writeJSON(w, http.StatusOK, tok)
}

// HandleSettings returns sanitized node configuration.
//
// `local_only` is true when no chain RPC endpoint is configured — the
// dashboard uses this to show an onboarding banner instead of waiting
// forever on a sync that will never arrive.
func (a *API) HandleSettings(w http.ResponseWriter, r *http.Request) {
	// Return only safe configuration values (no keys, passwords, etc.)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"node_type":  a.cfg.NodeType,
		"version":    a.cfg.Version,
		"chain_id":   a.cfg.ChainID,
		"rpc_addr":   a.cfg.RPCAddr,
		"local_only": a.cfg.RPCAddr == "",
		"data_dir":   a.cfg.DataDir,
		"log_level":  a.cfg.LogLevel,
		"dashboard":  a.cfg.Dashboard,
		"telemetry":  a.cfg.Telemetry,
		"delegation": map[string]interface{}{
			"auto_compound":     a.cfg.Delegation.AutoCompound,
			"compound_interval": a.cfg.Delegation.CompoundInterval,
			"validators":        a.cfg.Delegation.Validators,
			"rebalance_enabled": a.cfg.Delegation.RebalanceEnabled,
			"min_reputation":    a.cfg.Delegation.MinReputation,
		},
	})
}

// --- DB cache helpers ---

type cachedValidator struct {
	Address             string  `json:"address"`
	Moniker             string  `json:"moniker"`
	Uptime              float64 `json:"uptime"`
	ReputationComposite float64 `json:"reputation_composite"`
	Pool                string  `json:"pool"`
	Jailed              bool    `json:"jailed"`
	UpdatedAt           string  `json:"updated_at"`
}

func (a *API) cachedValidators() ([]cachedValidator, error) {
	rows, err := a.store.Conn().Query(
		`SELECT address, moniker, uptime, reputation_composite, pool, jailed, updated_at
		 FROM telemetry_validators ORDER BY reputation_composite DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vals []cachedValidator
	for rows.Next() {
		var v cachedValidator
		var jailed int
		if err := rows.Scan(&v.Address, &v.Moniker, &v.Uptime, &v.ReputationComposite, &v.Pool, &jailed, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.Jailed = jailed != 0
		vals = append(vals, v)
	}
	return vals, rows.Err()
}

func (a *API) cachedRewards() string {
	var amount sql.NullString
	_ = a.store.Conn().QueryRow(
		`SELECT amount FROM rewards ORDER BY id DESC LIMIT 1`,
	).Scan(&amount)
	if amount.Valid {
		return amount.String
	}
	return "0"
}

type networkTelemetryEntry struct {
	Height         int64   `json:"height"`
	Timestamp      string  `json:"timestamp"`
	BlockTimeMs    int     `json:"block_time_ms"`
	TxCount        int     `json:"tx_count"`
	ValidatorCount int     `json:"validator_count"`
	ActiveSetSize  int     `json:"active_set_size"`
	TotalStake     string  `json:"total_stake"`
	InflationRate  string  `json:"inflation_rate"`
	GasPrice       string  `json:"gas_price"`
	RLBlockSize    int     `json:"rl_block_size"`
	RLGasLimit     int     `json:"rl_gas_limit"`
	RLReward       float64 `json:"rl_reward"`
	RLEpoch        int     `json:"rl_epoch"`
}

func (a *API) cachedNetworkTelemetry() ([]networkTelemetryEntry, error) {
	rows, err := a.store.Conn().Query(
		`SELECT height, timestamp, COALESCE(block_time_ms, 0), COALESCE(tx_count, 0),
		        COALESCE(validator_count, 0), COALESCE(active_set_size, 0),
		        COALESCE(total_stake, ''), COALESCE(inflation_rate, ''),
		        COALESCE(gas_price, ''), COALESCE(rl_block_size, 0),
		        COALESCE(rl_gas_limit, 0), COALESCE(rl_reward, 0), COALESCE(rl_epoch, 0)
		 FROM telemetry_network ORDER BY height DESC LIMIT 100`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []networkTelemetryEntry
	for rows.Next() {
		var e networkTelemetryEntry
		if err := rows.Scan(
			&e.Height, &e.Timestamp, &e.BlockTimeMs, &e.TxCount,
			&e.ValidatorCount, &e.ActiveSetSize, &e.TotalStake,
			&e.InflationRate, &e.GasPrice, &e.RLBlockSize,
			&e.RLGasLimit, &e.RLReward, &e.RLEpoch,
		); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

type bridgeTelemetryEntry struct {
	Chain            string `json:"chain"`
	ChainType        string `json:"chain_type"`
	Status           string `json:"status"`
	PendingTransfers int    `json:"pending_transfers"`
	UpdatedAt        string `json:"updated_at"`
}

func (a *API) cachedBridgeTelemetry() ([]bridgeTelemetryEntry, error) {
	rows, err := a.store.Conn().Query(
		`SELECT chain, chain_type, status, pending_transfers, updated_at
		 FROM telemetry_bridge ORDER BY chain`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []bridgeTelemetryEntry
	for rows.Next() {
		var e bridgeTelemetryEntry
		if err := rows.Scan(&e.Chain, &e.ChainType, &e.Status, &e.PendingTransfers, &e.UpdatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, sql.ErrNoRows
	}
	return entries, rows.Err()
}

type tokenomicsEntry struct {
	Height        int64  `json:"height"`
	TotalBurned   string `json:"total_burned"`
	InflationRate string `json:"inflation_rate"`
	XqoreTVL      string `json:"xqore_tvl"`
	TotalSupply   string `json:"total_supply"`
	StakingRatio  string `json:"staking_ratio"`
	UpdatedAt     string `json:"updated_at"`
}

func (a *API) cachedTokenomics() (*tokenomicsEntry, error) {
	var e tokenomicsEntry
	err := a.store.Conn().QueryRow(
		`SELECT height, COALESCE(total_burned, ''), COALESCE(inflation_rate, ''),
		        COALESCE(xqore_tvl, ''), COALESCE(total_supply, ''),
		        COALESCE(staking_ratio, ''), updated_at
		 FROM telemetry_tokenomics ORDER BY height DESC LIMIT 1`,
	).Scan(&e.Height, &e.TotalBurned, &e.InflationRate, &e.XqoreTVL,
		&e.TotalSupply, &e.StakingRatio, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}
