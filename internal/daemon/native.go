package daemon

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
type nativeSubmitter struct {
	s   *chaintx.Signer
	gas uint64
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
	r, err := n.s.Submit(ctx, n.gas, msg)
	return txResult{TxHash: r.TxHash, Code: int(r.Code), RawLog: r.RawLog}, err
}

func gasFromConfig(cfg config.Config) uint64 {
	if g, err := strconv.ParseUint(cfg.Heartbeat.Gas, 10, 64); err == nil && g > 0 {
		return g
	}
	return 300000
}
