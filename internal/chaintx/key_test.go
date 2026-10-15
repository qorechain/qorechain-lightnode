package chaintx

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// The public BIP-39 test mnemonic, recovered with qorechaind v3.2.3
// (`keys add --recover`, test keyring) on 2026-10-16. The light node must
// derive the same account, or an operator bringing their wallet's words would
// sign for a different address than the one their licence is on.
const (
	testMnemonic  = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	testAddress   = "qor19rl4cm2hmr8afy4kldpxz3fka4jguq0agt672h"
	testPubKeyB64 = "Ak9OKtmcNNYLm6YoPJQxqEGK+GcyEpYfl6d7Y3f80Fti"
)

func TestMnemonicDerivesTheAccountQorechaindDerives(t *testing.T) {
	k, err := OperatorKeyFromMnemonic(testMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	if got := base64.StdEncoding.EncodeToString(k.PubKey()); got != testPubKeyB64 {
		t.Errorf("public key = %s, want %s", got, testPubKeyB64)
	}
	if got := k.Address(); got != testAddress {
		t.Errorf("address = %s, want %s", got, testAddress)
	}
	// Extra whitespace is how people paste words.
	k2, err := OperatorKeyFromMnemonic("  " + testMnemonic + "\n")
	if err != nil || k2.Address() != testAddress {
		t.Errorf("whitespace around the mnemonic changed the account: %v", err)
	}
	if _, err := OperatorKeyFromMnemonic("abandon abandon abandon"); err == nil {
		t.Error("a malformed mnemonic was accepted")
	}
}

func TestSignatureIsLowSAndVerifies(t *testing.T) {
	k, _ := OperatorKeyFromMnemonic(testMnemonic)
	msg := []byte("sign doc")
	sig := k.sign(msg)
	if len(sig) != 64 {
		t.Fatalf("signature is %d bytes, want 64", len(sig))
	}
	var r, s secp256k1.ModNScalar
	r.SetByteSlice(sig[:32])
	s.SetByteSlice(sig[32:])
	if s.IsOverHalfOrder() {
		t.Error("s is in the upper half; the SDK rejects malleable signatures")
	}
	pub, err := secp256k1.ParsePubKey(k.PubKey())
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(msg)
	if !ecdsa.NewSignature(&r, &s).Verify(h[:], pub) {
		t.Fatal("signature does not verify")
	}
}

func TestNewKeyRoundTripsThroughItsMnemonic(t *testing.T) {
	k, words, err := NewOperatorKey()
	if err != nil {
		t.Fatal(err)
	}
	back, err := OperatorKeyFromMnemonic(words)
	if err != nil || back.Address() != k.Address() {
		t.Fatalf("restoring from the shown mnemonic gave %s, want %s (%v)", back.Address(), k.Address(), err)
	}
}

// Public-key prefixes printed by qorechaind v3.2.3 `tx pqc recover-key` for the
// test mnemonic and address, --derivation adapter and bridge, 2026-10-16.
func TestPQCDerivationMatchesQorechaind(t *testing.T) {
	keys, err := PQCKeysFromMnemonic(testAddress, testMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"adapter": "747951c21b272716048334acbe44030849454bf1b08f3304a66656da9d5733947bc6361819",
		"bridge":  "dfd766b6881346fc66f19203bd0853c4eb88f552a9126bf04c8bdac1e049f027332880a5ae",
	}
	for _, k := range keys {
		if got := hex.EncodeToString(k.Pub)[:len(want[k.Derivation])]; got != want[k.Derivation] {
			t.Errorf("%s public key starts %s, want %s", k.Derivation, got, want[k.Derivation])
		}
	}
}
