package keyring

import (
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/pqc"
)

// A key the keyring created must be able to sign, and the signature must verify
// under the public key the keyring reports. This is the whole point of holding
// a key; it was a stub in both backends for several releases.
func TestFileBackendSignsWithTheKeyItCreated(t *testing.T) {
	b, err := NewEncryptedFileBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b.SetPassphrase("test")

	info, err := b.Create("node", KeyTypeDilithium5)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("heartbeat sign bytes")
	sig, err := b.Sign("node", msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	ok, err := pqc.DilithiumVerify(info.PubKey, msg, sig)
	if err != nil || !ok {
		t.Fatalf("signature does not verify under the reported public key: ok=%v err=%v", ok, err)
	}
	if _, err := b.Sign("missing", msg); err == nil {
		t.Fatal("signing with an unknown key name succeeded")
	}
}
