package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The light-node queries are decoded from responses RECORDED FROM LIVE MAINNET
// (testdata/live, captured 2026-10-14), not from a shape this repo invented.
//
// That distinction is the whole point of this file. Until 2026-10-14 the structs
// expected {"params":{...}}, {"stats":{...}} and {"light_node":{...}}, while the
// chain sends the params and stats fields flat and the node under "node" beside
// a "found" flag. encoding/json does not object to a member it has no field
// for, so every one of those decoded into an all-zero record and returned a nil
// error. The existing test passed because its fixture served the invented shape
// back to the client - the test agreed with the bug instead of with the chain.
//
// Recording real bytes is what stops that happening again: a fixture copied
// from the chain cannot quietly agree with a decoder that is wrong.
func liveFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "live", name))
	if err != nil {
		t.Fatalf("reading recorded chain response %s: %v", name, err)
	}
	return b
}

func serveLive(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(501)
			w.Write([]byte(`{"code":12,"message":"Not Implemented"}`)) //nolint:errcheck
			return
		}
		w.Write([]byte(body)) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, srv.URL)
}

// Params decode to the values mainnet actually reports. With the old "params"
// envelope every field here came back "" and nothing said so.
func TestParamsDecodeFromRecordedMainnet(t *testing.T) {
	c := serveLive(t, map[string]string{
		"/qorechain/lightnode/v1/params": string(liveFixture(t, "params.json")),
	})

	got, err := c.LightNodeParams(context.Background())
	if err != nil {
		t.Fatalf("LightNodeParams: %v", err)
	}
	for _, c := range []struct{ field, got, want string }{
		{"registration_fee", got.RegistrationFee, "1000000"},
		{"heartbeat_interval", got.HeartbeatInterval, "1000"},
		{"min_delegated_stake", got.MinDelegatedStake, "1000000000"},
		{"min_uptime_for_rewards", got.MinUptimeForRewards, "0.800000000000000000"},
		{"max_light_nodes", got.MaxLightNodes, "10000"},
		{"heartbeat_grace_period", got.HeartbeatGracePeriod, "100"},
	} {
		if c.got != c.want {
			t.Errorf("params.%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

func TestStatsDecodeFromRecordedMainnet(t *testing.T) {
	c := serveLive(t, map[string]string{
		"/qorechain/lightnode/v1/stats": string(liveFixture(t, "stats.json")),
	})

	got, err := c.LightNodeStats(context.Background())
	if err != nil {
		t.Fatalf("LightNodeStats: %v", err)
	}
	// Mainnet has no registered nodes, so the interesting assertion is that the
	// fields are populated at all rather than left at the zero value by a
	// mismatched envelope. "0" is a decoded zero; "" is a missed field.
	if got.TotalRegistered != "0" || got.LastRewardHeight != "0" {
		t.Fatalf("stats decoded to empty strings, so the envelope is wrong again: %+v", got)
	}
}

// The reward pool is the only place an operator can see what is earnable, and
// it is the endpoint the v3.1.98 upgrade added. Mainnet reports the whole
// balance as reserved and nothing distributable, which is the designed state
// while no eligible node is running.
func TestRewardPoolDecodeFromRecordedMainnet(t *testing.T) {
	c := serveLive(t, map[string]string{
		"/qorechain/lightnode/v1/reward_pool": string(liveFixture(t, "reward_pool.json")),
	})

	got, err := c.RewardPool(context.Background())
	if err != nil {
		t.Fatalf("RewardPool: %v", err)
	}
	if !got.Initialized {
		t.Error("reward pool reports uninitialized; the upgrade has executed on mainnet")
	}
	if got.Balance == "" || got.Reserved == "" || got.MaxRewardPerBlockPerNode == "" {
		t.Fatalf("reward pool fields missing from decode: %+v", got)
	}
	if got.Balance != got.Reserved {
		t.Errorf("balance %s and reserved %s differ; mainnet holds the whole pool in reserve",
			got.Balance, got.Reserved)
	}
	if got.Distributable != "0" {
		t.Errorf("distributable = %s, want 0 while no eligible node runs", got.Distributable)
	}
}

// A chain that predates the reward-pool upgrade answers 501, which must read as
// "this chain does not have it" rather than as a failure.
func TestRewardPoolOnAChainWithoutItSaysSo(t *testing.T) {
	c := serveLive(t, map[string]string{})
	if _, err := c.RewardPool(context.Background()); err != ErrEndpointUnavailable {
		t.Fatalf("want ErrEndpointUnavailable from a chain without the route, got %v", err)
	}
}

// Registration is the mismatch that actually bit: four production call sites
// read this answer, including the daemon's registration check and auto-claim.
// The field names below are the chain's own, taken from LightNodeView in
// qorechain-core x/lightnode/types/query.pb.go.
func TestRegistrationReadsNodeAndFound(t *testing.T) {
	registered := `{"node":{"address":"qor1op","node_type":"sx","version":"3.1.5",` +
		`"status":"active","registered_at":"3000000","last_heartbeat":"3501000",` +
		`"total_heartbeats":"501","expected_heartbeats":"501",` +
		`"delegated_stake":"1000000000","accumulated_rewards":"1713000",` +
		`"initial_heartbeat_interval":"1000"},"found":true}`

	c := serveLive(t, map[string]string{
		"/qorechain/lightnode/v1/node/qor1op":     registered,
		"/qorechain/lightnode/v1/node/qor1absent": string(liveFixture(t, "node_absent.json")),
	})
	ctx := context.Background()

	reg, node, err := c.LightNodeRegistration(ctx, "qor1op")
	if err != nil {
		t.Fatalf("registered node: %v", err)
	}
	if !reg {
		t.Fatal("a registered node reported as unregistered - this is QSR-grade: " +
			"auto-claim and the daemon's registration check both believe this answer")
	}
	if node.LightNode.LastHeartbeat != "3501000" {
		t.Errorf("last_heartbeat = %q, want 3501000", node.LightNode.LastHeartbeat)
	}
	if node.LightNode.AccumulatedRewards != "1713000" {
		t.Errorf("accumulated_rewards = %q, want 1713000", node.LightNode.AccumulatedRewards)
	}
	if node.LightNode.ExpectedHeartbeats != "501" {
		t.Errorf("expected_heartbeats = %q, want 501 (needed to show uptime)", node.LightNode.ExpectedHeartbeats)
	}

	// The chain's own "no such node" answer, recorded live: {"node":null,"found":false}.
	reg, _, err = c.LightNodeRegistration(ctx, "qor1absent")
	if err != nil || reg {
		t.Fatalf("absent node: reg=%v err=%v, want false/nil", reg, err)
	}
}

// The shape this repo used to assume must now read as "not registered" rather
// than silently succeeding, so nobody reintroduces the envelope believing the
// decoder tolerates both.
func TestOldInventedEnvelopeNoLongerCountsAsRegistered(t *testing.T) {
	c := serveLive(t, map[string]string{
		"/qorechain/lightnode/v1/node/qor1op": `{"light_node":{"address":"qor1op","status":"active"}}`,
	})

	reg, _, err := c.LightNodeRegistration(context.Background(), "qor1op")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg {
		t.Fatal("the old light_node envelope was accepted; the decoder must follow the chain only")
	}
}
