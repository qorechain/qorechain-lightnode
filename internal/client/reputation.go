package client

import (
	"context"
	"fmt"
	"strconv"
)

// ValidatorReputation is one validator's record as x/reputation stores it.
// Scores are decimal strings (LegacyDec) between 0 and 1.
type ValidatorReputation struct {
	Address           string `json:"address"`
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
// Until the chain serves this route it answers 501 (ErrEndpointUnavailable):
// x/reputation keeps scores but published only its params, and those over
// gRPC alone.
func (c *Client) ReputationScore(ctx context.Context, validator string) (*ValidatorReputation, error) {
	var resp struct {
		Reputation ValidatorReputation `json:"reputation"`
	}
	url := fmt.Sprintf("%s/qorechain/reputation/v1/validator/%s", c.lcdURL, validator)
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, err
	}
	return &resp.Reputation, nil
}
