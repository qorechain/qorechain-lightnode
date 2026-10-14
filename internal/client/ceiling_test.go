package client

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Today's mainnet states the bound per block only. At the 3.2151 s/block
// measured on 2026-10-14, 1713 uqor/block is ~1,381 QOR a month, not 2,000.
func TestCeilingsOnAPerBlockChain(t *testing.T) {
	var r RewardPoolResponse
	if err := json.Unmarshal(liveFixture(t, "reward_pool.json"), &r); err != nil {
		t.Fatal(err)
	}
	stated, hasStated, perBlock, hasPerBlock := r.Ceilings(3.2151)
	if hasStated || stated != 0 {
		t.Errorf("mainnet today sends no monthly bound, got %.0f", stated)
	}
	if !hasPerBlock || math.Abs(perBlock/1e6-1381.0) > 1.0 {
		t.Errorf("per-block bound as monthly = %.1f QOR (ok=%v), want ~1,381", perBlock/1e6, hasPerBlock)
	}
	if _, _, _, ok := r.Ceilings(0); ok {
		t.Error("without a cadence the per-block bound cannot be converted")
	}
}

// From v3.2.3 the chain sends the monthly bound always, including while the
// lightnode-monthly-cap switch is off and the per-block bound still applies.
// Both must come back: returning only the monthly one would show 2,000 QOR a
// month while the chain pays ~1,381. That is the bug this replaced.
func TestCeilingsOnAv323ChainReturnsBoth(t *testing.T) {
	r := RewardPoolResponse{MaxRewardPerBlockPerNode: "1713", MaxRewardPerMonthPerNode: "2000000000"}
	stated, hasStated, perBlock, hasPerBlock := r.Ceilings(3.2151)
	if !hasStated || stated != 2_000_000_000 {
		t.Errorf("stated monthly = %.0f (ok=%v), want 2000000000", stated, hasStated)
	}
	if !hasPerBlock || math.Abs(perBlock/1e6-1381.0) > 1.0 {
		t.Errorf("per-block as monthly = %.1f QOR (ok=%v), want ~1,381 alongside the stated bound",
			perBlock/1e6, hasPerBlock)
	}
}

// The switch is read from the applied plan, through a list of names. Shapes are
// the live mainnet answers of 2026-10-15: {"height":"0"} for an unapplied plan.
func TestMonthlyCapActiveFollowsTheAppliedPlan(t *testing.T) {
	for _, c := range []struct {
		name          string
		body          string
		status        int
		active, known bool
	}{
		{"before v3.2.3", `{"height":"0"}`, 200, false, true},
		{"after v3.2.3", `{"height":"3600000"}`, 200, true, true},
		{"chain cannot answer", `{"code":12}`, 501, false, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/cosmos/upgrade/v1beta1/applied_plan/v3.2.3" {
				w.WriteHeader(404)
				return
			}
			w.WriteHeader(c.status)
			w.Write([]byte(c.body)) //nolint:errcheck
		}))
		active, known := New(srv.URL, srv.URL).MonthlyCapActive(context.Background())
		srv.Close()
		if active != c.active || known != c.known {
			t.Errorf("%s: active=%v known=%v, want %v/%v", c.name, active, known, c.active, c.known)
		}
	}
}
