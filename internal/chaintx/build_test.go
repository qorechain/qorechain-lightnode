package chaintx

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	qpqc "github.com/qorechain/qorechain-pqc/go"
	"google.golang.org/protobuf/encoding/protowire"
)

// fields splits one protobuf message into (number, raw value) pairs.
func fields(t *testing.T, b []byte) [][2]interface{} {
	t.Helper()
	var out [][2]interface{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			t.Fatalf("bad tag: %v", protowire.ParseError(n))
		}
		b = b[n:]
		switch typ {
		case protowire.BytesType:
			v, m := protowire.ConsumeBytes(b)
			out = append(out, [2]interface{}{num, v})
			b = b[m:]
		case protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			out = append(out, [2]interface{}{num, v})
			b = b[m:]
		default:
			t.Fatalf("unexpected wire type %v", typ)
		}
	}
	return out
}

// fakeChain answers the queries a submission makes and records the broadcast.
func fakeChain(t *testing.T, chainID string, v2Height string) (*httptest.Server, *[]byte) {
	var sent []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/cosmos/upgrade/v1beta1/applied_plan/"):
			h := "0"
			if strings.HasSuffix(r.URL.Path, "/v3.2.0") {
				h = v2Height
			}
			io.WriteString(w, `{"height":"`+h+`"}`)
		case strings.HasPrefix(r.URL.Path, "/cosmos/auth/v1beta1/accounts/"):
			// A vesting account, to exercise the nested base account.
			io.WriteString(w, `{"account":{"@type":"/cosmos.vesting.v1beta1.PeriodicVestingAccount",`+
				`"base_vesting_account":{"base_account":{"address":"x","account_number":"42","sequence":"7"}}}}`)
		case r.URL.Path == "/cosmos/tx/v1beta1/txs" && r.Method == http.MethodPost:
			var req struct {
				TxBytes string `json:"tx_bytes"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			sent, _ = base64.StdEncoding.DecodeString(req.TxBytes)
			io.WriteString(w, `{"tx_response":{"txhash":"AB","code":0}}`)
		case strings.HasPrefix(r.URL.Path, "/cosmos/tx/v1beta1/txs/"):
			io.WriteString(w, `{"tx_response":{"txhash":"AB","height":"9","code":0}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &sent
}

// The contract, checked on the bytes that leave the process: the ML-DSA-87
// signature covers the v2 sign-bytes of the body WITHOUT the extension, the
// secp256k1 signature covers the SignDoc of the body WITH it, and the account
// number and sequence are the chain's.
func TestHybridTransactionSignsWhatTheChainVerifies(t *testing.T) {
	const chainID = "qorechain-vladi"
	srv, sent := fakeChain(t, chainID, "2981742")
	key, _ := OperatorKeyFromMnemonic(testMnemonic)
	pub, priv, err := qpqc.MLDSA87.Keygen()
	if err != nil {
		t.Fatal(err)
	}
	s := NewSigner(srv.URL, chainID, key, pub, priv)
	s.Poll = 0

	if _, err := s.Submit(context.Background(), 300000, MsgHeartbeat(key.Address())); err != nil {
		t.Fatal(err)
	}

	raw := fields(t, *sent)
	if len(raw) != 3 || raw[0][0] != protowire.Number(1) || raw[1][0] != protowire.Number(2) || raw[2][0] != protowire.Number(3) {
		t.Fatalf("TxRaw is not body, auth_info, one signature: %v", raw)
	}
	body, auth, classical := raw[0][1].([]byte), raw[1][1].([]byte), raw[2][1].([]byte)

	// The body: one message, then the PQC extension at 1023.
	var msgs, exts [][]byte
	for _, f := range fields(t, body) {
		switch f[0] {
		case protowire.Number(1):
			msgs = append(msgs, f[1].([]byte))
		case protowire.Number(1023):
			exts = append(exts, f[1].([]byte))
		default:
			t.Fatalf("unexpected body field %v", f[0])
		}
	}
	if len(msgs) != 1 || len(exts) != 1 {
		t.Fatalf("body has %d messages and %d extensions, want 1 and 1", len(msgs), len(exts))
	}
	ext := fields(t, exts[0])
	if string(ext[0][1].([]byte)) != typePQCHybridSig {
		t.Fatalf("extension type %q", ext[0][1])
	}
	sigFields := fields(t, ext[1][1].([]byte))
	if sigFields[0][1] != uint64(1) {
		t.Errorf("algorithm_id = %v, want 1 (Dilithium-5)", sigFields[0][1])
	}
	pqcSig := sigFields[1][1].([]byte)

	// PQC: over v2 sign-bytes of the body without the extension.
	b0 := txBody([]Any{MsgHeartbeat(key.Address())}, nil)
	if !qpqc.MLDSA87.Verify(pub, qpqc.HybridSignBytesV2(chainID, b0, auth), pqcSig) {
		t.Fatal("the ML-DSA-87 signature does not cover v2 sign-bytes of the body without the extension")
	}
	if qpqc.MLDSA87.Verify(pub, qpqc.HybridSignBytesV1(b0, auth), pqcSig) {
		t.Fatal("signed v1 on a network that applied the v2 switch")
	}

	// Classical: over the SignDoc of the body WITH the extension, account 42.
	h := sha256.Sum256(signDoc(body, auth, chainID, 42))
	var r, sc secp256k1.ModNScalar
	r.SetByteSlice(classical[:32])
	sc.SetByteSlice(classical[32:])
	pk, _ := secp256k1.ParsePubKey(key.PubKey())
	if !ecdsa.NewSignature(&r, &sc).Verify(h[:], pk) {
		t.Fatal("the secp256k1 signature does not cover the SignDoc of the body with the extension")
	}

	// AuthInfo carries the chain's sequence (7).
	signer := fields(t, fields(t, auth)[0][1].([]byte))
	if signer[len(signer)-1][1] != uint64(7) {
		t.Errorf("sequence = %v, want 7 from the account", signer[len(signer)-1][1])
	}
}

// Before its switching upgrade a legacy network verifies v1.
func TestLegacyNetworkBeforeTheSwitchSignsV1(t *testing.T) {
	srv, _ := fakeChain(t, "qorechain-diana", "0")
	key, _ := OperatorKeyFromMnemonic(testMnemonic)
	pub, priv, _ := qpqc.MLDSA87.Keygen()
	s := NewSigner(srv.URL, "qorechain-diana", key, pub, priv)
	v, err := s.signBytesVersion(context.Background())
	if err != nil || v != qpqc.SignBytesV1 {
		t.Fatalf("version = %d (%v), want v1", v, err)
	}
}

// A key registration from an account without a key is classical and alone.
func TestKeyRegistrationIsClassicalAndAlone(t *testing.T) {
	srv, sent := fakeChain(t, "qorechain-devnet323", "0")
	key, _ := OperatorKeyFromMnemonic(testMnemonic)
	pub, priv, _ := qpqc.MLDSA87.Keygen()
	s := NewSigner(srv.URL, "qorechain-devnet323", key, nil, nil)
	s.Poll = 0
	msg, err := MsgRegisterPQCKeyV2("qorechain-devnet323", key.Address(), pub, priv, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitClassical(context.Background(), 400000, msg); err != nil {
		t.Fatal(err)
	}
	body := fields(t, fields(t, *sent)[0][1].([]byte))
	if len(body) != 1 || body[0][0] != protowire.Number(1) {
		t.Fatalf("key registration body = %v, want exactly one message and no extension", body)
	}
	// The proof of possession verifies under the key being registered.
	m := fields(t, fields(t, body[0][1].([]byte))[1][1].([]byte))
	pop := m[len(m)-1]
	if pop[0] != protowire.Number(6) ||
		!qpqc.MLDSA87.Verify(pub, []byte(ProofOfPossessionMessage("qorechain-devnet323", key.Address(), pub)), pop[1].([]byte)) {
		t.Fatal("proof_of_possession missing or not the key's signature over the PoP message")
	}
	if _, err := s.Submit(context.Background(), 1, MsgHeartbeat(key.Address())); err == nil {
		t.Fatal("a hybrid submission without a PQC key was attempted")
	}
}
