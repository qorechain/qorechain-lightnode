package delegation

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/client"
)

type countingSubmitter struct{ n int }

func (c *countingSubmitter) SubmitLightNodeTx(context.Context, ...string) (string, error) {
	c.n++
	return fmt.Sprintf("HASH%d", c.n), nil
}

// QSR-2026-0836: an endpoint that keeps reporting rewards the node does not
// have made it submit (and pay for) a claim on every check. One claim per
// minClaimGap at most, whatever the endpoint says.
func TestALyingEndpointGetsOneClaimPerGap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"node":{"address":"qor1op","status":"active","accumulated_rewards":"999999999999"},"found":true}`)) //nolint:errcheck
	}))
	defer srv.Close()
	sub := &countingSubmitter{}
	ac := NewAutoClaimer(client.New(srv.URL, srv.URL), sub, "qor1op", minClaimInterval, 1000, slog.New(slog.DiscardHandler))
	for i := 0; i < 20; i++ {
		ac.claim(context.Background())
	}
	if sub.n != 1 {
		t.Fatalf("%d claims submitted in 20 checks, want 1", sub.n)
	}
}
