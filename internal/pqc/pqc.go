package pqc

/*
#cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/../../lib/darwin_arm64 -lqorepqc
#cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/../../lib/darwin_amd64 -lqorepqc
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../../lib/linux_amd64 -lqorepqc
#cgo linux,arm64 LDFLAGS: -L${SRCDIR}/../../lib/linux_arm64 -lqorepqc

#include "pqc.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const (
	DilithiumPublicKeySize  = 2592
	DilithiumPrivateKeySize = 4896
	DilithiumSignatureSize  = 4627
)

// DilithiumKeygen generates a new Dilithium-5 keypair.
func DilithiumKeygen() (pubkey []byte, privkey []byte, err error) {
	pk := make([]byte, DilithiumPublicKeySize)
	sk := make([]byte, DilithiumPrivateKeySize)

	rc := C.qore_dilithium_keygen(
		(*C.uint8_t)(unsafe.Pointer(&pk[0])),
		(*C.uint8_t)(unsafe.Pointer(&sk[0])),
	)
	if rc != 0 {
		return nil, nil, fmt.Errorf("dilithium keygen failed: code %d", rc)
	}
	return pk, sk, nil
}

// DilithiumSign signs a message with a Dilithium-5 private key.
func DilithiumSign(privkey, message []byte) ([]byte, error) {
	if len(privkey) != DilithiumPrivateKeySize {
		return nil, fmt.Errorf("invalid private key size: got %d, want %d", len(privkey), DilithiumPrivateKeySize)
	}
	sig := make([]byte, DilithiumSignatureSize)
	var sigLen C.size_t

	rc := C.qore_dilithium_sign(
		(*C.uint8_t)(unsafe.Pointer(&sig[0])),
		&sigLen,
		(*C.uint8_t)(unsafe.Pointer(&message[0])),
		C.size_t(len(message)),
		(*C.uint8_t)(unsafe.Pointer(&privkey[0])),
	)
	if rc != 0 {
		return nil, fmt.Errorf("dilithium sign failed: code %d", rc)
	}
	return sig[:sigLen], nil
}

// DilithiumVerify verifies a Dilithium-5 signature.
func DilithiumVerify(pubkey, message, signature []byte) (bool, error) {
	if len(pubkey) != DilithiumPublicKeySize {
		return false, fmt.Errorf("invalid public key size: got %d, want %d", len(pubkey), DilithiumPublicKeySize)
	}

	rc := C.qore_dilithium_verify(
		(*C.uint8_t)(unsafe.Pointer(&signature[0])),
		C.size_t(len(signature)),
		(*C.uint8_t)(unsafe.Pointer(&message[0])),
		C.size_t(len(message)),
		(*C.uint8_t)(unsafe.Pointer(&pubkey[0])),
	)
	return rc == 0, nil
}
