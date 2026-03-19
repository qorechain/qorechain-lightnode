package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/db"
)

// SlashingMonitor watches for slashing events.
type SlashingMonitor struct {
	store  *db.DB
	logger *slog.Logger
}

// NewSlashingMonitor creates a slashing event monitor.
func NewSlashingMonitor(store *db.DB, logger *slog.Logger) *SlashingMonitor {
	return &SlashingMonitor{store: store, logger: logger}
}

// RecordSlashingEvent stores a detected slashing event.
func (m *SlashingMonitor) RecordSlashingEvent(validator string, height int64, eventType string, amount string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := m.store.Conn().Exec(
		`INSERT INTO slashing_events (validator, height, type, amount, detected_at) VALUES (?, ?, ?, ?, ?)`,
		validator, height, eventType, amount, now,
	)
	return err
}

// RecentEvents returns recent slashing events.
func (m *SlashingMonitor) RecentEvents(ctx context.Context, limit int) ([]SlashingEvent, error) {
	rows, err := m.store.Conn().QueryContext(ctx,
		`SELECT validator, height, type, amount, detected_at FROM slashing_events ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []SlashingEvent
	for rows.Next() {
		var e SlashingEvent
		if err := rows.Scan(&e.Validator, &e.Height, &e.Type, &e.Amount, &e.DetectedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

// SlashingEvent represents a detected slashing event.
type SlashingEvent struct {
	Validator  string
	Height     int64
	Type       string
	Amount     string
	DetectedAt string
}
