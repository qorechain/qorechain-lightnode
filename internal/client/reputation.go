package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// ErrNoScore means the chain serves reputation but has no record for this
// validator yet (HTTP 404: unknown, or never scored).
var ErrNoScore = errors.New("no reputation score for this validator yet")

// ValidatorReputation is one validator's record as x/reputation stores it.
// Scores are decimal strings (LegacyDec) between 0 and 1.
type ValidatorReputation struct {
	ConsensusAddress  string `json:"consensus_address"`
	OperatorAddress   string `json:"operator_address"`
	CompositeScore    string `json:"composite_score"`
	StakeScore        string `json:"stake_score"`
	PerformanceScore  string `json:"performance_score"`
	ContributionScore string `json:"contribution_score"`
	TimeScore         string `json:"time_score"`
}

// Composite is the composite score as a number.
func (r ValidatorReputation) Composite() (float64, error) {
	return strconv.ParseFloat(r.CompositeScore, 64)
}

// ReputationScore returns a validator's reputation record.
//
// The route accepts a qorvaloper or qorvalcons address. A chain that does not
// serve it answers 501 (ErrEndpointUnavailable); diana serves it from
// v3.2.6-b2, mainnet from the release after v3.2.5. 404 is ErrNoScore.
func (c *Client) ReputationScore(ctx context.Context, validator string) (*ValidatorReputation, error) {
	var resp struct {
		Reputation ValidatorReputation `json:"reputation"`
	}
	url := fmt.Sprintf("%s/qorechain/reputation/v1/validator/%s", c.lcdURL, validator)
	if err := c.get(ctx, url, &resp); err != nil {
		var se *StatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			return nil, ErrNoScore
		}
		return nil, err
	}
	return &resp.Reputation, nil
}
