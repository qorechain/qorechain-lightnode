//go:build !windows

package update

import (
	"os"
	"syscall"
)

// Restart replaces this process with the binary at exe, same arguments and
// environment, same PID: a service manager sees no exit.
func Restart(exe string) error {
	return syscall.Exec(exe, os.Args, os.Environ())
}
