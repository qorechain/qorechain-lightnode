package dashboard

import (
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/config"
)

// TestBindAddrExposure pins the distinction the old default got wrong: ":8420"
// looks local and is not.
func TestBindAddrExposure(t *testing.T) {
	for _, tc := range []struct {
		addr    string
		local   bool
		display string
	}{
		{"127.0.0.1:8420", true, "http://127.0.0.1:8420"},
		{"localhost:8420", true, "http://localhost:8420"},
		{"[::1]:8420", true, "http://[::1]:8420"},
		{":8420", false, "http://<this-host>:8420"}, // the old default
		{"0.0.0.0:8420", false, "http://<this-host>:8420"},
		{"192.168.1.20:8420", false, "http://192.168.1.20:8420"},
	} {
		if got := isLoopback(tc.addr); got != tc.local {
			t.Errorf("isLoopback(%q) = %v, want %v", tc.addr, got, tc.local)
		}
		if got := DisplayURL(tc.addr); got != tc.display {
			t.Errorf("DisplayURL(%q) = %q, want %q", tc.addr, got, tc.display)
		}
		warned := ExposureWarning(tc.addr) != ""
		if warned == tc.local {
			t.Errorf("ExposureWarning(%q): warned=%v but loopback=%v", tc.addr, warned, tc.local)
		}
	}
}

// TestOriginAllowed covers the handshake guard. The dashboard has no auth, so a
// page on another origin must not be able to open the telemetry stream just
// because the operator has a browser on the same machine.
func TestOriginAllowed(t *testing.T) {
	const host = "127.0.0.1:8420"
	for _, tc := range []struct {
		origin string
		ok     bool
		why    string
	}{
		{"", true, "no Origin: CLI, curl, monitoring probe - not the threat here"},
		{"http://127.0.0.1:8420", true, "the dashboard itself"},
		{"HTTP://127.0.0.1:8420", true, "scheme and host compare case-insensitively"},
		{"http://evil.example", false, "another site in the operator's browser"},
		{"http://127.0.0.1:9999", false, "same host, different port"},
		{"http://127.0.0.1.evil.example", false, "prefix that looks like ours"},
		{"://", false, "unparseable"},
	} {
		if got := originAllowed(tc.origin, host); got != tc.ok {
			t.Errorf("originAllowed(%q) = %v, want %v - %s", tc.origin, got, tc.ok, tc.why)
		}
	}
}

// TestDefaultBindIsLoopback pins the actual vulnerability, not just the helpers.
// The default used to be ":8420" - every interface, no authentication - while the
// startup line said "localhost". An operator had nothing to notice.
func TestDefaultBindIsLoopback(t *testing.T) {
	def := config.DefaultConfig().Dashboard.BindAddr
	if !isLoopback(def) {
		t.Fatalf("default dashboard bind %q is reachable off-host; the dashboard has no auth", def)
	}
	if ExposureWarning(def) != "" {
		t.Fatalf("default bind %q should not warn", def)
	}
}
