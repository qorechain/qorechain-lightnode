package cli

import (
	"strings"
	"testing"
)

// QSR-2026-0830: a private key given as an argument is visible in the process
// list and kept in shell history. It is refused unless --unsafe-argv.
func TestKeysImportRefusesAKeyOnTheCommandLine(t *testing.T) {
	cmd := keysImportCmd()
	cmd.SetArgs([]string{"node", "deadbeef"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "not accepted on the command line") {
		t.Fatalf("err %v, want the command-line key refused", err)
	}
}
