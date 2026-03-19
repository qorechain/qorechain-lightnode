package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
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

	writeJSON(w, http.StatusOK, resp)
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

// HandleRewards returns total pending rewards.
func (a *API) HandleRewards(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	total, err := a.deleg.GetTotalRewards(ctx)
	if err != nil {
		// Fall back to last known rewards from DB
		a.logger.Debug("live rewards fetch failed, using cache", "error", err)
		cached := a.cachedRewards()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"total_rewards": cached,
			"denom":         "uqor",
			"cached":        true,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_rewards": total,
		"denom":         "uqor",
		"cached":        false,
	})
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
func (a *API) HandleSettings(w http.ResponseWriter, r *http.Request) {
	// Return only safe configuration values (no keys, passwords, etc.)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"node_type":    a.cfg.NodeType,
		"version":      a.cfg.Version,
		"chain_id":     a.cfg.ChainID,
		"rpc_addr":     a.cfg.RPCAddr,
		"data_dir":     a.cfg.DataDir,
		"log_level":    a.cfg.LogLevel,
		"dashboard":    a.cfg.Dashboard,
		"telemetry":    a.cfg.Telemetry,
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
