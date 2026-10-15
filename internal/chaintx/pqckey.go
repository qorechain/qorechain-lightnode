package chaintx

import (
	"strings"

	qpqc "github.com/qorechain/qorechain-pqc/go"
)

// PQCKey is a Dilithium-5 (ML-DSA-87) keypair and how it was derived.
type PQCKey struct {
	Derivation string // "adapter" or "bridge"
	Pub, Priv  []byte
}

// PQCKeysFromMnemonic derives the post-quantum keys the ecosystem derives from
// an account's words, so a node can sign with the key a wallet already
// registered on the account. Byte-identical to qorechaind `tx pqc recover-key`
// (checked against it on the public test mnemonic):
//
//   - "adapter", the SDK and wallet-adapter default:
//     seed = SHAKE-256("qorechain:pqc:v1|" + address + "|" + mnemonic, 32)
//   - "bridge", the dashboard's older form: seed = SHAKE-256(mnemonic, 32)
//
// adapter comes first: it is what a new key should be.
func PQCKeysFromMnemonic(address, mnemonic string) ([]PQCKey, error) {
	mnemonic = strings.Join(strings.Fields(mnemonic), " ")
	seeds := []struct{ name, input string }{
		{"adapter", "qorechain:pqc:v1|" + address + "|" + mnemonic},
		{"bridge", mnemonic},
	}
	out := make([]PQCKey, 0, len(seeds))
	for _, s := range seeds {
		pub, priv, err := qpqc.MLDSA87.KeygenFromSeed(qpqc.Shake256([]byte(s.input), 32))
		if err != nil {
			return nil, err
		}
		out = append(out, PQCKey{Derivation: s.name, Pub: pub, Priv: priv})
	}
	return out, nil
}
