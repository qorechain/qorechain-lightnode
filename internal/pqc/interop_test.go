//go:build chaininterop

// Cross-implementation check against the chain's native libqorepqc.
//
// Not part of the normal test run: it needs the shared library, which only
// exists on the chain nodes. Run it there, on the exact build the chain is
// using, whenever this package or the chain's PQC changes:
//
//	CGO_ENABLED=1 LD_LIBRARY_PATH=/opt/qorechain/lib \
//	  go test -tags chaininterop ./internal/pqc/ -v
//
// Proven passing 2026-08-24 against libqorepqc.so sha256 9d335cc3...,
// the build running on mainnet.
package pqc

/*
#cgo LDFLAGS: -lqorepqc
#include <stdint.h>
#include <stddef.h>
int32_t qore_dilithium_keygen(uint8_t *pubkey_out, size_t *pubkey_len,
                              uint8_t *privkey_out, size_t *privkey_len);
int32_t qore_dilithium_sign(const uint8_t *privkey, size_t privkey_len,
                            const uint8_t *message, size_t message_len,
                            uint8_t *sig_out, size_t *sig_len);
int32_t qore_dilithium_verify(const uint8_t *pubkey, size_t pubkey_len,
                              const uint8_t *message, size_t message_len,
                              const uint8_t *signature, size_t sig_len);
*/
import "C"

import (
	"bytes"
	"testing"
)

func chainKeygen(t *testing.T) (pub, priv []byte) {
	t.Helper()
	pub, priv = make([]byte, DilithiumPublicKeySize), make([]byte, DilithiumPrivateKeySize)
	pl, sl := C.size_t(len(pub)), C.size_t(len(priv))
	if rc := C.qore_dilithium_keygen((*C.uint8_t)(&pub[0]), &pl, (*C.uint8_t)(&priv[0]), &sl); rc != 0 {
		t.Fatalf("chain keygen: rc=%d", int(rc))
	}
	return pub[:pl], priv[:sl]
}

func chainSign(t *testing.T, priv, msg []byte) []byte {
	t.Helper()
	sig := make([]byte, DilithiumSignatureSize)
	sl := C.size_t(len(sig))
	if rc := C.qore_dilithium_sign((*C.uint8_t)(&priv[0]), C.size_t(len(priv)),
		(*C.uint8_t)(&msg[0]), C.size_t(len(msg)),
		(*C.uint8_t)(&sig[0]), &sl); rc != 0 {
		t.Fatalf("chain sign: rc=%d", int(rc))
	}
	return sig[:sl]
}

func chainVerify(t *testing.T, pub, msg, sig []byte) int {
	t.Helper()
	return int(C.qore_dilithium_verify((*C.uint8_t)(&pub[0]), C.size_t(len(pub)),
		(*C.uint8_t)(&msg[0]), C.size_t(len(msg)),
		(*C.uint8_t)(&sig[0]), C.size_t(len(sig))))
}

var interopMsg = []byte("qorechain lightnode interop vector")

// TestChainInterop is the claim the package doc makes: signatures cross both
// ways. If this ever fails, the light node cannot register or heartbeat.
func TestChainInterop(t *testing.T) {
	t.Run("chain signs, we verify", func(t *testing.T) {
		pub, priv := chainKeygen(t)
		sig := chainSign(t, priv, interopMsg)
		ok, err := DilithiumVerify(pub, interopMsg, sig)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if !ok {
			t.Fatal("rejected a signature the chain produced")
		}
	})

	t.Run("we sign, chain verifies", func(t *testing.T) {
		pub, priv, err := DilithiumKeygen()
		if err != nil {
			t.Fatalf("keygen: %v", err)
		}
		sig, err := DilithiumSign(priv, interopMsg)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if rc := chainVerify(t, pub, interopMsg, sig); rc != 1 {
			t.Fatalf("chain rejected our signature: rc=%d", rc)
		}
	})

	t.Run("we sign with a chain-generated key", func(t *testing.T) {
		pub, priv := chainKeygen(t)
		sig, err := DilithiumSign(priv, interopMsg)
		if err != nil {
			t.Fatalf("sign with chain key: %v", err)
		}
		if rc := chainVerify(t, pub, interopMsg, sig); rc != 1 {
			t.Fatalf("chain rejected a signature made with its own key: rc=%d", rc)
		}
	})

	t.Run("both reject tampering", func(t *testing.T) {
		pub, priv, _ := DilithiumKeygen()
		sig, _ := DilithiumSign(priv, interopMsg)
		bad := bytes.Clone(sig)
		bad[100] ^= 0xff
		if rc := chainVerify(t, pub, interopMsg, bad); rc == 1 {
			t.Fatal("chain accepted a tampered signature")
		}
		if ok, _ := DilithiumVerify(pub, interopMsg, bad); ok {
			t.Fatal("we accepted a tampered signature")
		}
	})
}
