package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/qorechain/qorechain-lightnode/internal/keyring"
)

// TxBuilder constructs, signs, and broadcasts transactions.
type TxBuilder struct {
	client  *Client
	keys    keyring.Backend
	keyName string
	chainID string
}

// NewTxBuilder creates a transaction builder.
func NewTxBuilder(client *Client, keys keyring.Backend, keyName, chainID string) *TxBuilder {
	return &TxBuilder{
		client:  client,
		keys:    keys,
		keyName: keyName,
		chainID: chainID,
	}
}

// MsgHeartbeat is the light node heartbeat message.
type MsgHeartbeat struct {
	Type     string `json:"@type"`
	Operator string `json:"operator"`
}

// MsgClaimLightNodeRewards is the claim rewards message.
type MsgClaimLightNodeRewards struct {
	Type     string `json:"@type"`
	Operator string `json:"operator"`
}

// BuildAndBroadcast builds, signs, and broadcasts a transaction with the given messages.
func (tb *TxBuilder) BuildAndBroadcast(ctx context.Context, msgs ...interface{}) (*BroadcastTxResponse, error) {
	keyInfo, err := tb.keys.Get(tb.keyName)
	if err != nil {
		return nil, fmt.Errorf("get key %s: %w", tb.keyName, err)
	}

	// Get account info for signing
	acctInfo, err := tb.client.GetAccount(ctx, keyInfo.Address)
	if err != nil {
		return nil, fmt.Errorf("get account info: %w", err)
	}

	// Marshal messages
	var rawMsgs []json.RawMessage
	for _, msg := range msgs {
		bz, err := json.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("marshal msg: %w", err)
		}
		rawMsgs = append(rawMsgs, bz)
	}

	// Build the sign doc (simplified Amino-JSON signing for REST broadcast)
	signDoc := map[string]interface{}{
		"chain_id":       tb.chainID,
		"account_number": acctInfo.AccountNumber,
		"sequence":       acctInfo.Sequence,
		"fee": map[string]interface{}{
			"amount": []map[string]string{
				{"denom": "uqor", "amount": "5000"},
			},
			"gas": "200000",
		},
		"msgs": rawMsgs,
		"memo": "qorechain-lightnode",
	}

	signBytes, err := json.Marshal(signDoc)
	if err != nil {
		return nil, fmt.Errorf("marshal sign doc: %w", err)
	}

	// Hash and sign
	hash := sha256.Sum256(signBytes)
	sig, err := tb.keys.Sign(tb.keyName, hash[:])
	if err != nil {
		return nil, fmt.Errorf("sign tx: %w", err)
	}

	// Build the broadcast-ready tx envelope
	txEnvelope := map[string]interface{}{
		"body": map[string]interface{}{
			"messages": rawMsgs,
			"memo":     "qorechain-lightnode",
		},
		"auth_info": map[string]interface{}{
			"signer_infos": []map[string]interface{}{
				{
					"public_key": map[string]interface{}{
						"@type": "/cosmos.crypto.secp256k1.PubKey",
						"key":   keyInfo.PubKey,
					},
					"mode_info": map[string]interface{}{
						"single": map[string]string{
							"mode": "SIGN_MODE_LEGACY_AMINO_JSON",
						},
					},
					"sequence": acctInfo.Sequence,
				},
			},
			"fee": map[string]interface{}{
				"amount": []map[string]string{
					{"denom": "uqor", "amount": "5000"},
				},
				"gas_limit": "200000",
			},
		},
		"signatures": [][]byte{sig},
	}

	txBytes, err := json.Marshal(txEnvelope)
	if err != nil {
		return nil, fmt.Errorf("marshal tx: %w", err)
	}

	return tb.client.BroadcastTx(ctx, txBytes)
}
