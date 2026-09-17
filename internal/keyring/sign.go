package keyring

import (
	"fmt"

	"github.com/qorechain/qorechain-lightnode/internal/pqc"
)

// signEntry produces a signature over message with the stored private key.
//
// Both backends used to return "signing requires PQC library" here, from before
// the PQC implementation was pure Go. It has been pure Go (ML-DSA-87 via circl)
// for several releases, and every other operation on the key already went
// through it; only signing was left behind, so a keyring that could create,
// list and export a key could not use it.
func signEntry(entry keyEntry, message []byte) ([]byte, error) {
	switch entry.Info.Type {
	case KeyTypeDilithium5:
		return pqc.DilithiumSign(entry.PrivKey, message)
	default:
		return nil, fmt.Errorf("unsupported key type for signing: %s", entry.Info.Type)
	}
}
