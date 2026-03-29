package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// TxBody represents the body of a transaction.
type TxBody struct {
	Messages []json.RawMessage `json:"messages"`
	Memo     string            `json:"memo,omitempty"`
}

// AuthInfo contains fee and signer info.
type AuthInfo struct {
	Fee        Fee          `json:"fee"`
	SignerInfo []SignerInfo `json:"signer_infos"`
}

// Fee defines the transaction fee.
type Fee struct {
	Amount   []Coin `json:"amount"`
	GasLimit string `json:"gas_limit"`
}

// Coin is a denom+amount pair.
type Coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

// SignerInfo contains the signer's public key and sequence.
type SignerInfo struct {
	PublicKey json.RawMessage `json:"public_key"`
	ModeInfo  json.RawMessage `json:"mode_info"`
	Sequence  string          `json:"sequence"`
}

// BroadcastTxRequest is the request body for /cosmos/tx/v1beta1/txs.
type BroadcastTxRequest struct {
	TxBytes string `json:"tx_bytes"` // base64-encoded signed tx
	Mode    string `json:"mode"`     // BROADCAST_MODE_SYNC, BROADCAST_MODE_ASYNC
}

// BroadcastTxResponse is the response from the broadcast endpoint.
type BroadcastTxResponse struct {
	TxResponse struct {
		Code      int    `json:"code"`
		TxHash    string `json:"txhash"`
		RawLog    string `json:"raw_log"`
		Height    string `json:"height"`
		GasUsed   string `json:"gas_used"`
		GasWanted string `json:"gas_wanted"`
	} `json:"tx_response"`
}

// AccountInfo returns the account number and sequence for signing.
type AccountInfo struct {
	AccountNumber string `json:"account_number"`
	Sequence      string `json:"sequence"`
}

type accountResponse struct {
	Account struct {
		AccountNumber string `json:"account_number"`
		Sequence      string `json:"sequence"`
	} `json:"account"`
}

// GetAccount fetches the account number and sequence for an address.
func (c *Client) GetAccount(ctx context.Context, address string) (*AccountInfo, error) {
	var resp accountResponse
	url := fmt.Sprintf("%s/cosmos/auth/v1beta1/accounts/%s", c.lcdURL, address)
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, fmt.Errorf("get account %s: %w", address, err)
	}
	return &AccountInfo{
		AccountNumber: resp.Account.AccountNumber,
		Sequence:      resp.Account.Sequence,
	}, nil
}

// BroadcastTx submits a signed transaction to the chain.
func (c *Client) BroadcastTx(ctx context.Context, txBytes []byte) (*BroadcastTxResponse, error) {
	reqBody := BroadcastTxRequest{
		TxBytes: base64.StdEncoding.EncodeToString(txBytes),
		Mode:    "BROADCAST_MODE_SYNC",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal broadcast request: %w", err)
	}

	url := c.lcdURL + "/cosmos/tx/v1beta1/txs"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("broadcast tx: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("broadcast tx: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var txResp BroadcastTxResponse
	if err := json.NewDecoder(resp.Body).Decode(&txResp); err != nil {
		return nil, fmt.Errorf("decode broadcast response: %w", err)
	}

	return &txResp, nil
}

// SimulateTx simulates a transaction to estimate gas.
func (c *Client) SimulateTx(ctx context.Context, txBytes []byte) (uint64, error) {
	type simReq struct {
		TxBytes string `json:"tx_bytes"`
	}
	type simResp struct {
		GasInfo struct {
			GasUsed string `json:"gas_used"`
		} `json:"gas_info"`
	}

	reqBody, _ := json.Marshal(simReq{
		TxBytes: base64.StdEncoding.EncodeToString(txBytes),
	})

	url := c.lcdURL + "/cosmos/tx/v1beta1/simulate"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return 0, fmt.Errorf("simulate tx: %w", err)
	}
	defer resp.Body.Close()

	var result simResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	gasUsed, _ := strconv.ParseUint(result.GasInfo.GasUsed, 10, 64)
	return gasUsed, nil
}
