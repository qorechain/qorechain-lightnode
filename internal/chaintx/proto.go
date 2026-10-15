// Package chaintx builds, signs and broadcasts QoreChain transactions in pure
// Go, so the light node needs no qorechaind binary.
//
// Until v3.1.8 every transaction went through the qorechaind CLI, which is
// published for linux/amd64 only. On macOS, Windows, Linux arm64 and Android a
// node could run and verify headers but could never register, heartbeat or
// claim. Everything here is CGO-free: ML-DSA-87 from cloudflare/circl (through
// qorechain-pqc), secp256k1 from decred, protobuf by hand with protowire.
//
// The transaction format is the chain's hybrid scheme, taken from
// x/pqc/client/cli/hybrid_sign.go (core v3.2.3 6581e40) and verified against a
// v3.2.3 devnet with classical fallback disabled, as on mainnet:
//
//   - b0 is the TxBody WITHOUT the PQC extension; the chain re-derives it by
//     decoding the body, dropping the extension and re-marshalling, so the body
//     must be canonical (fields in tag order, nothing unknown).
//   - the ML-DSA-87 signature covers HybridSignBytes(version, chainID, b0,
//     authInfo) and travels as a PQCHybridSignature extension option;
//   - the secp256k1 signature covers the SignDoc of the body WITH the extension.
//
// Getting the last two backwards is the classic failure: the PQC check fails,
// or the classical one does (unauthorized, code 4).
package chaintx

import (
	"google.golang.org/protobuf/encoding/protowire"
)

// The encoders below write proto3 the way gogoproto does: fields in tag order,
// zero scalars and empty strings/bytes omitted, embedded messages written when
// present.

func putString(b []byte, n protowire.Number, s string) []byte {
	if s == "" {
		return b
	}
	b = protowire.AppendTag(b, n, protowire.BytesType)
	return protowire.AppendString(b, s)
}

func putBytes(b []byte, n protowire.Number, v []byte) []byte {
	if len(v) == 0 {
		return b
	}
	b = protowire.AppendTag(b, n, protowire.BytesType)
	return protowire.AppendBytes(b, v)
}

// putMessage writes an embedded message even when it encodes to nothing: a
// present-but-empty message is not the same as an absent one.
func putMessage(b []byte, n protowire.Number, msg []byte) []byte {
	b = protowire.AppendTag(b, n, protowire.BytesType)
	return protowire.AppendBytes(b, msg)
}

func putUvarint(b []byte, n protowire.Number, v uint64) []byte {
	if v == 0 {
		return b
	}
	b = protowire.AppendTag(b, n, protowire.VarintType)
	return protowire.AppendVarint(b, v)
}

// Any is google.protobuf.Any.
type Any struct {
	TypeURL string
	Value   []byte
}

func (a Any) marshal() []byte {
	var b []byte
	b = putString(b, 1, a.TypeURL)
	b = putBytes(b, 2, a.Value)
	return b
}

// Coin is cosmos.base.v1beta1.Coin.
type Coin struct {
	Denom  string
	Amount string
}

func (c Coin) marshal() []byte {
	var b []byte
	b = putString(b, 1, c.Denom)
	b = putString(b, 2, c.Amount)
	return b
}

// txBody is cosmos.tx.v1beta1.TxBody with the fields this client uses:
// messages (1) and extension_options (1023). Memo, timeout height and the
// non-critical extensions stay empty.
func txBody(msgs []Any, ext []Any) []byte {
	var b []byte
	for _, m := range msgs {
		b = putMessage(b, 1, m.marshal())
	}
	for _, e := range ext {
		b = putMessage(b, 1023, e.marshal())
	}
	return b
}

// signModeDirect is cosmos.tx.signing.v1beta1.SignMode SIGN_MODE_DIRECT.
const signModeDirect = 1

// authInfo is cosmos.tx.v1beta1.AuthInfo for one signer in SIGN_MODE_DIRECT.
func authInfo(pubKey Any, sequence uint64, fee []Coin, gasLimit uint64) []byte {
	var single []byte
	single = putUvarint(single, 1, signModeDirect)
	var modeInfo []byte
	modeInfo = putMessage(modeInfo, 1, single)

	var signer []byte
	signer = putMessage(signer, 1, pubKey.marshal())
	signer = putMessage(signer, 2, modeInfo)
	signer = putUvarint(signer, 3, sequence)

	var feeMsg []byte
	for _, c := range fee {
		feeMsg = putMessage(feeMsg, 1, c.marshal())
	}
	feeMsg = putUvarint(feeMsg, 2, gasLimit)

	var b []byte
	b = putMessage(b, 1, signer)
	b = putMessage(b, 2, feeMsg)
	return b
}

// signDoc is cosmos.tx.v1beta1.SignDoc, what the classical key signs.
func signDoc(body, auth []byte, chainID string, accountNumber uint64) []byte {
	var b []byte
	b = putBytes(b, 1, body)
	b = putBytes(b, 2, auth)
	b = putString(b, 3, chainID)
	b = putUvarint(b, 4, accountNumber)
	return b
}

// txRaw is cosmos.tx.v1beta1.TxRaw, what is broadcast.
func txRaw(body, auth []byte, sigs ...[]byte) []byte {
	var b []byte
	b = putBytes(b, 1, body)
	b = putBytes(b, 2, auth)
	for _, s := range sigs {
		b = protowire.AppendTag(b, 3, protowire.BytesType)
		b = protowire.AppendBytes(b, s)
	}
	return b
}
