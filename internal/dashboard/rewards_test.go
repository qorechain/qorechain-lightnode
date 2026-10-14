package dashboard

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/client"
	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/db"
	"github.com/qorechain/qorechain-lightnode/internal/delegation"
	"github.com/qorechain/qorechain-lightnode/internal/lightclient"
)

const (
	testOperator = "qor1op"
	// What the light node itself has earned from the 3% fee share.
	feeShareUqor = "1713000"
	// What the operator's bonded stake has earned. A different quantity, which
	// moves for reasons that have nothing to do with running a node.
	stakingUqor = "42000000"
)

// chainStub answers the handful of chain routes the rewards and status
// handlers touch, in the shapes live mainnet uses.
func chainStub(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/qorechain/lightnode/v1/node/" + testOperator:
			w.Write([]byte(`{"node":{"address":"` + testOperator + `","node_type":"sx",` + //nolint:errcheck
				`"status":"active","last_heartbeat":"3500000","total_heartbeats":"900",` +
				`"expected_heartbeats":"1000","accumulated_rewards":"` + feeShareUqor + `"},"found":true}`))
		case "/cosmos/distribution/v1beta1/delegators/" + testOperator + "/rewards":
			w.Write([]byte(`{"rewards":[],"total":[{"denom":"uqor","amount":"` + stakingUqor + `"}]}`)) //nolint:errcheck
		case "/status":
			// The light client has no verified header in a unit test, so the
			// handler falls back to the node's own height, as it does on a node
			// that has just started.
			w.Write([]byte(`{"result":{"node_info":{"network":"qorechain-vladi","version":"0.38.25"},` + //nolint:errcheck
				`"sync_info":{"latest_block_height":"3500400","latest_block_time":"2026-10-14T20:00:00Z",` +
				`"catching_up":false}}}`))
		case "/qorechain/lightnode/v1/params":
			w.Write([]byte(`{"registration_fee":"1000000","heartbeat_interval":"1000",` + //nolint:errcheck
				`"min_delegated_stake":"1000000000","reward_share":"0.030000000000000000",` +
				`"min_uptime_for_rewards":"0.800000000000000000","max_light_nodes":"10000",` +
				`"heartbeat_grace_period":"100"}`))
		default:
			w.WriteHeader(501)
			w.Write([]byte(`{"code":12,"message":"Not Implemented"}`)) //nolint:errcheck
		}
	}))
	t.Cleanup(srv.Close)
	return client.New(srv.URL, srv.URL)
}

// discardLogger is the logger every test in this package uses.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func testAPI(t *testing.T) *API {
	t.Helper()
	logger := discardLogger()
	store, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	chain := chainStub(t)
	lc, err := lightclient.New(chain, store, logger, nil)
	if err != nil {
		t.Fatalf("lightclient.New: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.OperatorAddress = testOperator
	cfg.Delegation.AutoClaim = true

	return NewAPI(chain, store, lc, delegation.New(chain, store, logger, testOperator), cfg, logger)
}

func getJSON(t *testing.T, h http.HandlerFunc, path string) map[string]interface{} {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s -> %d: %s", path, rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return out
}

// QoreX reads /api/rewards total_rewards as "Accrued 3% fee-share rewards" and
// shows it as light-node earnings. This handler used to put the operator's
// STAKING yield there, so the companion app showed a number that was non-zero
// even when the node had earned nothing, and that moved with delegation rather
// than with running the node. The two quantities must stay apart.
func TestRewardsSeparatesFeeShareFromStakingYield(t *testing.T) {
	got := getJSON(t, testAPI(t).HandleRewards, "/api/rewards")

	if got["total_rewards"] != feeShareUqor {
		t.Errorf("total_rewards = %v, want the light-node fee share %s "+
			"(QoreX renders this field as light-node earnings)", got["total_rewards"], feeShareUqor)
	}
	if got["total_rewards"] == stakingUqor {
		t.Error("total_rewards is the staking yield again; that is the conflation this fixes")
	}
	if got["staking_rewards"] != stakingUqor {
		t.Errorf("staking_rewards = %v, want %s under its own name", got["staking_rewards"], stakingUqor)
	}
}

// QoreX declares auto_compound and defaults it to false when absent, so a node
// with claiming switched on reported it as off.
func TestRewardsReportsTheClaimSetting(t *testing.T) {
	got := getJSON(t, testAPI(t).HandleRewards, "/api/rewards")

	if got["auto_compound"] != true {
		t.Errorf("auto_compound = %v, want true: the node has auto_claim on", got["auto_compound"])
	}
	if got["auto_claim"] != true {
		t.Errorf("auto_claim = %v, want true", got["auto_claim"])
	}
}

// heartbeat_age_blocks is the field QoreX declares and never received. Without
// it the companion app cannot show the one number that decides whether the node
// is about to be marked inactive and stop earning.
func TestStatusReportsHeartbeatAgeAndUptime(t *testing.T) {
	got := getJSON(t, testAPI(t).HandleStatus, "/api/status")

	if got["heartbeat_age_blocks"] != float64(400) {
		t.Fatalf("heartbeat_age_blocks = %v, want 400 (height 3500400 - last beat 3500000); "+
			"absent means QoreX's NodeStatus.heartbeatAgeBlocks stays undefined",
			got["heartbeat_age_blocks"])
	}
	if got["heartbeat_overdue"] != false {
		t.Errorf("heartbeat_overdue = %v, want false: 400 blocks is inside the 1100 deadline",
			got["heartbeat_overdue"])
	}
	if got["heartbeat_interval_blocks"] != float64(1000) {
		t.Errorf("heartbeat_interval_blocks = %v, want 1000 from the chain params",
			got["heartbeat_interval_blocks"])
	}
	if got["heartbeat_deadline_blocks"] != float64(1100) {
		t.Errorf("heartbeat_deadline_blocks = %v, want interval+grace = 1100",
			got["heartbeat_deadline_blocks"])
	}
	// 900 of 1000 expected beats is 0.9, above the 0.8 reward floor.
	if got["uptime"] != 0.9 {
		t.Errorf("uptime = %v, want 0.9 (total/expected, the ratio the reward gate uses)", got["uptime"])
	}
}
