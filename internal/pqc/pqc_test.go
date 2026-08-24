package pqc

import (
	"bytes"
	"testing"
)

// TestKeygenSizes guards the v3.0.2 hotfix: the FFI keygen must be called with
// the 4-argument (buffer + length-pointer) signature. The earlier 2-argument
// call passed garbage length pointers and returned BufferTooSmall (-10) on
// darwin / NullPointer (-11) on linux, which surfaced to operators as
// "dilithium5 keygen failed: code -11".
func TestKeygenSizes(t *testing.T) {
	pub, priv, err := DilithiumKeygen()
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	if len(pub) != DilithiumPublicKeySize {
		t.Errorf("public key size = %d, want %d", len(pub), DilithiumPublicKeySize)
	}
	if len(priv) != DilithiumPrivateKeySize {
		t.Errorf("private key size = %d, want %d", len(priv), DilithiumPrivateKeySize)
	}
}

// TestKeygenUnique ensures two successive keygens produce different material
// (a fixed/zero key would be a catastrophic RNG failure).
func TestKeygenUnique(t *testing.T) {
	_, priv1, err := DilithiumKeygen()
	if err != nil {
		t.Fatalf("keygen 1: %v", err)
	}
	_, priv2, err := DilithiumKeygen()
	if err != nil {
		t.Fatalf("keygen 2: %v", err)
	}
	if bytes.Equal(priv1, priv2) {
		t.Fatal("two keygens produced identical private keys")
	}
}

// TestSignVerifyRoundTrip is the happy path: a signature over a message must
// verify against the matching public key.
func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := DilithiumKeygen()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	msg := []byte("qorechain block header @ height 1")

	sig, err := DilithiumSign(priv, msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if len(sig) == 0 || len(sig) > DilithiumSignatureSize {
		t.Fatalf("signature size = %d, want 1..=%d", len(sig), DilithiumSignatureSize)
	}

	ok, err := DilithiumVerify(pub, msg, sig)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("valid signature was rejected")
	}
}

// TestVerifyRejectsTamperedSignature is the regression test for the v3.0.2
// security fix: DilithiumVerify previously treated rc == 0 as "valid", which
// inverted the verify result and accepted forged signatures. A tampered
// signature MUST be rejected.
func TestVerifyRejectsTamperedSignature(t *testing.T) {
	pub, priv, err := DilithiumKeygen()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	msg := []byte("authorize withdrawal of 1000 uqor")

	sig, err := DilithiumSign(priv, msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	tampered := append([]byte(nil), sig...)
	tampered[0] ^= 0xFF // flip bits in the first signature byte

	ok, err := DilithiumVerify(pub, msg, tampered)
	if err != nil {
		t.Fatalf("verify returned error instead of false: %v", err)
	}
	if ok {
		t.Fatal("SECURITY: tampered signature was accepted as valid")
	}
}

// TestVerifyRejectsTamperedMessage ensures a signature does not verify against
// a different message than the one signed.
func TestVerifyRejectsTamperedMessage(t *testing.T) {
	pub, priv, err := DilithiumKeygen()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	msg := []byte("transfer to qor1alice")

	sig, err := DilithiumSign(priv, msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	ok, err := DilithiumVerify(pub, []byte("transfer to qor1mallory"), sig)
	if err != nil {
		t.Fatalf("verify returned error instead of false: %v", err)
	}
	if ok {
		t.Fatal("SECURITY: signature verified against a different message")
	}
}

// TestVerifyRejectsWrongKey ensures a signature made with one key does not
// verify under an unrelated public key.
func TestVerifyRejectsWrongKey(t *testing.T) {
	pubA, privA, err := DilithiumKeygen()
	if err != nil {
		t.Fatalf("keygen A: %v", err)
	}
	pubB, _, err := DilithiumKeygen()
	if err != nil {
		t.Fatalf("keygen B: %v", err)
	}
	if bytes.Equal(pubA, pubB) {
		t.Fatal("two keygens produced identical public keys")
	}
	msg := []byte("signed by A")

	sig, err := DilithiumSign(privA, msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	ok, err := DilithiumVerify(pubB, msg, sig)
	if err != nil {
		t.Fatalf("verify returned error instead of false: %v", err)
	}
	if ok {
		t.Fatal("SECURITY: A's signature verified under B's public key")
	}
}

// TestSignRejectsBadKeySize guards the input validation that produced clearer
// operator errors in the hotfix.
func TestSignRejectsBadKeySize(t *testing.T) {
	if _, err := DilithiumSign([]byte("too short"), []byte("msg")); err == nil {
		t.Fatal("sign accepted an undersized private key")
	}
}

// TestVerifyRejectsBadKeySize mirrors the sign-side validation.
func TestVerifyRejectsBadKeySize(t *testing.T) {
	if _, err := DilithiumVerify([]byte("too short"), []byte("msg"), []byte("sig")); err == nil {
		t.Fatal("verify accepted an undersized public key")
	}
}

// TestSignRejectsEmptyMessage ensures empty input is rejected up front.
func TestSignRejectsEmptyMessage(t *testing.T) {
	_, priv, err := DilithiumKeygen()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	if _, err := DilithiumSign(priv, nil); err == nil {
		t.Fatal("sign accepted an empty message")
	}
}
