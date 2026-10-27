// Package mobile is the Android-facing surface of the light node.
//
// It is bound into an AAR with `gomobile bind`, so its API obeys gomobile's
// type rules: exported functions take and return strings, integers, booleans,
// []byte, error and pointers to exported structs - no maps, no slices of
// structs, no generics. Anything with real structure crosses as a JSON string,
// which keeps the Kotlin side to one decode and leaves the shape owned by this
// repo rather than duplicated by hand on the other side.
//
// # What runs on the phone, and what cannot
//
// The light client itself ports cleanly. The whole node is CGO-free - circl for
// ML-DSA-87, modernc.org/sqlite for storage - and cross-compiles to
// android/arm64 unchanged, so header verification and witness cross-checking
// run in-process on the device.
//
// Submitting transactions does not port. On desktop the daemon signs by driving
// the bundled `qorechaind` CLI (internal/daemon/signer.go). On Android that
// binary does not exist for the platform - it needs CGO plus libwasmvm,
// libqorepqc and libqoresvm - and Android 10+ refuses to exec a binary from app
// storage in any case. So this package deliberately offers NO registration,
// heartbeat or claim. An Android node observes and verifies; it does not earn.
//
// Earning on Android needs heartbeat signing inside this process, which needs
// the exact sign-bytes and PQC-attachment contract from the chain side. That is
// specified in android/README.md rather than guessed at here: a transaction
// built from a guessed contract is rejected by the chain at best, and at worst
// a key that can do more than beat.
package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/db"
	"github.com/qorechain/qorechain-lightnode/internal/lightclient"
)

// Version is the light node version this build came from.
func Version() string { return "3.1.12" }

// Node is a light node running inside an Android app.
//
// It is safe to call from any thread: Android drives it from a foreground
// service, and Start blocks, so the caller runs it on its own thread while the
// UI thread reads status.
type Node struct {
	mu     sync.Mutex
	chain  *client.Client
	store  *db.DB
	lc     *lightclient.LightClient
	cancel context.CancelFunc
	logger *slog.Logger
}

// NewNode opens a node against the given chain endpoints.
//
// dataDir must be a directory the app owns - on Android, Context.getFilesDir().
// rpcAddr and apiAddr are the consensus RPC and the REST gateway; witnesses is
// a comma-separated list of additional RPC endpoints to cross-check the primary
// against, and may be empty.
//
// A single witness is not a cross-check. Every spelling of loopback collapses
// to one witness per port, so pointing two of them at this phone proves
// nothing; the point of a witness is that it is somebody else's node.
func NewNode(dataDir, rpcAddr, apiAddr, witnesses string) (*Node, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("dataDir is required")
	}
	if rpcAddr == "" {
		return nil, fmt.Errorf("rpcAddr is required")
	}
	if apiAddr == "" {
		apiAddr = rpcAddr
	}

	logger := slog.New(slog.NewTextHandler(androidLog{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store, err := db.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("opening store in %s: %w", dataDir, err)
	}

	chain := client.New(rpcAddr, apiAddr)
	lc, err := lightclient.New(chain, store, logger, splitWitnesses(witnesses))
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("starting light client: %w", err)
	}

	return &Node{chain: chain, store: store, lc: lc, logger: logger}, nil
}

// Start runs the light client until Stop is called. It blocks, so Android must
// call it off the main thread.
func (n *Node) Start() error {
	n.mu.Lock()
	if n.cancel != nil {
		n.mu.Unlock()
		return fmt.Errorf("already started")
	}
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	n.mu.Unlock()

	return n.lc.Start(ctx)
}

// Stop ends the sync loop and closes the store. The node cannot be restarted.
func (n *Node) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cancel != nil {
		n.cancel()
		n.cancel = nil
	}
	if n.store != nil {
		_ = n.store.Close()
		n.store = nil
	}
}

