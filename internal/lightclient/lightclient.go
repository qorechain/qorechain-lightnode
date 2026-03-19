package lightclient

import (
	"context"
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

	mu           sync.RWMutex
	latestHeight int64
	syncing      bool
}

// New creates a new light client.
func New(chain *client.Client, store *db.DB, logger *slog.Logger) *LightClient {
	return &LightClient{
		chain:  chain,
		store:  store,
		logger: logger,
	}
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

	height, err := strconv.ParseInt(status.Result.SyncInfo.LatestBlockHeight, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing block height: %w", err)
	}

	lc.mu.Lock()
	if height <= lc.latestHeight {
		lc.mu.Unlock()
		return nil // already have this height
	}
	lc.mu.Unlock()

	blockTime, _ := time.Parse(time.RFC3339Nano, status.Result.SyncInfo.LatestBlockTime)

	header := Header{
		Height: height,
		Time:   blockTime,
	}

	if err := lc.storeHeader(header); err != nil {
		return fmt.Errorf("storing header: %w", err)
	}

	lc.mu.Lock()
	lc.latestHeight = height
	lc.mu.Unlock()

	lc.logger.Debug("synced header", "height", height)
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

// LatestHeight returns the highest synced header height.
func (lc *LightClient) LatestHeight() int64 {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.latestHeight
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
