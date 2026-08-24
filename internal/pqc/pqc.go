// Package pqc provides Dilithium-5 (ML-DSA-87, FIPS-204) signing for the light
// node.
//
// This is a pure-Go implementation backed by cloudflare/circl. It replaces the
// previous cgo bridge to libqorepqc, which had three problems:
//
//   - It needed a native library per platform. lib/windows_amd64 and
//     lib/darwin_amd64 never existed, so the release workflow built those
//     targets with CGO_ENABLED=0 and silently shipped binaries whose keygen,
//     sign and verify all returned errors. Operators on Windows and macOS could
//     start the node and watch the dashboard, but could not register on chain.
//   - A shipped library can go stale against the chain. It had before: the
//     bundled build predated the FIPS-204 migration and produced signatures the
//     chain rejected with code 21.
//   - It was slower. Measured on this chain, circl verifies in ~0.033 ms against
//     ~0.290 ms through the FFI.
//
// Interoperability with the chain is not assumed. It was proven on 2026-08-24
// against libqorepqc.so sha256 9d335cc3... — the build running on mainnet — in
// both directions: signatures produced here verify there, signatures produced
// there verify here, and both reject tampered input. See TestChainInterop.
package pqc

import (
	"crypto/rand"
	"fmt"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
)

// Dilithium-5 (ML-DSA-87) constant sizes, fixed by FIPS-204.
const (
	DilithiumPublicKeySize  = mldsa87.PublicKeySize  // 2592
	DilithiumPrivateKeySize = mldsa87.PrivateKeySize // 4896
	DilithiumSignatureSize  = mldsa87.SignatureSize  // 4627
)

// signCtx is the FIPS-204 context string. The chain signs with an empty
// context, so anything else here produces signatures it will reject.
var signCtx []byte

// DilithiumKeygen generates a new ML-DSA-87 keypair, returning both halves in
// their packed FIPS-204 encoding.
func DilithiumKeygen() (pubkey []byte, privkey []byte, err error) {
	pk, sk, err := mldsa87.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("dilithium keygen: %w", err)
	}
	return pk.Bytes(), sk.Bytes(), nil
}

// DilithiumSign signs message with a packed private key.
func DilithiumSign(privkey, message []byte) ([]byte, error) {
	// FIPS-204 permits signing an empty message, but nothing in this codebase
	// ever means to: an empty message here is a caller that passed nothing.
	// The previous FFI rejected it, and that guard is worth keeping.
	if len(message) == 0 {
		return nil, fmt.Errorf("dilithium sign: empty message")
	}
	if len(privkey) != DilithiumPrivateKeySize {
		return nil, fmt.Errorf("dilithium sign: private key is %d bytes, want %d",
			len(privkey), DilithiumPrivateKeySize)
	}
	var buf [DilithiumPrivateKeySize]byte
	copy(buf[:], privkey)
	var sk mldsa87.PrivateKey
	sk.Unpack(&buf)

	sig := make([]byte, DilithiumSignatureSize)
	// randomized=true is the FIPS-204 hedged variant: it mixes fresh entropy
	// into the signature. Verification is unaffected either way.
	if err := mldsa87.SignTo(&sk, message, signCtx, true, sig); err != nil {
		return nil, fmt.Errorf("dilithium sign: %w", err)
	}
	return sig, nil
}

// DilithiumVerify reports whether signature is valid for message under pubkey.
// A malformed input is an error; a well-formed but wrong signature is (false, nil).
func DilithiumVerify(pubkey, message, signature []byte) (bool, error) {
	if len(pubkey) != DilithiumPublicKeySize {
		return false, fmt.Errorf("dilithium verify: public key is %d bytes, want %d",
			len(pubkey), DilithiumPublicKeySize)
	}
	if len(signature) != DilithiumSignatureSize {
		return false, fmt.Errorf("dilithium verify: signature is %d bytes, want %d",
			len(signature), DilithiumSignatureSize)
	}
	var buf [DilithiumPublicKeySize]byte
	copy(buf[:], pubkey)
	var pk mldsa87.PublicKey
	pk.Unpack(&buf)

	return mldsa87.Verify(&pk, message, signCtx, signature), nil
}