// StatusJSON reports what the node knows about the chain and about itself.
//
// The assurance field is the one worth surfacing: it says how many independent
// witnesses corroborated the latest header. A reading taken from one endpoint
// is a reading from one endpoint, and the UI must not present it as verified.
func (n *Node) StatusJSON() string {
	assurance, sources := n.lc.Assurance()
	out := map[string]interface{}{
		"version":       Version(),
		"latest_height": n.lc.LatestHeight(),
		"syncing":       n.lc.IsSyncing(),
		"assurance":     string(assurance),
		"witness_count": sources,
		"last_conflict": n.lc.LastConflict(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if st, err := n.chain.NodeStatus(ctx); err == nil {
		out["chain_id"] = st.Result.NodeInfo.Network
		out["node_version"] = st.Result.NodeInfo.Version
		out["catching_up"] = st.Result.SyncInfo.CatchingUp
	} else {
		out["chain_error"] = err.Error()
	}
	return toJSON(out)
}

// RewardPoolJSON reports the chain's light-node reward pool, and what the
// per-node ceiling is worth per month at the live block cadence.
//
// The module account balance is not the earnable reward: most of it is
// Reserved, which nobody earned and only governance can move. The chain states
// the ceiling per BLOCK, so the monthly figure is derived here from a measured
// cadence - it moved from 2.32 s/block in August 2026 to 3.22 s in October,
// which is a 28% change in what the same ceiling pays.
func (n *Node) RewardPoolJSON() string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := n.chain.RewardPool(ctx)
	if err != nil {
		return toJSON(map[string]interface{}{"error": err.Error()})
	}
	out := map[string]interface{}{
		"balance":                       pool.Balance,
		"reserved":                      pool.Reserved,
		"payable":                       pool.Payable,
		"outstanding":                   pool.Outstanding,
		"distributable":                 pool.Distributable,
		"initialized":                   pool.Initialized,
		"max_reward_per_block_per_node": pool.MaxRewardPerBlockPerNode,
		"denom":                         "uqor",
	}
	bt := 0.0
	if v, err := n.chain.BlockTimeSeconds(ctx, 20000); err == nil {
		bt = v
		out["block_time_seconds"] = bt
	}
	// Both bounds, each with its condition: the chain does not say which one
	// is in force (see client.RewardPoolResponse.Ceilings).
	stated, hasStated, perBlock, hasPerBlock := pool.Ceilings(bt)
	if hasStated {
		out["max_reward_per_month_per_node"] = strconv.FormatFloat(stated, 'f', 0, 64)
	}
	if hasPerBlock {
		out["per_block_bound_as_monthly"] = strconv.FormatFloat(perBlock, 'f', 0, 64)
	}
	if active, known := n.chain.MonthlyCapActive(ctx); known {
		out["monthly_cap_active"] = active
		switch {
		case active && hasStated:
			out["bound_in_force_per_month"] = strconv.FormatFloat(stated, 'f', 0, 64)
		case !active && hasPerBlock:
			out["bound_in_force_per_month"] = strconv.FormatFloat(perBlock, 'f', 0, 64)
		}
	}
	return toJSON(out)
}

// HeartbeatWindowJSON reports the chain's heartbeat cadence in seconds, which
// is what an Android scheduler has to be built around.
//
// This is the number that decides whether a phone can hold a registered node at
// all. The chain marks a node inactive once it goes heartbeat_interval +
// heartbeat_grace_period without a beat, and an inactive node falls below
// min_uptime_for_rewards and earns nothing. At the October 2026 cadence that
// deadline is about 59 minutes, while Doze can defer deferrable work past an
// hour on a stationary unplugged device - so a periodic WorkManager job cannot
// hold the floor and a foreground service is the only honest answer.
//
// It is reported even though this package cannot heartbeat, because the app
// needs it to tell the operator what running a registered node would require.
func (n *Node) HeartbeatWindowJSON() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params, err := n.chain.LightNodeParams(ctx)
	if err != nil {
		return toJSON(map[string]interface{}{"error": err.Error()})
	}
	interval, e1 := strconv.ParseInt(params.HeartbeatInterval, 10, 64)
	grace, e2 := strconv.ParseInt(params.HeartbeatGracePeriod, 10, 64)
	if e1 != nil || e2 != nil {
		return toJSON(map[string]interface{}{"error": "chain returned unreadable heartbeat params"})
	}

	out := map[string]interface{}{
		"interval_blocks":        interval,
		"grace_blocks":           grace,
		"deadline_blocks":        interval + grace,
		"min_uptime_for_rewards": params.MinUptimeForRewards,
	}
	if bt, err := n.chain.BlockTimeSeconds(ctx, 20000); err == nil && bt > 0 {
		out["block_time_seconds"] = bt
		out["interval_seconds"] = int64(float64(interval) * bt)
		out["deadline_seconds"] = int64(float64(interval+grace) * bt)
		// What Android has to beat, stated plainly for the UI.
		out["needs_foreground_service"] = float64(interval+grace)*bt < 3600
	}
	return toJSON(out)
}

// CanEarn reports whether this build can run a registered, earning node.
//
// It is false and says why, rather than being absent: an operator installing a
// light node on a phone deserves to be told that it verifies the chain but does
// not earn, instead of discovering it from an empty rewards screen.
func (n *Node) CanEarn() bool { return false }

// CannotEarnReason explains CanEarn.
func (n *Node) CannotEarnReason() string {
	return "This build verifies the chain but cannot register or heartbeat. " +
		"Signing on desktop runs through the bundled qorechaind binary, which has " +
		"no Android build and which Android 10+ will not exec from app storage. " +
		"Registering, claiming and withdrawing go through QoreX."
}

func splitWitnesses(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, w := range splitComma(s) {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, trimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(out, trimSpace(s[start:]))
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func toJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"encoding failed"}`
	}
	return string(b)
}

// androidLog sends slog output to stderr, which Android surfaces in logcat.
type androidLog struct{}

func (androidLog) Write(p []byte) (int, error) {
	return fmt.Print(string(p))
}
