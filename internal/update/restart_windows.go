//go:build windows

package update

import (
	"os"
	"os/exec"
)

// Restart starts the binary at exe with the same arguments and exits. Windows
// has no exec; a service manager that tracks the PID sees an exit and should
// be set to restart the service.
func Restart(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
