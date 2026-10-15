// Package walletreq builds and queues signing requests for QoreX.
//
// The light node does not sign staking transactions itself by default. It holds
// a hot key because heartbeats are automatic and frequent, and that key must be
// able to do exactly one thing: heartbeat. A key that can also undelegate turns
// a compromised server into a loss of funds 21 days later.
//
// So the node builds a request describing what it wants done and leaves it for
// QoreX to pick up. The wallet re-validates every field, rebuilds the
// transaction locally, reads the account sequence at signing time, and asks the
// operator to approve. Nothing signed here crosses the wire, and the wallet
// never signs an object it did not build.
//
// The shape of these requests is fixed by @qorex/core (parseDashboardRequest).
// It is a shared contract with the dashboard, not something this package may
// change unilaterally.
package walletreq

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Kind enumerates the request types the wallet understands. A wallet that meets
// a kind it does not know refuses the request rather than guessing at it, so
// adding a kind here without shipping wallet support fails closed.
type Kind string

const (
	KindDelegate     Kind = "delegate"
	KindUndelegate   Kind = "undelegate"
	KindRedelegate   Kind = "redelegate"
	KindClaimRewards Kind = "claim-rewards"
)

// Request is one action awaiting the operator's approval in QoreX.
//
// Amounts are strings holding an integer count of uqor. They are deliberately
// not numbers: a JSON number is a float64 on the far side, and a balance in
// uqor exceeds the range where that stays exact.
//
// There is deliberately no fee or gas field. The wallet supplies those from its
// own measured constants, so the two cannot drift apart.
//
// There is deliberately no recipient field either. See validate.
type Request struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`

	FromQorAddress string `json:"fromQorAddress"`

	// delegate, undelegate
	ValidatorAddress string `json:"validatorAddress,omitempty"`

	// redelegate
	SrcValidatorAddress string `json:"srcValidatorAddress,omitempty"`
	DstValidatorAddress string `json:"dstValidatorAddress,omitempty"`

	// claim-rewards
	ValidatorAddresses []string `json:"validatorAddresses,omitempty"`

	// delegate, undelegate, redelegate
	AmountUqor string `json:"amountUqor,omitempty"`

	Memo string `json:"memo,omitempty"`
}

var (
	ErrUnknownKind   = errors.New("unknown request kind")
	ErrSameValidator = errors.New("redelegate source and destination must differ")
)

const (
	accountPrefix   = "qor1"
	validatorPrefix = "qorvaloper1"
)

// Validate reports whether the request is well formed and safe to hand over.
//
// The node validates on the way out even though the wallet validates on the way
// in. The two checks guard different failures: the wallet protects itself from
// a bad sender, this one stops the node from producing a request it did not
// mean to.
func (r Request) Validate() error {
	if !strings.HasPrefix(r.FromQorAddress, accountPrefix) {
		return fmt.Errorf("fromQorAddress %q is not a qor1 address", r.FromQorAddress)
	}

	switch r.Kind {
	case KindDelegate, KindUndelegate:
		if err := checkValidator("validatorAddress", r.ValidatorAddress); err != nil {
			return err
		}
		if err := checkAmount(r.AmountUqor); err != nil {
			return err
		}

	case KindRedelegate:
		if err := checkValidator("srcValidatorAddress", r.SrcValidatorAddress); err != nil {
			return err
		}
		if err := checkValidator("dstValidatorAddress", r.DstValidatorAddress); err != nil {
			return err
		}
		// The chain rejects a redelegation to the same validator, but only after
		// the transaction is in a block, so the operator pays the fee for
		// nothing. Catch it here instead.
		if r.SrcValidatorAddress == r.DstValidatorAddress {
			return ErrSameValidator
		}
		if err := checkAmount(r.AmountUqor); err != nil {
			return err
		}

	case KindClaimRewards:
		if len(r.ValidatorAddresses) == 0 {
			return errors.New("claim-rewards needs at least one validator")
		}
		seen := make(map[string]bool, len(r.ValidatorAddresses))
		for i, v := range r.ValidatorAddresses {
			if err := checkValidator(fmt.Sprintf("validatorAddresses[%d]", i), v); err != nil {
				return err
			}
			if seen[v] {
				return fmt.Errorf("validatorAddresses contains %s twice", v)
			}
			seen[v] = true
		}

	default:
		return fmt.Errorf("%w: %q", ErrUnknownKind, r.Kind)
	}

	return nil
}

func checkValidator(field, addr string) error {
	if addr == "" {
		return fmt.Errorf("%s is required for this kind", field)
	}
	if !strings.HasPrefix(addr, validatorPrefix) {
		return fmt.Errorf("%s %q is not a qorvaloper address", field, addr)
	}
	return nil
}

// checkAmount insists on a positive integer count of uqor. Anything with a
// decimal point, a sign, or an exponent is a caller that meant QOR and forgot
// to convert.
func checkAmount(amount string) error {
	if amount == "" {
		return errors.New("amountUqor is required for this kind")
	}
	n, ok := new(big.Int).SetString(amount, 10)
	if !ok {
		return fmt.Errorf("amountUqor %q is not an integer in uqor", amount)
	}
	if n.Sign() <= 0 {
		return fmt.Errorf("amountUqor %q must be positive", amount)
	}
	return nil
}
