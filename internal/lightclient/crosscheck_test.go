package lightclient

import (
	"strings"
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/client"
)

// TestWitnessValidation covers the two ways a witness list can look redundant
// while corroborating nothing.
func TestWitnessValidation(t *testing.T) {
	primary := client.New("https://rpc.qore.host", "https://rpc.qore.host")

	t.Run("rejects a witness that is the primary again", func(t *testing.T) {
		// A compromised endpoint agrees with itself. Counting it as a second
		// source would report "corroborated" for a single point of failure.
		_, err := newCrossChecker(primary, []string{"https://rpc.qore.host"})
		if err == nil {
			t.Fatal("accepted the primary as its own witness")
		}
		if !strings.Contains(err.Error(), "independent") {
			t.Fatalf("rejected, but not as a duplicate host: %v", err)
		}
	})

	t.Run("rejects plaintext http to a remote host", func(t *testing.T) {
		// Corroboration over a channel an attacker can rewrite is theatre: the
		// same attacker answers as every witness.
		_, err := newCrossChecker(primary, []string{"http://rpc.example.com"})
		if err == nil {
			t.Fatal("accepted a remote witness over plaintext http")
		}
		if !strings.Contains(err.Error(), "https") {
			t.Fatalf("rejected, but not on the scheme: %v", err)
		}
	})

	t.Run("allows plaintext http to localhost", func(t *testing.T) {
		// A node the operator runs themselves is reached over loopback; there is
		// no transit to rewrite.
		if _, err := newCrossChecker(primary, []string{"http://127.0.0.1:26657"}); err != nil {
			t.Fatalf("rejected a loopback witness: %v", err)
		}
	})

	t.Run("accepts distinct https witnesses", func(t *testing.T) {
		cc, err := newCrossChecker(primary, []string{
			"https://rpc-2.example.com",
			"https://rpc-3.example.com",
		})
		if err != nil {
			t.Fatalf("rejected valid witnesses: %v", err)
		}
		if len(cc.witnesses) != 2 {
			t.Fatalf("got %d witnesses, want 2", len(cc.witnesses))
		}
	})

	t.Run("no witnesses is allowed but yields no corroboration", func(t *testing.T) {
		cc, err := newCrossChecker(primary, nil)
		if err != nil {
			t.Fatalf("rejected an empty witness list: %v", err)
		}
		if len(cc.witnesses) != 0 {
			t.Fatalf("invented %d witnesses", len(cc.witnesses))
		}
	})
}

// TestRequireSafeSchemeLoopbackIsParsed pins QSR-2026-0020: plaintext http is
// allowed only to a real loopback address, never to a DNS name that happens to
// start with "127.".
func TestRequireSafeSchemeLoopbackIsParsed(t *testing.T) {
	cases := []struct {
		addr string
		ok   bool
	}{
		{"http://127.0.0.1:26657", true},
		{"http://127.1.2.3:26657", true},
		{"http://[::1]:26657", true},
		{"http://localhost:26657", true},
		{"https://127.evil.com", true},

		{"http://127.evil.com:26657", false},
		{"http://127.0.0.1.nip.io:26657", false},
		{"http://10.0.0.1:26657", false},
		{"ftp://127.0.0.1", false},
	}
	for _, tc := range cases {
		err := requireSafeScheme(tc.addr)
		if (err == nil) != tc.ok {
			t.Errorf("requireSafeScheme(%q) err = %v, want ok=%v", tc.addr, err, tc.ok)
		}
	}
}

// TestLoopbackWitnessesCountOnce: two spellings of the same local node are one
// source, so they must not be reported as corroboration.
func TestLoopbackWitnessesCountOnce(t *testing.T) {
	primary := client.New("https://rpc.qore.host", "https://rpc.qore.host")

	dupes := [][]string{
		{"http://127.0.0.1:26657", "http://localhost:26657"},
		{"http://127.0.0.1:26657", "http://[::1]:26657"},
		{"http://LOCALHOST:26657", "http://127.9.9.9:26657"},
	}
	for _, ws := range dupes {
		_, err := newCrossChecker(primary, ws)
		if err == nil || !strings.Contains(err.Error(), "independent") {
			t.Errorf("witnesses %v: want a duplicate-host rejection, got %v", ws, err)
		}
	}

	// Different ports on loopback are different listeners and stay distinct.
	cc, err := newCrossChecker(primary, []string{"http://127.0.0.1:26657", "http://localhost:36657"})
	if err != nil {
		t.Fatalf("rejected loopback witnesses on distinct ports: %v", err)
	}
	if len(cc.witnesses) != 2 {
		t.Fatalf("got %d witnesses, want 2", len(cc.witnesses))
	}

	// A local primary and a local witness on the same port are one node.
	local := client.New("http://127.0.0.1:26657", "http://127.0.0.1:1317")
	if _, err := newCrossChecker(local, []string{"http://localhost:26657"}); err == nil {
		t.Error("accepted the local primary again under another loopback name")
	}
}

// TestDisagreementMessageNamesTheSources is what an operator has to act on: not
// "something is wrong" but which endpoint said what.
func TestDisagreementMessageNamesTheSources(t *testing.T) {
	err := &disagreementError{
		Height: 2040000,
		ByHash: map[string][]string{
			"AAAAAAAAAAAAAAAAAAAA": {"https://rpc.qore.host"},
			"BBBBBBBBBBBBBBBBBBBB": {"https://rpc-2.example.com", "https://rpc-3.example.com"},
		},
	}
	msg := err.Error()
	for _, want := range []string{"2040000", "rpc.qore.host", "rpc-2.example.com", "rpc-3.example.com"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message does not name %q: %s", want, msg)
		}
	}
}
