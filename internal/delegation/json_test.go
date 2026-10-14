package delegation

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/qorechain/qorechain-lightnode/internal/db"
)

// DelegationInfo had no JSON tags, so /api/delegation emitted Validator,
// Amount and UpdatedAt. Both of its readers want lower case: the node's own
// web dashboard (web/index.html reads d.validator and d.amount) and QoreX. So
// the delegation table has shown empty rows in every release until 3.1.6.
func TestDelegationInfoUsesTheKeysItsReadersUse(t *testing.T) {
	b, err := json.Marshal(DelegationInfo{Validator: "qorvaloper1x", Amount: "1000000", UpdatedAt: "2026-10-15"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"validator", "amount", "updated_at"} {
		if _, ok := got[k]; !ok {
			t.Errorf("key %q missing from %s", k, b)
		}
	}
}

// Amounts are stored as text, and ORDER BY on text ranks "9" above
// "47222000000". The largest delegation must come first.
func TestDelegationsAreOrderedByAmountNotByText(t *testing.T) {
	store, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, row := range [][2]string{{"qorvaloper1small", "9"}, {"qorvaloper1big", "47222000000"}, {"qorvaloper1mid", "300000"}} {
		if _, err := store.Conn().Exec(`INSERT INTO delegations (validator, amount, updated_at) VALUES (?, ?, ?)`, row[0], row[1], "t"); err != nil {
			t.Fatal(err)
		}
	}
	m := New(nil, store, slog.New(slog.DiscardHandler), "qor1op")
	got, err := m.GetDelegations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Validator != "qorvaloper1big" || got[2].Validator != "qorvaloper1small" {
		t.Fatalf("order = %+v, want big, mid, small", got)
	}
}
