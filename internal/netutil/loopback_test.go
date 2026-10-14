package netutil

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"LocalHost", true},
		{"127.0.0.1", true},
		{"127.1.2.3", true},
		{"::1", true},
		{"::ffff:127.0.0.1", true},

		// Names that only look like loopback resolve wherever their owner says.
		{"127.evil.com", false},
		{"127.0.0.1.nip.io", false},
		{"127.", false},
		{"localhost.evil.com", false},
		// Shorthand the Go parser does not accept is treated as remote.
		{"127.1", false},
		// Bracketed or zoned forms are not what Hostname() returns.
		{"[::1]", false},
		{"10.0.0.1", false},
		{"0.0.0.0", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsLoopbackHost(tc.host); got != tc.want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}
