package keyring

import (
	"fmt"
	"os"
)

// KeyType identifies the cryptographic algorithm.
type KeyType string

const (
	KeyTypeSecp256k1  KeyType = "secp256k1"
	KeyTypeEd25519    KeyType = "ed25519"
	KeyTypeDilithium5 KeyType = "dilithium5"
)

// KeyInfo holds metadata about a stored key.
type KeyInfo struct {
	Name    string  `json:"name"`
	Type    KeyType `json:"type"`
	Address string  `json:"address"` // bech32 address (qor1...)
	PubKey  []byte  `json:"pubkey"`
}

// Backend defines the keyring storage interface.
type Backend interface {
	// Create generates a new key and stores it.
	Create(name string, keyType KeyType) (KeyInfo, error)
	// Import imports a private key.
	Import(name string, keyType KeyType, privkey []byte) (KeyInfo, error)
	// Export exports the private key (requires passphrase for file backend).
	Export(name string) ([]byte, error)
	// Sign signs a message using the named key.
	Sign(name string, message []byte) ([]byte, error)
	// List returns all stored keys.
	List() ([]KeyInfo, error)
	// Delete removes a key.
	Delete(name string) error
	// Get returns key info.
	Get(name string) (KeyInfo, error)
}

// PassphraseEnv is the environment variable the file backend reads its passphrase
// from. It is read at open time and never written anywhere.
const PassphraseEnv = "QORE_LIGHTNODE_KEYRING_PASSPHRASE"

// New creates a keyring backend based on the type.
func New(backendType string, dataDir string) (Backend, error) {
	switch backendType {
	case "file":
		// THE FILE BACKEND REQUIRES A PASSPHRASE. It used to be created with none,
		// and the only caller that ever set one was the "test" backend below, so the
		// production keystore was encrypted under an empty key while the development
		// one had a real one. AES-GCM under an argon2 derivation of an empty
		// passphrase is a keystore that opens for anyone holding the file, and a
		// working recovery of a 4896-byte ML-DSA-87 secret key from such a file was
		// supplied to us. The passphrase comes from the environment so an unattended
		// daemon can start; an empty one is refused rather than defaulted.
		b, err := NewEncryptedFileBackend(dataDir)
		if err != nil {
			return nil, err
		}
		pass := os.Getenv(PassphraseEnv)
		if pass == "" {
			return nil, fmt.Errorf("keyring backend %q needs a passphrase: set %s "+
				"(use the \"test\" backend only for local devnets)", backendType, PassphraseEnv)
		}
		b.SetPassphrase(pass)
		return b, nil
	case "os":
		return NewOSKeychainBackend()
	case "test":
		// Unattended dev/test backend: an encrypted-file keyring with a fixed,
		// well-known passphrase (mirrors the Cosmos SDK "test" keyring). Suitable
		// for local devnets and automation, not for production key material.
		b, err := NewEncryptedFileBackend(dataDir)
		if err != nil {
			return nil, err
		}
		b.SetPassphrase("test")
		return b, nil
	default:
		return nil, fmt.Errorf("unknown keyring backend: %s", backendType)
	}
}
