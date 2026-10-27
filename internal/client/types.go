package client

import "strconv"

// StatusResponse from /status RPC
type StatusResponse struct {
	Result struct {
		NodeInfo struct {
			Network string `json:"network"`
			Version string `json:"version"`
		} `json:"node_info"`
		SyncInfo struct {
			LatestBlockHeight string `json:"latest_block_height"`
			LatestBlockTime   string `json:"latest_block_time"`
			CatchingUp        bool   `json:"catching_up"`
		} `json:"sync_info"`
	} `json:"result"`
}

// BlockResponse from REST API
type BlockResponse struct {
	Block struct {
		Header struct {
			Height string `json:"height"`
			Time   string `json:"time"`
		} `json:"header"`
	} `json:"block"`
}

// ValidatorsResponse from REST API
type ValidatorsResponse struct {
	Validators []ValidatorInfo `json:"validators"`
	Pagination PaginationResp  `json:"pagination"`
}

// ValidatorInfo holds validator details.
type ValidatorInfo struct {
	OperatorAddress string `json:"operator_address"`
	// Description is a nested object in the staking API; Moniker lives inside it.
	Description struct {
		Moniker string `json:"moniker"`
	} `json:"description"`
	Jailed     bool   `json:"jailed"`
	Status     string `json:"status"`
	Tokens     string `json:"tokens"`
	Commission struct {
		Rate string `json:"rate"`
	} `json:"commission"`
}

// PaginationResp holds pagination metadata.
type PaginationResp struct {
	NextKey string `json:"next_key"`
	Total   string `json:"total"`
}

// DelegationResponse from REST API
type DelegationResponse struct {
	DelegationResponses []DelegationEntry `json:"delegation_responses"`
}

// DelegationEntry holds a single delegation.
type DelegationEntry struct {
	Delegation struct {
		DelegatorAddress string `json:"delegator_address"`
		ValidatorAddress string `json:"validator_address"`
		Shares           string `json:"shares"`
	} `json:"delegation"`
	Balance struct {
		Denom  string `json:"denom"`
		Amount string `json:"amount"`
	} `json:"balance"`
}

// RewardsResponse from REST API
type RewardsResponse struct {
	Rewards []struct {
		ValidatorAddress string `json:"validator_address"`
		Reward           []struct {
			Denom  string `json:"denom"`
			Amount string `json:"amount"`
		} `json:"reward"`
	} `json:"rewards"`
	Total []struct {
		Denom  string `json:"denom"`
		Amount string `json:"amount"`
	} `json:"total"`
}

// LightNodeView is one registered light node as the chain reports it. The
// field names follow qorechain/lightnode/v1 LightNodeView exactly; they were
// verified against live mainnet and diana on 2026-10-14, not assumed.
type LightNodeView struct {
	Address                  string   `json:"address"`
	NodeType                 string   `json:"node_type"`
	Version                  string   `json:"version"`
	Capabilities             []string `json:"capabilities"`
	Status                   string   `json:"status"`
	RegisteredAt             string   `json:"registered_at"`
	LastHeartbeat            string   `json:"last_heartbeat"`
	TotalHeartbeats          string   `json:"total_heartbeats"`
	ExpectedHeartbeats       string   `json:"expected_heartbeats"`
	DelegatedStake           string   `json:"delegated_stake"`
	AccumulatedRewards       string   `json:"accumulated_rewards"`
	InitialHeartbeatInterval string   `json:"initial_heartbeat_interval"`
}

// LightNodeQueryResponse is the answer to /qorechain/lightnode/v1/node/{addr}.
//
// The wire shape is {"node": ..., "found": bool} - NOT {"light_node": ...}.
// This struct said "light_node" until 2026-10-14, which no decoder ever
// complained about: encoding/json ignores members it has no field for, so a
// genuinely registered node unmarshalled into an all-zero record and
// LightNodeRegistration reported it as unregistered. The daemon's registration
// check and auto-claim both read that answer, so auto-claim could never see an
// accrual to claim. The Go field keeps its name so call sites read the same;
// only the tag was wrong.
type LightNodeQueryResponse struct {
	LightNode LightNodeView `json:"node"`
	Found     bool          `json:"found"`
}

// LightNodeParamsResponse is the answer to /qorechain/lightnode/v1/params.
//
// Flat, with no "params" envelope: QueryParamsResponse in the proto declares
// the seven fields at the top level. Verified live 2026-10-14.
type LightNodeParamsResponse struct {
	RegistrationFee      string `json:"registration_fee"`
	HeartbeatInterval    string `json:"heartbeat_interval"`
	MinDelegatedStake    string `json:"min_delegated_stake"`
	RewardShare          string `json:"reward_share"`
	MinUptimeForRewards  string `json:"min_uptime_for_rewards"`
	MaxLightNodes        string `json:"max_light_nodes"`
	HeartbeatGracePeriod string `json:"heartbeat_grace_period"`
}

