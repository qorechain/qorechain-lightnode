package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeQorechaind writes a qorechaind stand-in that logs each call and answers
// like the real one: generate prints a document, cosign answers a sync
// broadcast with code 0, and `query tx` finds the transaction only from its
// second look, with the block code given.
func fakeQorechaind(t *testing.T, blockCode string) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls.log")
	seen := filepath.Join(dir, "seen")
	script := `#!/bin/sh
echo "start $2 $$" >> ` + log + `
case "$1 $2" in
  "tx lightnode") echo '{"body":{}}' ;;
  "tx pqc") sleep 0.2; echo '{"txhash":"H1","code":0,"raw_log":""}' ;;
  "query tx")
    if [ -f ` + seen + ` ]; then echo '{"txhash":"H1","code":` + blockCode + `,"raw_log":"insufficient delegated stake"}'
    else touch ` + seen + `; echo "tx (H1) not found" >&2; echo "end $2 $$" >> ` + log + `; exit 1; fi ;;
esac
echo "end $2 $$" >> ` + log + `
`
	bin = filepath.Join(dir, "qorechaind")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func testSigner(bin string) *cliSigner {
	return &cliSigner{binary: bin, chainID: "c", node: "https://rpc.example", fees: "1uqor", gas: "1",
		inclusionWait: 5 * time.Second, poll: 10 * time.Millisecond}
}

// A sync broadcast's code 0 is the mempool's answer. The block can still
// refuse the transaction, and Submit must say so instead of reporting success.
func TestSubmitReportsTheBlockVerdict(t *testing.T) {
	bin, _ := fakeQorechaind(t, "13")
	_, err := testSigner(bin).Submit(context.Background(), "operator", "node", "lightnode", "register", "sx", "3.1.7")
	if err == nil || !strings.Contains(err.Error(), "code 13") {
		t.Fatalf("err = %v, want the block's code 13", err)
	}

	bin, _ = fakeQorechaind(t, "0")
	res, err := testSigner(bin).Submit(context.Background(), "operator", "node", "lightnode", "heartbeat")
	if err != nil || res.TxHash != "H1" {
		t.Fatalf("included transaction: res=%+v err=%v", res, err)
	}
}

// The heartbeat and the claim share an account. Built in parallel they carry
// the same sequence and the chain refuses the second (code 32), which is what
// the devnet showed on 2026-10-16. Submissions must not overlap.
func TestSubmissionsDoNotOverlap(t *testing.T) {
	bin, log := fakeQorechaind(t, "0")
	s := testSigner(bin)

	var wg sync.WaitGroup
	for _, msg := range []string{"heartbeat", "claim-rewards"} {
		wg.Add(1)
		go func(msg string) {
			defer wg.Done()
			_, _ = s.Submit(context.Background(), "operator", "node", "lightnode", msg)
		}(msg)
	}
	wg.Wait()

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	// Each submission is generate, cosign, then one or more queries. With one
	// at a time, the second generate starts only after the first submission's
	// last query has ended.
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	open := 0
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "start"):
			open++
		case strings.HasPrefix(l, "end"):
			open--
		}
		if open > 1 {
			t.Fatalf("two qorechaind calls ran at once:\n%s", b)
		}
	}
	firstGen, secondGen := -1, -1
	lastQueryBeforeSecond := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "start lightnode") {
			if firstGen < 0 {
				firstGen = i
			} else {
				secondGen = i
			}
		}
		if secondGen < 0 && strings.HasPrefix(l, "end tx") {
			lastQueryBeforeSecond = i
		}
	}
	if secondGen < 0 || lastQueryBeforeSecond < 0 || lastQueryBeforeSecond > secondGen {
		t.Fatalf("the second transaction was built before the first was in a block:\n%s", b)
	}
}
