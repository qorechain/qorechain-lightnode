package cli

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/chaintx"
	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/keyring"
	qpqc "github.com/qorechain/qorechain-pqc/go"
)

const words = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func pqcAccountServer(t *testing.T, pub []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pub == nil {
			w.Write([]byte(`{"found":false,"account":null}`)) //nolint:errcheck
			return
		}
		w.Write([]byte(`{"found":true,"account":{"public_key":"` + base64.StdEncoding.EncodeToString(pub) + `"}}`)) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func choose(t *testing.T, registered []byte) (*chaintx.PQCKey, string, error) {
	t.Helper()
	op, _ := chaintx.OperatorKeyFromMnemonic(words)
	b, _ := keyring.NewEncryptedFileBackend(t.TempDir())
	b.SetPassphrase("t")
	cfg := config.DefaultConfig()
	cfg.APIAddr = pqcAccountServer(t, registered)
	cfg.KeyName = "validator"
	return choosePQCKey(context.Background(), cfg, b, op, words, true)
}

// Four of the six paid mainnet addresses already carry a PQC key, registered
// by a wallet from the same words. The node must sign with that key, under
// either ecosystem derivation, or the chain refuses everything it sends.
func TestRecoverUsesTheWalletRegisteredKey(t *testing.T) {
	op, _ := chaintx.OperatorKeyFromMnemonic(words)
	derived, _ := chaintx.PQCKeysFromMnemonic(op.Address(), words)
	for _, d := range derived {
		k, _, err := choose(t, d.Pub)
		if err != nil || k == nil || k.Derivation != d.Derivation {
			t.Errorf("registered %s key: chose %+v, err %v", d.Derivation, k, err)
		}
	}
}

// A key the words cannot reproduce cannot be signed with; say so up front.
func TestRecoverRefusesAKeyTheWordsCannotReproduce(t *testing.T) {
	other, _, _ := qpqc.MLDSA87.Keygen()
	_, _, err := choose(t, other)
	if err == nil || !strings.Contains(err.Error(), "dedicated operator account") {
		t.Fatalf("err = %v, want a refusal pointing to a dedicated account", err)
	}
}

// No key yet: one derived from the words, so the words restore it.
func TestRecoverWithoutAKeyDerivesOne(t *testing.T) {
	k, _, err := choose(t, nil)
	if err != nil || k == nil || k.Derivation != "adapter" {
		t.Fatalf("chose %+v, err %v; want the adapter derivation", k, err)
	}
}
