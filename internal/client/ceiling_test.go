package client

import (
	"encoding/json"
	"math"
	"testing"
)

// Today's chain states the ceiling per block only; the monthly figure must be
// derived from the cadence. At the 3.2151 s/block measured on mainnet on
// 2026-10-14, 1713 uqor/block is ~1,381 QOR a month, not the 2,000 decided.
func TestMonthlyCeilingDerivedFromPerBlockChain(t *testing.T) {
	var r RewardPoolResponse
	if err := json.Unmarshal(liveFixture(t, "reward_pool.json"), &r); err != nil {
		t.Fatal(err)
	}
	got, fromChain, ok := r.MonthlyCeiling(3.2151)
	if !ok || fromChain {
		t.Fatalf("ok=%v fromChain=%v, want a derived figure", ok, fromChain)
	}
	if math.Abs(got/1e6-1381.0) > 1.0 {
		t.Errorf("derived %.1f QOR/month, want ~1,381", got/1e6)
	}
	if _, _, ok := r.MonthlyCeiling(0); ok {
		t.Error("no cadence and no per-month field must not produce a figure")
	}
}

// Once the chain states the ceiling per month (the change proposed for
// v3.2.3), that figure is authoritative and the cadence is irrelevant.
func TestMonthlyCeilingFromChainWins(t *testing.T) {
	r := RewardPoolResponse{MaxRewardPerBlockPerNode: "1713", MaxRewardPerMonthPerNode: "2000000000"}
	for _, bt := range []float64{0, 2.22, 3.2151} {
		got, fromChain, ok := r.MonthlyCeiling(bt)
		if !ok || !fromChain || got != 2_000_000_000 {
			t.Errorf("cadence %.4f: got %.0f fromChain=%v ok=%v, want 2000000000 from chain", bt, got, fromChain, ok)
		}
	}
}
