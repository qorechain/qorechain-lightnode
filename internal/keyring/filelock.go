package keyring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Writes to keys.enc are load-modify-save of the whole map. The in-process
// mutex orders the daemon's own writes; this lock orders them against another
// process (two CLI commands, or a CLI command next to the running daemon), which
// otherwise each loaded the old map and the second save dropped the first's key.
//
// A lock file created with O_EXCL works the same on every platform the node
// ships for. A lock older than lockStale is left over from a process that died
// holding it (a key save takes well under a second) and is taken over.
const (
	lockWait  = 30 * time.Second
	lockStale = 2 * time.Minute
	lockPoll  = 50 * time.Millisecond
)

func (b *EncryptedFileBackend) lockPath() string { return filepath.Join(b.dir, "keys.lock") }

// lockFile takes the keystore's cross-process lock and returns its release.
func (b *EncryptedFileBackend) lockFile() (func(), error) {
	path := b.lockPath()
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("keystore lock: %w", err)
		}
		if st, serr := os.Stat(path); serr == nil && time.Since(st.ModTime()) > lockStale {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("keystore is locked by another process (%s); retry, or remove it if no lightnode process is running", path)
		}
		time.Sleep(lockPoll)
	}
}

// writeAtomic replaces path with data so that a reader, or a crash at any
// moment, sees either the old file or the new one, never a truncated one: the
// data goes to a temporary file in the same directory, is synced, and is
// renamed over path. The previous file is kept as path+".bak" first, so one
// bad generation (a wrong passphrase saved over, a disk error) is recoverable.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil && len(old) > 0 {
		if err := writeSynced(path+".bak", old); err != nil {
			return fmt.Errorf("keeping the previous keystore as .bak: %w", err)
		}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	syncDir(dir)
	return nil
}

func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// syncDir makes the rename durable. Directories cannot be opened for sync on
// Windows; the rename there is already durable enough for this use.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync() //nolint:errcheck // best effort, see above
		d.Close()
	}
}
