package keyring

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/pqc"
)

// QSR-2026-0831: two processes saving at once each loaded the old map, and the
// later save dropped the other's key. Separate backends on one directory stand
// in for separate processes: they share nothing but the files.
func TestConcurrentSavesFromSeparateBackendsKeepEveryKey(t *testing.T) {
	dir := t.TempDir()
	_, priv, err := pqc.DilithiumKeygen()
	if err != nil {
		t.Fatal(err)
	}
	const n = 6
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, err := NewEncryptedFileBackend(dir)
			if err != nil {
				errs <- err
				return
			}
			b.SetPassphrase("test")
			if _, err := b.Import(fmt.Sprintf("k%d", i), KeyTypeDilithium5, priv); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	b, _ := NewEncryptedFileBackend(dir)
	b.SetPassphrase("test")
	keys, err := b.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != n {
		t.Fatalf("%d keys after %d concurrent imports, want all of them", len(keys), n)
	}
	if _, err := os.Stat(filepath.Join(dir, "keystore", "keys.lock")); !os.IsNotExist(err) {
		t.Fatal("the lock file was left behind")
	}
}

// The previous generation is kept, and the file is never left half written:
// the temporary file is renamed over it, so a crash leaves the old or the new.
func TestSaveKeepsThePreviousKeystoreAsBak(t *testing.T) {
	dir := t.TempDir()
	b, _ := NewEncryptedFileBackend(dir)
	b.SetPassphrase("test")
	_, priv, _ := pqc.DilithiumKeygen()
	if _, err := b.Import("a", KeyTypeDilithium5, priv); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "keystore", "keys.enc"))
	if _, err := b.Import("b", KeyTypeDilithium5, priv); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(filepath.Join(dir, "keystore", "keys.enc.bak"))
	if err != nil || string(bak) != string(first) {
		t.Fatalf("keys.enc.bak is not the previous keystore (err %v)", err)
	}
	if st, _ := os.Stat(filepath.Join(dir, "keystore", "keys.enc")); st.Mode().Perm() != 0600 {
		t.Fatalf("keys.enc mode %v, want 0600", st.Mode().Perm())
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "keystore", "keys.enc.tmp-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
}

// A lock left by a process that died is taken over, not waited on forever.
func TestStaleLockIsTakenOver(t *testing.T) {
	dir := t.TempDir()
	b, _ := NewEncryptedFileBackend(dir)
	b.SetPassphrase("test")
	lock := filepath.Join(dir, "keystore", "keys.lock")
	if err := os.WriteFile(lock, []byte("999999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-lockStale - time.Minute)
	os.Chtimes(lock, old, old) //nolint:errcheck
	_, priv, _ := pqc.DilithiumKeygen()
	if _, err := b.Import("a", KeyTypeDilithium5, priv); err != nil {
		t.Fatalf("stale lock blocked the save: %v", err)
	}
}
