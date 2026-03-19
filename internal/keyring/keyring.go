package keyring

import (
	"fmt"
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

// New creates a keyring backend based on the type.
func New(backendType string, dataDir string) (Backend, error) {
	switch backendType {
	case "file":
		return NewEncryptedFileBackend(dataDir)
	case "os":
		return NewOSKeychainBackend()
	default:
		return nil, fmt.Errorf("unknown keyring backend: %s", backendType)
	}
}
