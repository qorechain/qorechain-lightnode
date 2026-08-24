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
