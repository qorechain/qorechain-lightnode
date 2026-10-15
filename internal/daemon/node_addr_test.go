package daemon

import "testing"

// Measured against mainnet on 2026-10-16 with qorechaind status: https:// with
// or without :443 connects; tcp://rpc.qore.host and tcp://rpc.qore.host:443
// both fail. The signer must hand qorechaind a form that connects.
func TestSignerNodeAddressKeepsTLS(t *testing.T) {
	cases := map[string]string{
		"https://rpc.qore.host":     "https://rpc.qore.host",
		"https://rpc.qore.host:443": "https://rpc.qore.host:443",
		"http://127.0.0.1:26657":    "tcp://127.0.0.1:26657",
		"tcp://127.0.0.1:26657":     "tcp://127.0.0.1:26657",
	}
	for in, want := range cases {
		if got := toTCP(in); got != want {
			t.Errorf("toTCP(%q) = %q, want %q", in, got, want)
		}
	}
}
