package delegation

import (
	"context"
	"log/slog"
	"math/big"
	"time"

	"github.com/qorechain/qorechain-lightnode/internal/client"
)

// TxSubmitter submits one light-node message under the operator key and
// returns its transaction hash. The daemon's qorechaind signer implements it.
type TxSubmitter interface {
	SubmitLightNodeTx(ctx context.Context, txArgs ...string) (txHash string, err error)
}

// AutoClaimer periodically claims the light node's accrued rewards into the
// operator wallet once they pass a threshold.
//
// It claims; it does not re-delegate. Re-delegating from a server would need
// the hot key to be allowed to move stake, and a key that can move stake turns
// a compromised server into a loss of funds. Delegation changes go through the
// operator's wallet. The earlier version of this loop was called a compounder
// and signed nothing the chain could verify; see the 3.1.2 changelog.
type AutoClaimer struct {
	chain     *client.Client
	submit    TxSubmitter
	operator  string
	interval  time.Duration
	minReward *big.Int // uqor
	logger    *slog.Logger
}

// NewAutoClaimer creates the claim loop.
func NewAutoClaimer(chain *client.Client, submit TxSubmitter, operator string, interval time.Duration, minReward int64, logger *slog.Logger) *AutoClaimer {
	return &AutoClaimer{
		chain:     chain,
		submit:    submit,
		operator:  operator,
		interval:  interval,
		minReward: big.NewInt(minReward),
		logger:    logger,
	}
}

// Run drives the loop until ctx ends.
func (ac *AutoClaimer) Run(ctx context.Context) error {
	ticker := time.NewTicker(ac.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			ac.logger.Info("auto-claim stopped")
			return nil
		case <-ticker.C:
			ac.claim(ctx)
		}
	}
}

func (ac *AutoClaimer) claim(ctx context.Context) {
	registered, node, err := ac.chain.LightNodeRegistration(ctx, ac.operator)
	if err != nil {
		ac.logger.Warn("auto-claim: cannot read the node record", "error", err)
		return
	}
	if !registered {
		ac.logger.Debug("auto-claim: node not registered, nothing to claim")
		return
	}
	accrued, ok := new(big.Int).SetString(node.LightNode.AccumulatedRewards, 10)
	if !ok {
		ac.logger.Warn("auto-claim: unreadable accumulated_rewards", "value", node.LightNode.AccumulatedRewards)
		return
	}
	if accrued.Cmp(ac.minReward) < 0 {
		ac.logger.Debug("auto-claim: rewards below threshold", "accrued_uqor", accrued.String(), "min_uqor", ac.minReward.String())
		return
	}

	ac.logger.Info("auto-claim: claiming light node rewards", "accrued_uqor", accrued.String())
	hash, err := ac.submit.SubmitLightNodeTx(ctx, "lightnode", "claim-rewards")
	if err != nil {
		ac.logger.Warn("auto-claim: claim failed", "error", err)
		return
	}
	ac.logger.Info("auto-claim: rewards claimed", "accrued_uqor", accrued.String(), "tx_hash", hash)
}
