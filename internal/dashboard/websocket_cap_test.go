package dashboard

import (
	"log/slog"
	"testing"
)

// QSR-2026-0832: the hub had no connection cap.
func TestHubRefusesConnectionsPastTheCap(t *testing.T) {
	h := NewHub(slog.New(slog.DiscardHandler))
	for i := 0; i < maxClients; i++ {
		if !h.register(&wsClient{hub: h, send: make(chan []byte, 1)}) {
			t.Fatalf("connection %d refused below the cap", i+1)
		}
	}
	if h.register(&wsClient{hub: h, send: make(chan []byte, 1)}) {
		t.Fatalf("connection %d accepted past the cap of %d", maxClients+1, maxClients)
	}
}
