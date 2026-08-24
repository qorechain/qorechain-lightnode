// Package lightclient follows the chain's headers.
//
// It is NOT a light client in the protocol sense, and the name is retained only
// because it is load-bearing across the config file, the daemon and the
// dashboard. It does not verify commit signatures against the validator set, it
// does not track validator set transitions, and it does not enforce a trust
// period. A security audit in August 2026 called the original version a
// "trusted RPC mirror", and that was accurate: it asked one endpoint for the
// height and believed the answer.
//
// What it does now is corroborate. The same height is fetched from the primary
// and from every configured witness, and a header is stored only when they
// return the same block hash. That raises the bar from "compromise one
// endpoint" to "compromise every configured endpoint, consistently and at the
// same time". It does not reach the bar of verifying consensus, and Assurance
// reports which of the two an operator is actually running.
//
// The honest upgrade is a real light client. Until then, do not describe this
// as verifying the chain.
package lightclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/db"
)

// Header represents a verified block header.
type Header struct {
	Height        int64
	Hash          string
	Time          time.Time
	ValidatorHash string
}

// LightClient verifies and stores block headers.
type LightClient struct {
	chain  *client.Client
	store  *db.DB
	logger *slog.Logger

	cc *crossChecker

	mu           sync.RWMutex
	latestHeight int64
	syncing      bool
	assurance    Assurance
	sources      int
	lastConflict string
}

// New creates a new light client.
func New(chain *client.Client, store *db.DB, logger *slog.Logger, witnesses []string) (*LightClient, error) {
	cc, err := newCrossChecker(chain, witnesses)
	if err != nil {
		return nil, err
	}
	if len(cc.witnesses) == 0 {
		logger.Warn("no witness endpoints configured: headers come from a single source, " +
			"which can fabricate every value you see. Set witness_addrs to at least one " +
			"independently operated RPC.")
	}
	return &LightClient{
		chain:     chain,
		store:     store,
		logger:    logger,
		cc:        cc,
		assurance: AssuranceTrusted,
	}, nil
}

// Assurance reports how the most recent header was established: believed from a
// single source, or corroborated across independent ones.
func (lc *LightClient) Assurance() (Assurance, int) {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.assurance, lc.sources
}

// LastConflict returns the most recent disagreement between endpoints, or the
// empty string. A non-empty value means one of the configured sources lied, and
// the operator should find out which before trusting anything the node shows.
func (lc *LightClient) LastConflict() string {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.lastConflict
}

// Start begins the header sync loop.
func (lc *LightClient) Start(ctx context.Context) error {
	lc.logger.Info("starting light client header sync")

	// Initial sync — get latest height
	if err := lc.syncLatest(ctx); err != nil {
		lc.logger.Warn("initial sync failed, will retry", "error", err)
	}

	// Periodic sync
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			lc.logger.Info("light client stopped")
			return nil
		case <-ticker.C:
			if err := lc.syncLatest(ctx); err != nil {
				lc.logger.Warn("header sync failed", "error", err)
			}
		}
	}
}

// syncLatest fetches the latest block header and stores it.
func (lc *LightClient) syncLatest(ctx context.Context) error {
	lc.mu.Lock()
	lc.syncing = true
	lc.mu.Unlock()
	defer func() {
		lc.mu.Lock()
		lc.syncing = false
		lc.mu.Unlock()
	}()

	status, err := lc.chain.NodeStatus(ctx)
	if err != nil {
		return fmt.Errorf("fetching node status: %w", err)
	}

	tip, err := strconv.ParseInt(status.Result.SyncInfo.LatestBlockHeight, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing block height: %w", err)
	}

	// One block behind the tip. Endpoints reach a new height at slightly
	// different moments, so comparing the very latest block would report a
	// disagreement every time one witness is a beat behind - and a check that
	// cries wolf every few seconds is one an operator learns to ignore.
	height := tip - 1
	if height < 1 {
		return nil
	}

	lc.mu.Lock()
	if height <= lc.latestHeight {
		lc.mu.Unlock()
		return nil // already have this height
	}
	lc.mu.Unlock()

	checked, err := lc.cc.headerAt(ctx, height)
	if err != nil {
		var conflict *disagreementError
		if errors.As(err, &conflict) {
			// Nothing is stored. One of the endpoints is lying, and which one
			// cannot be decided from here - so refuse to pick, and say so
			// loudly enough that it is not mistaken for a network blip.
			lc.mu.Lock()
			lc.lastConflict = conflict.Error()
			lc.mu.Unlock()
			lc.logger.Error("REFUSING HEADER: configured endpoints disagree",
				"detail", conflict.Error(),
				"action", "one of these sources is not telling the truth; do not trust displayed state until resolved")
			return err
		}
		return err
	}

	if err := lc.storeHeader(checked.Header); err != nil {
		return fmt.Errorf("storing header: %w", err)
	}

	lc.mu.Lock()
	lc.latestHeight = height
	lc.assurance = checked.Assurance
	lc.sources = checked.Sources
	lc.lastConflict = ""
	lc.mu.Unlock()

	lc.logger.Debug("synced header", "height", height,
		"hash", checked.Header.Hash, "assurance", checked.Assurance, "sources", checked.Sources)
	return nil
}

// storeHeader saves a header to SQLite.
func (lc *LightClient) storeHeader(h Header) error {
	_, err := lc.store.Conn().Exec(
		`INSERT OR REPLACE INTO headers (height, hash, time, validator_hash) VALUES (?, ?, ?, ?)`,
		h.Height, h.Hash, h.Time.Format(time.RFC3339Nano), h.ValidatorHash,
	)
	return err
}

// LatestHeight returns the highest synced header height. When called from a
// short-lived process that did not run the sync loop (e.g. the `status`
// subcommand), the in-memory counter is 0, so it falls back to the persisted
// maximum so operators see the true synced height of the running daemon.
func (lc *LightClient) LatestHeight() int64 {
	lc.mu.RLock()
	h := lc.latestHeight
	lc.mu.RUnlock()
	if h > 0 {
		return h
	}
	var dbMax int64
	if lc.store != nil && lc.store.Conn() != nil {
		_ = lc.store.Conn().QueryRow(`SELECT COALESCE(MAX(height), 0) FROM headers`).Scan(&dbMax)
	}
	return dbMax
}

// IsSyncing returns whether the client is actively syncing.
func (lc *LightClient) IsSyncing() bool {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.syncing
}

// GetHeader returns a stored header by height.
func (lc *LightClient) GetHeader(height int64) (*Header, error) {
	row := lc.store.Conn().QueryRow(
		`SELECT height, hash, time, validator_hash FROM headers WHERE height = ?`, height,
	)
	var h Header
	var timeStr string
	if err := row.Scan(&h.Height, &h.Hash, &timeStr, &h.ValidatorHash); err != nil {
		return nil, err
	}
	h.Time, _ = time.Parse(time.RFC3339Nano, timeStr)
	return &h, nil
}

// RecentHeaders returns the N most recent stored headers.
func (lc *LightClient) RecentHeaders(limit int) ([]Header, error) {
	rows, err := lc.store.Conn().Query(
		`SELECT height, hash, time, validator_hash FROM headers ORDER BY height DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var headers []Header
	for rows.Next() {
		var h Header
		var timeStr string
		if err := rows.Scan(&h.Height, &h.Hash, &timeStr, &h.ValidatorHash); err != nil {
			return nil, err
		}
		h.Time, _ = time.Parse(time.RFC3339Nano, timeStr)
		headers = append(headers, h)
	}
	return headers, nil
}

// parseBlockTime parses a consensus header timestamp.
func parseBlockTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}
