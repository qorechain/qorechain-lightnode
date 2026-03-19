package keyring

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/argon2"
)

const (
	argon2Time    = 3
	argon2Memory  = 64 * 1024 // 64 MB
	argon2Threads = 4
	argon2KeyLen  = 32
	saltLen       = 32
)

// EncryptedFileBackend stores keys encrypted with Argon2id + AES-256-GCM.
type EncryptedFileBackend struct {
	dir        string
	passphrase []byte
	mu         sync.RWMutex
}

type encryptedStore struct {
	Salt  []byte `json:"salt"`
	Nonce []byte `json:"nonce"`
	Data  []byte `json:"data"` // encrypted JSON of map[name]keyEntry
}

type keyEntry struct {
	Info    KeyInfo `json:"info"`
	PrivKey []byte  `json:"privkey"`
}

// NewEncryptedFileBackend creates a file-based keyring.
func NewEncryptedFileBackend(dataDir string) (*EncryptedFileBackend, error) {
	dir := filepath.Join(dataDir, "keystore")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &EncryptedFileBackend{dir: dir}, nil
}

// SetPassphrase sets the encryption passphrase.
func (b *EncryptedFileBackend) SetPassphrase(passphrase string) {
	b.passphrase = []byte(passphrase)
}

func (b *EncryptedFileBackend) keystorePath() string {
	return filepath.Join(b.dir, "keys.enc")
}

func (b *EncryptedFileBackend) loadEntries() (map[string]keyEntry, error) {
	data, err := os.ReadFile(b.keystorePath())
	if os.IsNotExist(err) {
		return make(map[string]keyEntry), nil
	}
	if err != nil {
		return nil, err
	}

	var store encryptedStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}

	key := argon2.IDKey(b.passphrase, store.Salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintext, err := gcm.Open(nil, store.Nonce, store.Data, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong passphrase?): %w", err)
	}

	var entries map[string]keyEntry
	if err := json.Unmarshal(plaintext, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (b *EncryptedFileBackend) saveEntries(entries map[string]keyEntry) error {
	plaintext, err := json.Marshal(entries)
	if err != nil {
		return err
	}

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return err
	}

	key := argon2.IDKey(b.passphrase, salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	store := encryptedStore{Salt: salt, Nonce: nonce, Data: ciphertext}
	data, err := json.Marshal(store)
	if err != nil {
		return err
	}

	return os.WriteFile(b.keystorePath(), data, 0600)
}

func (b *EncryptedFileBackend) Create(name string, keyType KeyType) (KeyInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entries, err := b.loadEntries()
	if err != nil {
		return KeyInfo{}, err
	}
	if _, exists := entries[name]; exists {
		return KeyInfo{}, fmt.Errorf("key %q already exists", name)
	}

	// Generate keypair based on type
	var pubkey, privkey []byte
	switch keyType {
	case KeyTypeDilithium5:
		// Will be implemented when PQC package is wired
		return KeyInfo{}, fmt.Errorf("dilithium5 key generation requires PQC library")
	default:
		return KeyInfo{}, fmt.Errorf("unsupported key type: %s", keyType)
	}

	info := KeyInfo{
		Name:   name,
		Type:   keyType,
		PubKey: pubkey,
	}
	entries[name] = keyEntry{Info: info, PrivKey: privkey}

	if err := b.saveEntries(entries); err != nil {
		return KeyInfo{}, err
	}
	return info, nil
}

func (b *EncryptedFileBackend) Import(name string, keyType KeyType, privkey []byte) (KeyInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entries, err := b.loadEntries()
	if err != nil {
		return KeyInfo{}, err
	}
	if _, exists := entries[name]; exists {
		return KeyInfo{}, fmt.Errorf("key %q already exists", name)
	}

	info := KeyInfo{
		Name: name,
		Type: keyType,
	}
	entries[name] = keyEntry{Info: info, PrivKey: privkey}

	if err := b.saveEntries(entries); err != nil {
		return KeyInfo{}, err
	}
	return info, nil
}

func (b *EncryptedFileBackend) Export(name string) ([]byte, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	entries, err := b.loadEntries()
	if err != nil {
		return nil, err
	}
	entry, exists := entries[name]
	if !exists {
		return nil, fmt.Errorf("key %q not found", name)
	}
	return entry.PrivKey, nil
}

func (b *EncryptedFileBackend) Sign(name string, message []byte) ([]byte, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	entries, err := b.loadEntries()
	if err != nil {
		return nil, err
	}
	entry, exists := entries[name]
	if !exists {
		return nil, fmt.Errorf("key %q not found", name)
	}

	switch entry.Info.Type {
	case KeyTypeDilithium5:
		// Will use PQC package when wired
		return nil, fmt.Errorf("dilithium5 signing requires PQC library")
	default:
		return nil, fmt.Errorf("unsupported key type for signing: %s", entry.Info.Type)
	}
}

func (b *EncryptedFileBackend) List() ([]KeyInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	entries, err := b.loadEntries()
	if err != nil {
		return nil, err
	}
	var infos []KeyInfo
	for _, e := range entries {
		infos = append(infos, e.Info)
	}
	return infos, nil
}

func (b *EncryptedFileBackend) Delete(name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	entries, err := b.loadEntries()
	if err != nil {
		return err
	}
	if _, exists := entries[name]; !exists {
		return fmt.Errorf("key %q not found", name)
	}
	delete(entries, name)
	return b.saveEntries(entries)
}

func (b *EncryptedFileBackend) Get(name string) (KeyInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	entries, err := b.loadEntries()
	if err != nil {
		return KeyInfo{}, err
	}
	entry, exists := entries[name]
	if !exists {
		return KeyInfo{}, fmt.Errorf("key %q not found", name)
	}
	return entry.Info, nil
}
