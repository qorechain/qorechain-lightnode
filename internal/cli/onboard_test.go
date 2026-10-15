//go:build cgo

package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestRunSelftest exercises the operator-facing `selftest` command end to end.
// It must complete without error and report all five PQC checks passing. This
// is the live "proof the PQC stack works" path operators rely on before the
// chain is deployed.
func TestRunSelftest(t *testing.T) {
	var buf bytes.Buffer
	if err := runSelftest(&buf); err != nil {
		t.Fatalf("runSelftest returned error: %v\noutput:\n%s", err, buf.String())
	}

	out := buf.String()
	for _, want := range []string{
		"[1/5] Keygen",
		"[2/5] Sign",
		"[3/5] Verify",
		"[4/5]",
		"[5/5]",
		"All 5 checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("selftest output missing %q\nfull output:\n%s", want, out)
		}
	}
}
