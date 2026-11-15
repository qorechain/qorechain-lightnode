package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// QSR-2026-0835: a malicious or intercepted endpoint could stream a body until
// the daemon ran out of memory. Every body is capped at MaxResponseBytes, and
// an error keeps only the start of the body.
func TestResponsesAreBounded(t *testing.T) {
	big := strings.Repeat("x", MaxResponseBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok-json":
			w.Write([]byte(`{"pad":"` + big + `"}`)) //nolint:errcheck
		case "/err":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(big)) //nolint:errcheck
		}
	}))
	defer srv.Close()
	c := New(srv.URL, srv.URL)

	var out map[string]any
	if err := c.get(context.Background(), srv.URL+"/ok-json", &out); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("oversized JSON: err %v, want ErrResponseTooLarge", err)
	}
	err := c.get(context.Background(), srv.URL+"/err", &out)
	var se *StatusError
	if !errors.As(err, &se) || len(se.Body) > maxErrorBody+3 {
		t.Fatalf("error body kept %d bytes (err %T), want at most %d", len(se.Body), err, maxErrorBody+3)
	}
}

func TestBodyOfExactlyTheLimitIsAccepted(t *testing.T) {
	body := `{"a":"` + strings.Repeat("y", MaxResponseBytes-8) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body)) //nolint:errcheck
	}))
	defer srv.Close()
	var out map[string]any
	if err := New(srv.URL, srv.URL).get(context.Background(), srv.URL, &out); err != nil {
		t.Fatalf("a body of %d bytes was refused: %v", len(body), err)
	}
}
