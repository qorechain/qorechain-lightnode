package pqc

import (
	"bytes"
	"testing"
)

func TestPublicKeyIsDerivedFromThePrivateKey(t *testing.T) {
	pub, priv, err := DilithiumKeygen()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DilithiumPublicKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pub) {
		t.Fatal("derived public key differs from the keypair's")
	}
	if _, err := DilithiumPublicKey(priv[:100]); err == nil {
		t.Fatal("a truncated private key was accepted")
	}
}
