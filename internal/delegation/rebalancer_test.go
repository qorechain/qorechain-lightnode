package delegation

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/client"
)

// fakeLCD answers the staking, light node and reputation routes the
// rebalancer reads. Reputation answers 501, as on both networks today.
func fakeLCD(t *testing.T, delegations string) *httptest.Server {
	return fakeLCDWithReputation(t, delegations, nil)
}

// fakeLCDWithReputation serves the reputation route from scores (valoper ->
// composite_score) when scores is not nil, as the chain will once it
// publishes them.
func fakeLCDWithReputation(t *testing.T, delegations string, scores map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/cosmos/staking/v1beta1/delegations/"):
			fmt.Fprintf(w, `{"delegation_responses":[%s]}`, delegations)
		case p == "/cosmos/staking/v1beta1/validators":
			io := `{"validators":[{"operator_address":"qorvaloper1good","status":"BOND_STATUS_BONDED"}]}`
			w.Write([]byte(io)) //nolint:errcheck
		case p == "/cosmos/staking/v1beta1/validators/qorvaloper1jailed":
			w.Write([]byte(`{"validator":{"operator_address":"qorvaloper1jailed","jailed":true,"status":"BOND_STATUS_UNBONDING"}}`)) //nolint:errcheck
		case p == "/cosmos/staking/v1beta1/validators/qorvaloper1out":
			w.Write([]byte(`{"validator":{"operator_address":"qorvaloper1out","jailed":false,"status":"BOND_STATUS_UNBONDED"}}`)) //nolint:errcheck
		case p == "/qorechain/lightnode/v1/params":
			w.Write([]byte(`{"min_delegated_stake":"1000000000"}`)) //nolint:errcheck
		case scores != nil && strings.HasPrefix(p, "/qorechain/reputation/v1/validator/"):
			val := strings.TrimPrefix(p, "/qorechain/reputation/v1/validator/")
			if _, ok := scores[val]; !ok {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"code":5,"message":"no reputation record"}`)) //nolint:errcheck
				return
			}
			fmt.Fprintf(w, `{"reputation":{"consensus_address":"qorvalcons1x","missed_blocks":"3865632","operator_address":%q,"composite_score":%q}}`, val, scores[val])
		case strings.HasPrefix(p, "/qorechain/reputation/"):
			w.WriteHeader(http.StatusNotImplemented)
			w.Write([]byte(`{"code":12,"message":"Not Implemented"}`)) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func del(val, amount string) string {
	return fmt.Sprintf(`{"delegation":{"validator_address":%q},"balance":{"denom":"uqor","amount":%q}}`, val, amount)
}

func reasons(alerts []RebalanceAlert) map[string]bool {
	m := map[string]bool{}
	for _, a := range alerts {
		m[a.Key()] = true
	}
	return m
}

func TestAlertsWhenStakeStopsCounting(t *testing.T) {
	srv := fakeLCD(t, strings.Join([]string{
		del("qorvaloper1good", "600000000"),
		del("qorvaloper1jailed", "900000000"),
		del("qorvaloper1out", "100000000"),
	}, ","))
	var log bytes.Buffer
	r := NewRebalancer(client.New(srv.URL, srv.URL), &Manager{address: "qor1op"}, 0.5, slog.New(slog.NewTextHandler(&log, nil)))

	alerts, err := r.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := reasons(alerts)
	for _, want := range []string{
		ReasonValidatorJailed + "|qorvaloper1jailed",
		ReasonValidatorUnbonded + "|qorvaloper1out",
		ReasonStakeBelowMinimum + "|", // 600 QOR bonded of 1,000 required
	} {
		if !got[want] {
			t.Errorf("missing alert %q in %v", want, got)
		}
	}
	if len(alerts) != 3 {
		t.Errorf("%d alerts, want 3: %v", len(alerts), got)
	}

	// Reputation: one line saying it is not published, then silence.
	if _, err := r.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(log.String(), "does not publish validator reputation"); n != 1 {
		t.Errorf("reputation notice logged %d times, want once:\n%s", n, log.String())
	}
	if strings.Contains(log.String(), "failed to check reputation") {
		t.Errorf("the 501 is still logged as a failure:\n%s", log.String())
	}
}

func TestNoAlertsWhenAllStakeIsBonded(t *testing.T) {
	srv := fakeLCD(t, del("qorvaloper1good", "1500000000"))
	r := NewRebalancer(client.New(srv.URL, srv.URL), &Manager{address: "qor1op"}, 0.5, slog.New(slog.DiscardHandler))
	alerts, err := r.Check(context.Background())
	if err != nil || len(alerts) != 0 {
		t.Fatalf("alerts %v, err %v", reasons(alerts), err)
	}
}

func TestLowReputationAlertsOnceTheChainPublishesScores(t *testing.T) {
	srv := fakeLCDWithReputation(t, del("qorvaloper1good", "1500000000"),
		map[string]string{"qorvaloper1good": "0.420000000000000000"})
	r := NewRebalancer(client.New(srv.URL, srv.URL), &Manager{address: "qor1op"}, 0.5, slog.New(slog.DiscardHandler))
	alerts, err := r.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].Reason != ReasonLowReputation || alerts[0].Reputation != 0.42 {
		t.Fatalf("alerts %+v", alerts)
	}
}

// A validator the chain has not scored yet answers 404: no alert, no warning.
func TestValidatorWithoutAScoreIsQuiet(t *testing.T) {
	srv := fakeLCDWithReputation(t, del("qorvaloper1good", "1500000000"), map[string]string{})
	var log bytes.Buffer
	r := NewRebalancer(client.New(srv.URL, srv.URL), &Manager{address: "qor1op"}, 0.5, slog.New(slog.NewTextHandler(&log, nil)))
	alerts, err := r.Check(context.Background())
	if err != nil || len(alerts) != 0 {
		t.Fatalf("alerts %v, err %v", reasons(alerts), err)
	}
	if strings.Contains(log.String(), "WARN") {
		t.Fatalf("a missing score was logged as a warning:\n%s", log.String())
	}
}
