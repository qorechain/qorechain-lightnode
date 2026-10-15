package chaintx

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/cosmos/go-bip39"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/ripemd160" //nolint:staticcheck // Cosmos addresses are RIPEMD-160; there is no replacement to choose
)

// AccountPrefix is the bech32 prefix of QoreChain accounts.
const AccountPrefix = "qor"

// CosmosHDPath is the derivation path qorechaind and Keplr use for a
// secp256k1 account (coin type 118, first account, first address).
var CosmosHDPath = []uint32{44 | hardened, 118 | hardened, 0 | hardened, 0, 0}

const hardened = 0x80000000

// OperatorKey is the operator account's secp256k1 key, the classical half of
// every hybrid signature and the only signature on a key registration.
type OperatorKey struct {
	priv *secp256k1.PrivateKey
}

// NewOperatorKey generates a fresh key and the 24-word mnemonic that restores
// it. The mnemonic is the backup: it is shown once and never stored.
func NewOperatorKey() (OperatorKey, string, error) {
	entropy, err := bip39.NewEntropy(256)
	if err != nil {
		return OperatorKey{}, "", err
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return OperatorKey{}, "", err
	}
	k, err := OperatorKeyFromMnemonic(mnemonic)
	return k, mnemonic, err
}

// OperatorKeyFromMnemonic derives the key qorechaind `keys add --recover` and
// Keplr derive from the same words, so an operator can bring the account their
// licence was bought for.
func OperatorKeyFromMnemonic(mnemonic string) (OperatorKey, error) {
	mnemonic = strings.Join(strings.Fields(mnemonic), " ")
	seed, err := bip39.NewSeedWithErrorChecking(mnemonic, "")
	if err != nil {
		return OperatorKey{}, fmt.Errorf("mnemonic: %w", err)
	}
	priv, err := derivePath(seed, CosmosHDPath)
	if err != nil {
		return OperatorKey{}, err
	}
	return OperatorKeyFromBytes(priv)
}

// OperatorKeyFromBytes loads a 32-byte secp256k1 private key.
func OperatorKeyFromBytes(b []byte) (OperatorKey, error) {
	if len(b) != 32 {
		return OperatorKey{}, fmt.Errorf("secp256k1 private key is %d bytes, want 32", len(b))
	}
	var s secp256k1.ModNScalar
	if overflow := s.SetByteSlice(b); overflow || s.IsZero() {
		return OperatorKey{}, errors.New("secp256k1 private key is out of range")
	}
	return OperatorKey{priv: secp256k1.NewPrivateKey(&s)}, nil
}

// Bytes is the 32-byte private key, for the keyring.
func (k OperatorKey) Bytes() []byte { return k.priv.Serialize() }

// PubKey is the 33-byte compressed public key.
func (k OperatorKey) PubKey() []byte { return k.priv.PubKey().SerializeCompressed() }

// Address is the account's bech32 qor1... address: RIPEMD-160(SHA-256(pubkey)).
func (k OperatorKey) Address() string {
	sh := sha256.Sum256(k.PubKey())
	r := ripemd160.New()
	r.Write(sh[:])
	addr, err := bech32Encode(AccountPrefix, r.Sum(nil))
	if err != nil {
		// 20 bytes always encode; an error here is a programming error.
		panic(err)
	}
	return addr
}

// pubKeyAny is the key as the SignerInfo carries it.
func (k OperatorKey) pubKeyAny() Any {
	return Any{TypeURL: typeSecp256k1PubKey, Value: putBytes(nil, 1, k.PubKey())}
}

// sign is the Cosmos secp256k1 signature over msg: ECDSA over SHA-256(msg),
// encoded as 64 bytes r||s with s in the lower half (RFC 6979, as the SDK).
func (k OperatorKey) sign(msg []byte) []byte {
	h := sha256.Sum256(msg)
	sig := ecdsa.Sign(k.priv, h[:])
	r, s := sig.R(), sig.S()
	if s.IsOverHalfOrder() {
		s.Negate()
	}
	out := make([]byte, 64)
	r.PutBytesUnchecked(out[:32])
	s.PutBytesUnchecked(out[32:])
	return out
}

// derivePath is BIP-32 private derivation from a BIP-39 seed.
func derivePath(seed []byte, path []uint32) ([]byte, error) {
	mac := hmac.New(sha512.New, []byte("Bitcoin seed"))
	mac.Write(seed)
	I := mac.Sum(nil)
	key, chain := I[:32], I[32:]
	for _, idx := range path {
		var data []byte
		if idx&hardened != 0 {
			data = append([]byte{0}, key...)
		} else {
			var s secp256k1.ModNScalar
			s.SetByteSlice(key)
			data = secp256k1.NewPrivateKey(&s).PubKey().SerializeCompressed()
		}
		data = binary.BigEndian.AppendUint32(data, idx)
		mac := hmac.New(sha512.New, chain)
		mac.Write(data)
		I := mac.Sum(nil)

		var il, k secp256k1.ModNScalar
		if overflow := il.SetByteSlice(I[:32]); overflow {
			return nil, errors.New("bip32: derived key out of range")
		}
		k.SetByteSlice(key)
		k.Add(&il)
		if k.IsZero() {
			return nil, errors.New("bip32: derived key is zero")
		}
		kb := k.Bytes()
		key, chain = kb[:], I[32:]
	}
	return key, nil
}

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

func bech32Polymod(values []byte) uint32 {
	gen := [5]uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := uint32(1)
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (top>>uint(i))&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

func bech32HRPExpand(hrp string) []byte {
	out := make([]byte, 0, len(hrp)*2+1)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]>>5)
	}
	out = append(out, 0)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]&31)
	}
	return out
}

func bech32Encode(hrp string, data []byte) (string, error) {
	// 8-bit to 5-bit groups, padded.
	var values []byte
	acc, bits := uint32(0), uint(0)
	for _, b := range data {
		acc = acc<<8 | uint32(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			values = append(values, byte(acc>>bits)&31)
		}
	}
	if bits > 0 {
		values = append(values, byte(acc<<(5-bits))&31)
	}
	poly := bech32Polymod(append(append(bech32HRPExpand(hrp), values...), 0, 0, 0, 0, 0, 0)) ^ 1
	for i := 0; i < 6; i++ {
		values = append(values, byte(poly>>uint(5*(5-i)))&31)
	}
	var sb strings.Builder
	sb.WriteString(hrp)
	sb.WriteByte('1')
	for _, v := range values {
		sb.WriteByte(bech32Charset[v])
	}
	return sb.String(), nil
}
