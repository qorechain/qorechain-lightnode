package keyring

import (
	"encoding/json"
	"fmt"

	gokeyring "github.com/zalando/go-keyring"
)

const serviceName = "qorechain-lightnode"

// OSKeychainBackend stores keys in the OS keychain.
type OSKeychainBackend struct{}

// NewOSKeychainBackend creates an OS keychain backend.
func NewOSKeychainBackend() (*OSKeychainBackend, error) {
	return &OSKeychainBackend{}, nil
}

func (b *OSKeychainBackend) loadEntries() (map[string]keyEntry, error) {
	data, err := gokeyring.Get(serviceName, "keys")
	if err != nil {
		// No keys stored yet
		return make(map[string]keyEntry), nil
	}
	var entries map[string]keyEntry
	if err := json.Unmarshal([]byte(data), &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (b *OSKeychainBackend) saveEntries(entries map[string]keyEntry) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return gokeyring.Set(serviceName, "keys", string(data))
}

func (b *OSKeychainBackend) Create(name string, keyType KeyType) (KeyInfo, error) {
	entries, err := b.loadEntries()
	if err != nil {
		return KeyInfo{}, err
	}
	if _, exists := entries[name]; exists {
		return KeyInfo{}, fmt.Errorf("key %q already exists", name)
	}
	// Generation delegated to PQC package
	return KeyInfo{}, fmt.Errorf("key generation not yet implemented for %s", keyType)
}

func (b *OSKeychainBackend) Import(name string, keyType KeyType, privkey []byte) (KeyInfo, error) {
	entries, err := b.loadEntries()
	if err != nil {
		return KeyInfo{}, err
	}
	if _, exists := entries[name]; exists {
		return KeyInfo{}, fmt.Errorf("key %q already exists", name)
	}
	info := KeyInfo{Name: name, Type: keyType}
	entries[name] = keyEntry{Info: info, PrivKey: privkey}
	if err := b.saveEntries(entries); err != nil {
		return KeyInfo{}, err
	}
	return info, nil
}

func (b *OSKeychainBackend) Export(name string) ([]byte, error) {
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

func (b *OSKeychainBackend) Sign(name string, message []byte) ([]byte, error) {
	return nil, fmt.Errorf("signing not yet implemented")
}

func (b *OSKeychainBackend) List() ([]KeyInfo, error) {
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

func (b *OSKeychainBackend) Delete(name string) error {
	entries, err := b.loadEntries()
	if err != nil {
		return err
	}
	delete(entries, name)
	return b.saveEntries(entries)
}

func (b *OSKeychainBackend) Get(name string) (KeyInfo, error) {
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
