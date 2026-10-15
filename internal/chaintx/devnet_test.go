package chaintx

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	qpqc "github.com/qorechain/qorechain-pqc/go"
)

// TestDevnetEndToEnd runs the whole operator path through this package alone,
// against a live chain with classical fallback disabled, as mainnet has it:
// register the PQC key (classical), delegate, register the node, heartbeat,
// claim (all hybrid). No qorechaind is involved.
//
// It needs a devnet and a script that funds an address and grants it the
// lightnode_operator licence:
//
//	QORE_DEVNET_LCD=http://127.0.0.1:41417 QORE_DEVNET_CHAIN_ID=qorechain-devnet323 \
//	QORE_DEVNET_FUND=/path/to/fund-and-grant.sh go test ./internal/chaintx -run Devnet -v
func TestDevnetEndToEnd(t *testing.T) {
	lcd, chainID, fund := os.Getenv("QORE_DEVNET_LCD"), os.Getenv("QORE_DEVNET_CHAIN_ID"), os.Getenv("QORE_DEVNET_FUND")
	if lcd == "" || chainID == "" || fund == "" {
		t.Skip("set QORE_DEVNET_LCD, QORE_DEVNET_CHAIN_ID and QORE_DEVNET_FUND to run against a devnet")
	}
	ctx := context.Background()

	key, _, err := NewOperatorKey()
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := qpqc.MLDSA87.Keygen()
	if err != nil {
		t.Fatal(err)
	}
	op := key.Address()
	t.Logf("operator %s", op)

	if out, err := exec.Command("bash", fund, op).CombinedOutput(); err != nil {
		t.Fatalf("funding: %v\n%s", err, out)
	}
	s := NewSigner(lcd, chainID, key, pub, priv)

	pop, err := s.SupportsProofOfPossession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := MsgRegisterPQCKeyV2(chainID, op, pub, priv, pop)
	if err != nil {
		t.Fatal(err)
	}
	step := func(name string, r Result, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%s: code %d at height %d (%s)", name, r.Code, r.Height, r.TxHash)
	}
	r, err := s.SubmitClassical(ctx, 400000, reg)
	step("register-key-v2 (proof of possession: "+map[bool]string{true: "yes", false: "no"}[pop]+")", r, err)

	val := firstBondedValidator(t, lcd)
	r, err = s.Submit(ctx, 400000, MsgDelegate(op, val, Coin{Denom: "uqor", Amount: "1000000000"}))
	step("delegate", r, err)

	r, err = s.Submit(ctx, 300000, MsgRegisterLightNode(op, "sx", "3.1.8"))
	step("register light node", r, err)

	r, err = s.Submit(ctx, 300000, MsgHeartbeat(op))
	step("heartbeat", r, err)

	// A node registered seconds ago is not eligible yet; the module saying so
	// means the transaction passed both signature checks and executed.
	r, err = s.Submit(ctx, 300000, MsgClaimRewards(op))
	if err != nil && strings.Contains(err.Error(), "not eligible for reward") {
		t.Logf("claim: executed, refused by the module as expected for a new node: %v", err)
	} else {
		step("claim", r, err)
	}

	// A classical transaction from an account that now has a PQC key must be
	// refused: that is the chain's rule, and proves the hybrid path was needed.
	if _, err := s.SubmitClassical(ctx, 300000, MsgHeartbeat(op)); err == nil {
		t.Error("a classically signed heartbeat was accepted from an account with a PQC key")
	} else {
		t.Logf("classical heartbeat refused, as it must be: %v", err)
	}
}

func firstBondedValidator(t *testing.T, lcd string) string {
	t.Helper()
	resp, err := http.Get(strings.TrimRight(lcd, "/") + "/cosmos/staking/v1beta1/validators?status=BOND_STATUS_BONDED")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v struct {
		Validators []struct {
			OperatorAddress string `json:"operator_address"`
		} `json:"validators"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil || len(v.Validators) == 0 {
		t.Fatalf("no bonded validator: %v", err)
	}
	return v.Validators[0].OperatorAddress
}
