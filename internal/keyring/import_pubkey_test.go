package keyring

import (
	"bytes"
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/pqc"
)

// `onboard` imports the key it generates, and Import stored a Dilithium-5 key
// without its public half. `register` then printed a register-key-v2 command
// with an empty key, found on the v3.2.3 devnet rehearsal on 2026-10-16. The
// public key must be there after an import, and must be the right one.
func TestImportedKeyReportsItsPublicKey(t *testing.T) {
	pub, priv, err := pqc.DilithiumKeygen()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewEncryptedFileBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b.SetPassphrase("test")

	imported, err := b.Import("node", KeyTypeDilithium5, priv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Get("node")
	if err != nil {
		t.Fatal(err)
	}
	for name, info := range map[string]KeyInfo{"Import": imported, "Get": got} {
		if !bytes.Equal(info.PubKey, pub) {
			t.Errorf("%s: public key is %d bytes and not the keypair's; want the %d-byte public key",
				name, len(info.PubKey), len(pub))
		}
	}
	list, err := b.List()
	if err != nil || len(list) != 1 || !bytes.Equal(list[0].PubKey, pub) {
		t.Errorf("List does not report the public key: %v", err)
	}

	msg := []byte("register-key-v2 proof")
	sig, err := b.Sign("node", msg)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := pqc.DilithiumVerify(got.PubKey, msg, sig); err != nil || !ok {
		t.Fatalf("signature does not verify under the reported public key: ok=%v err=%v", ok, err)
	}
}

// Every node onboarded before v3.1.7 has a keystore entry with an empty public
// key on disk. Reading it must repair the answer without the operator
// re-creating the key, which would also change their registered PQC key.
func TestKeystoreWrittenWithoutPublicKeyIsRepairedOnRead(t *testing.T) {
	pub, priv, err := pqc.DilithiumKeygen()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewEncryptedFileBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b.SetPassphrase("test")
	// Exactly what a v3.1.6 import left on disk.
	if err := b.saveEntries(map[string]keyEntry{
		"validator": {Info: KeyInfo{Name: "validator", Type: KeyTypeDilithium5}, PrivKey: priv},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := b.Get("validator")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.PubKey, pub) {
		t.Fatalf("legacy entry still reports a %d-byte public key", len(got.PubKey))
	}
}