// LightNodeStatsResponse is the answer to /qorechain/lightnode/v1/stats.
// Flat, same reasoning as the params response.
type LightNodeStatsResponse struct {
	TotalRegistered  string `json:"total_registered"`
	TotalActive      string `json:"total_active"`
	TotalRewards     string `json:"total_rewards"`
	LastRewardHeight string `json:"last_reward_height"`
}

// RewardPoolResponse is the answer to /qorechain/lightnode/v1/reward_pool,
// added to the chain by the v3.1.98 reward-pool upgrade.
//
// It is the only place an operator can see what is actually earnable. The
// module account balance is NOT the reward pool: Reserved is the part that
// accumulated while nobody was eligible and that only governance can move, and
// Outstanding is already credited to operators. Distributable is what the next
// settlement may hand out, and it is swept to Reserved when unearned, so it
// reads zero whenever no eligible node is running.
//
// MaxRewardPerBlockPerNode is the per-node ceiling, in uqor per block. It is a
// per-BLOCK figure, so what it is worth per month depends on the live block
// cadence - see RewardCeilingPerMonth.
type RewardPoolResponse struct {
	Balance                  string `json:"balance"`
	Reserved                 string `json:"reserved"`
	Payable                  string `json:"payable"`
	Outstanding              string `json:"outstanding"`
	Distributable            string `json:"distributable"`
	MaxRewardPerBlockPerNode string `json:"max_reward_per_block_per_node"`
	// MaxRewardPerMonthPerNode is the monthly bound the chain states (chain
	// v3.2.3+). It is NOT necessarily the bound in force: the chain fills it
	// from a defaulting accessor even while the lightnode-monthly-cap switch is
	// off, and in that state - and for the first settlement after the switch -
	// the per-block bound still applies. The response carries no signal for
	// which one is in force, so a client must not present this figure as
	// authoritative on its own. See Ceilings.
	MaxRewardPerMonthPerNode string `json:"max_reward_per_month_per_node"`
	Initialized              bool   `json:"initialized"`
}

// Ceilings returns both per-node bounds the chain may be applying, each in uqor
// per 30 days.
//
// statedMonthly is the chain's own monthly bound (hasStated is false before
// v3.2.3, which does not send it). It applies once the lightnode-monthly-cap
// switch is on and a first settlement has stamped its time.
//
// perBlockMonthly is the per-block bound converted at secondsPerBlock. It is
// the bound in force before that, and the only one on older chains. Its value
// moves with block time: 1713 uqor/block was 2,000 QOR/month at 2.22 s/block
// and ~1,381 at the 3.2151 s/block measured on mainnet on 2026-10-14.
//
// Neither is returned as "the" bound, because the chain's response does not
// say which is in force. Callers show both, each with its condition.
func (r *RewardPoolResponse) Ceilings(secondsPerBlock float64) (statedMonthly float64, hasStated bool, perBlockMonthly float64, hasPerBlock bool) {
	if r.MaxRewardPerMonthPerNode != "" {
		if v, err := strconv.ParseFloat(r.MaxRewardPerMonthPerNode, 64); err == nil {
			statedMonthly, hasStated = v, true
		}
	}
	if secondsPerBlock > 0 {
		if pb, err := strconv.ParseFloat(r.MaxRewardPerBlockPerNode, 64); err == nil {
			perBlockMonthly, hasPerBlock = pb*2592000.0/secondsPerBlock, true
		}
	}
	return
}

// BurnStatsResponse from burn module.
type BurnStatsResponse struct {
	Stats struct {
		TotalBurned string `json:"total_burned"`
	} `json:"stats"`
}

// BridgeStatusResponse from bridge module.
type BridgeStatusResponse struct {
	Connections []struct {
		ChainName        string `json:"chain_name"`
		ChainType        string `json:"chain_type"`
		Status           string `json:"status"`
		PendingTransfers int    `json:"pending_transfers"`
	} `json:"connections"`
}

// TxBroadcastResponse from tx broadcast.
type TxBroadcastResponse struct {
	TxResponse struct {
		Code   int    `json:"code"`
		TxHash string `json:"txhash"`
		RawLog string `json:"raw_log"`
	} `json:"tx_response"`
}

// BlockAtResponse is the consensus RPC /block reply. Unlike BlockResponse, which
// comes from the REST gateway and carries only height and time, this one carries
// the block id hash and the validator set hash - the two values a cross-check
// has to compare, and the two the header table was storing empty.
type BlockAtResponse struct {
	Result struct {
		BlockID struct {
			Hash string `json:"hash"`
		} `json:"block_id"`
		Block struct {
			Header struct {
				Height         string `json:"height"`
				Time           string `json:"time"`
				ValidatorsHash string `json:"validators_hash"`
				ChainID        string `json:"chain_id"`
			} `json:"header"`
		} `json:"block"`
	} `json:"result"`
}
