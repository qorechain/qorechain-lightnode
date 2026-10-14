package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// From chain v3.2.3 an expired grant also answers active=false, and the
// remedies differ: an expired licence is renewed, a suspended one goes to
// support. The node used to call every inactive licence "suspended". These
// shapes follow LicenseView in qorechain-core v3.2.3 query.proto; the REST
// gateway renders int64 expires_at as a JSON string, so both spellings are
// covered.
func TestLicenceTellsExpiredFromSuspended(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/qorechain/license/v1/check/qor1exp/lightnode_operator":
			w.Write([]byte(`{"license":{"grantee":"qor1exp","expires_at":"3400000","suspended":false,"active":false},"active":false}`)) //nolint:errcheck
		case "/qorechain/license/v1/check/qor1expnum/lightnode_operator":
			w.Write([]byte(`{"license":{"grantee":"qor1expnum","expires_at":3400000,"suspended":false},"active":false}`)) //nolint:errcheck
		case "/qorechain/license/v1/check/qor1sus/lightnode_operator":
			w.Write([]byte(`{"license":{"grantee":"qor1sus","expires_at":"0","suspended":true},"active":false}`)) //nolint:errcheck
		case "/qorechain/license/v1/check/qor1ok/lightnode_operator":
			w.Write([]byte(`{"license":{"grantee":"qor1ok","expires_at":"0","suspended":false,"active":true},"active":true}`)) //nolint:errcheck
		default:
			w.WriteHeader(501)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, srv.URL)
	ctx := context.Background()

	for _, addr := range []string{"qor1exp", "qor1expnum"} {
		got, err := c.LicenseCheck(ctx, addr, FeatureLightNodeOperator)
		if err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
		if !got.Expired() || got.ExpiresAt != 3400000 {
			t.Errorf("%s: want expired at 3400000, got %+v", addr, got)
		}
	}

	got, err := c.LicenseCheck(ctx, "qor1sus", FeatureLightNodeOperator)
	if err != nil || got.Expired() || !got.Suspended || got.Active {
		t.Errorf("suspended licence read as %+v err %v", got, err)
	}

	got, err = c.LicenseCheck(ctx, "qor1ok", FeatureLightNodeOperator)
	if err != nil || !got.Active || got.Expired() {
		t.Errorf("active licence read as %+v err %v", got, err)
	}
}
