package client

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
	Moniker         string `json:"description,omitempty"` // nested, simplified
	Jailed          bool   `json:"jailed"`
	Status          string `json:"status"`
	Tokens          string `json:"tokens"`
	Commission      struct {
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

// LightNodeQueryResponse from custom lightnode module query.
type LightNodeQueryResponse struct {
	LightNode struct {
		Address            string `json:"address"`
		NodeType           string `json:"node_type"`
		Version            string `json:"version"`
		Status             string `json:"status"`
		LastHeartbeat      string `json:"last_heartbeat"`
		TotalHeartbeats    string `json:"total_heartbeats"`
		DelegatedStake     string `json:"delegated_stake"`
		AccumulatedRewards string `json:"accumulated_rewards"`
	} `json:"light_node"`
}

// LightNodeParamsResponse from lightnode module.
type LightNodeParamsResponse struct {
	Params struct {
		RegistrationFee     string `json:"registration_fee"`
		HeartbeatInterval   string `json:"heartbeat_interval"`
		MinDelegatedStake   string `json:"min_delegated_stake"`
		RewardShare         string `json:"reward_share"`
		MinUptimeForRewards string `json:"min_uptime_for_rewards"`
		MaxLightNodes       string `json:"max_light_nodes"`
	} `json:"params"`
}

// LightNodeStatsResponse from lightnode module.
type LightNodeStatsResponse struct {
	Stats struct {
		TotalRegistered  string `json:"total_registered"`
		TotalActive      string `json:"total_active"`
		TotalRewards     string `json:"total_rewards"`
		LastRewardHeight string `json:"last_reward_height"`
	} `json:"stats"`
}

// ReputationResponse from reputation module.
type ReputationResponse struct {
	Score struct {
		Composite float64 `json:"composite"`
		Stake     float64 `json:"stake"`
		Perf      float64 `json:"perf"`
		Contrib   float64 `json:"contrib"`
		Time      float64 `json:"time"`
	} `json:"score"`
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
