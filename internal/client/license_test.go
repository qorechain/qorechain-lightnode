package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The chain answers "no licence" with a not-found error document, exactly as
// mainnet does today. That is an answer, not a failure: the caller must get
// Found=false and no error, or every unlicensed operator sees a crash where
// they should see where to buy a licence.
func TestLicenseCheckReadsTheChainsThreeAnswers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/qorechain/license/v1/check/qor1none/lightnode_operator":
			w.WriteHeader(404)
			w.Write([]byte(`{"code":5, "message":"no license for qor1none/lightnode_operator", "details":[]}`))
		case "/qorechain/license/v1/check/qor1active/lightnode_operator":
			w.Write([]byte(`{"license":{"grantee":"qor1active","feature_id":"lightnode_operator"},"active":true}`))
		case "/qorechain/license/v1/check/qor1suspended/lightnode_operator":
			w.Write([]byte(`{"license":{"grantee":"qor1suspended"},"active":false}`))
		case "/qorechain/lightnode/v1/node/qor1active":
			w.Write([]byte(`{"light_node":{"address":"qor1active","node_type":"sx","status":"active","last_heartbeat":"1234"}}`))
		case "/qorechain/lightnode/v1/node/qor1none":
			w.WriteHeader(404)
			w.Write([]byte(`{"code":5, "message":"light node not found", "details":[]}`))
		default:
			w.WriteHeader(501)
			w.Write([]byte(`{"code":12, "message":"Not Implemented"}`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, srv.URL)
	ctx := context.Background()

	got, err := c.LicenseCheck(ctx, "qor1none", FeatureLightNodeOperator)
	if err != nil || got.Found || got.Active {
		t.Fatalf("no licence: got %+v err %v", got, err)
	}
	got, err = c.LicenseCheck(ctx, "qor1active", FeatureLightNodeOperator)
	if err != nil || !got.Found || !got.Active {
		t.Fatalf("active licence: got %+v err %v", got, err)
	}
	got, err = c.LicenseCheck(ctx, "qor1suspended", FeatureLightNodeOperator)
	if err != nil || !got.Found || got.Active {
		t.Fatalf("suspended licence: got %+v err %v", got, err)
	}
	if _, err = c.LicenseCheck(ctx, "qor1old", FeatureLightNodeOperator); err != ErrEndpointUnavailable {
		t.Fatalf("a chain without the route must say so, got %v", err)
	}

	reg, node, err := c.LightNodeRegistration(ctx, "qor1active")
	if err != nil || !reg || node.LightNode.LastHeartbeat != "1234" {
		t.Fatalf("registered node: reg=%v node=%+v err=%v", reg, node, err)
	}
	reg, _, err = c.LightNodeRegistration(ctx, "qor1none")
	if err != nil || reg {
		t.Fatalf("unregistered node: reg=%v err=%v", reg, err)
	}
}

func TestDeriveLCDURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:26657":            "http://localhost:1317",
		"http://host.docker.internal:26657": "http://host.docker.internal:1317",
		"https://rpc.qore.host":             "https://api.qore.host",
		"https://rpc.testnet.qorechain.xyz": "https://api.testnet.qorechain.xyz",
		"https://example.org/rpc":           "https://example.org/rpc",
	} {
		if got := DeriveLCDURL(in); got != want {
			t.Errorf("DeriveLCDURL(%q) = %q, want %q", in, got, want)
		}
	}
}
