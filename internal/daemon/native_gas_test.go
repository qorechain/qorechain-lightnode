package daemon

import (
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/chaintx"
)

// The fee is charged on the limit, so after a heartbeat the next limit follows
// what it used (plus a quarter), never above the configured ceiling, and goes
// back to the ceiling after an out-of-gas refusal.
func TestGasLimitFollowsWhatTheLastRunUsed(t *testing.T) {
	s := &chaintx.Signer{GasPrice: 0.25}
	n := &nativeSubmitter{s: s, gas: 300000}
	const hb = "lightnode heartbeat"

	if got := n.limitFor(hb); got != 300000 {
		t.Fatalf("first run: limit %d, want the ceiling 300000", got)
	}
	n.record(hb, chaintx.Result{Code: 0, GasUsed: 171270})
	if got := n.limitFor(hb); got != 215000 {
		t.Fatalf("after 171,270 used: limit %d, want 215000", got)
	}
	if got := n.FeePerTx(hb); got != 53750 {
		t.Fatalf("fee %d uqor, want 53750 (was 75000 at 300,000)", got)
	}
	if got := n.limitFor("lightnode claim-rewards"); got != 300000 {
		t.Fatalf("another kind shares the heartbeat's measurement: %d", got)
	}

	n.record(hb, chaintx.Result{Code: codeOutOfGas, GasUsed: 215000})
	if got := n.limitFor(hb); got != 300000 {
		t.Fatalf("after out of gas: limit %d, want the ceiling", got)
	}

	n.record(hb, chaintx.Result{Code: 0, GasUsed: 290000})
	if got := n.limitFor(hb); got != 300000 {
		t.Fatalf("limit %d above the ceiling", got)
	}
	n.record(hb, chaintx.Result{Code: 0, GasUsed: 20000})
	if got := n.limitFor(hb); got != gasFloor {
		t.Fatalf("limit %d, want the floor %d", got, gasFloor)
	}
}
