package daemon

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/qorechain/qorechain-lightnode/internal/chaintx"
	"github.com/qorechain/qorechain-lightnode/internal/config"
	"github.com/qorechain/qorechain-lightnode/internal/keyring"
)

// OperatorKeyName is the keyring entry holding the operator account's
// secp256k1 key, the classical half of every signature the node makes.
const OperatorKeyName = "operator"

// txSubmitter is what the heartbeat and claim loops sign through: the native
// signer when the keyring holds the operator key, qorechaind otherwise.
type txSubmitter interface {
	Submit(ctx context.Context, keyName, pqcKey string, txArgs ...string) (txResult, error)
}

// NativeSigner loads the operator key and the node's post-quantum key from the
// keyring. It returns nil and the reason when either is missing or the
// operator key is not the account in operator_address: signing for any other
// account would spend someone else's sequence, or nobody's.
func NativeSigner(cfg config.Config, keys keyring.Backend, lcd string) (*chaintx.Signer, string) {
	opInfo, err := keys.Get(OperatorKeyName)
	if err != nil {
		return nil, "no operator key in the keyring (add one with `keys add-operator`)"
	}
	if opInfo.Type != keyring.KeyTypeSecp256k1 {
		return nil, fmt.Sprintf("keyring entry %q is %s, not secp256k1", OperatorKeyName, opInfo.Type)
	}
	if cfg.OperatorAddress != "" && opInfo.Address != cfg.OperatorAddress {
		return nil, fmt.Sprintf("the operator key is %s but operator_address is %s", opInfo.Address, cfg.OperatorAddress)
	}
	opPriv, err := keys.Export(OperatorKeyName)
	if err != nil {
		return nil, fmt.Sprintf("reading the operator key: %v", err)
	}
	op, err := chaintx.OperatorKeyFromBytes(opPriv)
	if err != nil {
		return nil, fmt.Sprintf("operator key: %v", err)
	}
	pqcInfo, err := keys.Get(cfg.KeyName)
	if err != nil {
		return nil, fmt.Sprintf("node key %q: %v", cfg.KeyName, err)
	}
	pqcPriv, err := keys.Export(cfg.KeyName)
	if err != nil {
		return nil, fmt.Sprintf("reading the node key: %v", err)
	}
	return chaintx.NewSigner(lcd, cfg.ChainID, op, pqcInfo.PubKey, pqcPriv), ""
}

// nativeSubmitter adapts chaintx.Signer to the loops' qorechaind-shaped calls.
//
// The fee is charged on the gas limit, not on the gas used, so a limit far
// above what a heartbeat uses was paid for on every heartbeat: 300,000 declared
// for ~165,000-172,000 used, about 2 QOR a day on mainnet. The limit for each
// transaction kind is the gas its last run used plus a quarter (gasMargin),
// within [gasFloor, gas]; the first run, and any run after one that ran out of
// gas, uses gas, the configured ceiling.
type nativeSubmitter struct {
	s   *chaintx.Signer
	gas uint64

	mu       sync.Mutex
	lastUsed map[string]uint64
}

const (
	gasMargin = 1.25
	gasFloor  = 100000
	// codeOutOfGas is the SDK's ErrOutOfGas.
	codeOutOfGas = 11
)

// limitFor is the gas limit for the next transaction of this kind.
func (n *nativeSubmitter) limitFor(kind string) uint64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	used := n.lastUsed[kind]
	if used == 0 {
		return n.gas
	}
	limit := uint64(math.Ceil(float64(used)*gasMargin/1000)) * 1000
	if limit < gasFloor {
		limit = gasFloor
	}
	if limit > n.gas {
		limit = n.gas
	}
	return limit
}

func (n *nativeSubmitter) record(kind string, r chaintx.Result) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.lastUsed == nil {
		n.lastUsed = map[string]uint64{}
	}
	switch {
	case r.Code == codeOutOfGas:
		delete(n.lastUsed, kind) // back to the ceiling
	case r.Code == 0 && r.GasUsed > 0:
		n.lastUsed[kind] = r.GasUsed
	}
}

// FeePerTx is what the next transaction of this kind costs, in uqor.
func (n *nativeSubmitter) FeePerTx(kind string) uint64 {
	return uint64(math.Ceil(float64(n.limitFor(kind)) * n.s.GasPrice))
}

func (n *nativeSubmitter) Submit(ctx context.Context, _, _ string, txArgs ...string) (txResult, error) {
	op := n.s.Address()
	var msg chaintx.Any
	switch strings.Join(txArgs, " ") {
	case "lightnode heartbeat":
		msg = chaintx.MsgHeartbeat(op)
	case "lightnode claim-rewards":
		msg = chaintx.MsgClaimRewards(op)
	default:
		return txResult{}, fmt.Errorf("native signer: unsupported transaction %q", strings.Join(txArgs, " "))
	}
	kind := strings.Join(txArgs, " ")
	r, err := n.s.Submit(ctx, n.limitFor(kind), msg)
	n.record(kind, r)
	return txResult{TxHash: r.TxHash, Code: int(r.Code), RawLog: r.RawLog}, err
}

func gasFromConfig(cfg config.Config) uint64 {
	if g, err := strconv.ParseUint(cfg.Heartbeat.Gas, 10, 64); err == nil && g > 0 {
		return g
	}
	return 300000
}
