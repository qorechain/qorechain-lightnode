package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// With no token configured nothing changes. This is the case nearly every
// operator is in - the dashboard binds to loopback by default - and breaking it
// to add access control for the few who expose it would be a bad trade.
func TestNoTokenConfiguredLeavesRoutesOpen(t *testing.T) {
	srv := New("127.0.0.1:0", testAPI(t), discardLogger())

	for _, path := range []string{"/api/status", "/api/rewards", "/api/rewardpool"} {
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code == http.StatusUnauthorized {
			t.Errorf("%s = 401 with no token configured; the default must stay open", path)
		}
	}
}

// QoreX sends `Authorization: Bearer <token>` whenever its pairing record has
// a token. Until v3.1.5 this node ignored the header, so an operator who set a
// token in the wallet and exposed the dashboard to pair it believed they had
// access control they did not have.
func TestConfiguredTokenIsRequiredOnEveryDataRoute(t *testing.T) {
	api := testAPI(t)
	api.cfg.Dashboard.Token = "s3cret"
	srv := New("0.0.0.0:0", api, discardLogger())

	// Every data route, and /ws, must refuse an unauthenticated caller. The
	// list is taken from the server's own routing so a route added later
	// without the check shows up here.
	paths := []string{
		"/api/status", "/api/validators", "/api/delegation", "/api/rewards",
		"/api/rewardpool", "/api/network", "/api/bridge", "/api/tokenomics",
		"/api/settings", "/api/config", "/ws",
	}
	for _, path := range paths {
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s = %d without a token, want 401", path, rec.Code)
		}
	}

	// The right token gets through.
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/status", nil)
	r.Header.Set("Authorization", "Bearer s3cret")
	srv.mux.ServeHTTP(rec, r)
	if rec.Code == http.StatusUnauthorized {
		t.Error("the configured token was rejected")
	}

	// A wrong one does not.
	rec = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/status", nil)
	r.Header.Set("Authorization", "Bearer wrong")
	srv.mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a wrong token got %d, want 401", rec.Code)
	}
}

// The UI itself stays reachable: a browser cannot attach a bearer header to a
// page load, and the page is inert without the data routes anyway.
func TestStaticUIIsNotBehindTheToken(t *testing.T) {
	api := testAPI(t)
	api.cfg.Dashboard.Token = "s3cret"
	srv := New("127.0.0.1:0", api, discardLogger())

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code == http.StatusUnauthorized {
		t.Error("the static UI is behind the token; a page load cannot carry one")
	}
}

func TestTokenHeaderParsing(t *testing.T) {
	want := []byte("abc")
	cases := []struct {
		header string
		ok     bool
	}{
		{"Bearer abc", true},
		{"bearer abc", true}, // scheme is case-insensitive per RFC 7235
		{"BEARER abc", true},
		{"Bearer  abc ", true}, // surrounding whitespace is not part of the token
		{"Bearer abcd", false},
		{"Bearer ab", false},
		{"Bearer", false},
		{"Bearer ", false},
		{"abc", false}, // no scheme
		{"Basic abc", false},
		{"", false},
	}
	for _, c := range cases {
		if got := tokenMatches(want, c.header); got != c.ok {
			t.Errorf("tokenMatches(%q) = %v, want %v", c.header, got, c.ok)
		}
	}
}
