package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client connects to a QoreChain node via REST/RPC.
type Client struct {
	rpcURL  string
	lcdURL  string // REST API (typically :1317)
	httpCli *http.Client
}

// New creates a new chain client.
func New(rpcURL, lcdURL string) *Client {
	return &Client{
		rpcURL: rpcURL,
		lcdURL: lcdURL,
		httpCli: &http.Client{Timeout: 15 * time.Second},
	}
}

// get performs a GET request and decodes JSON response.
func (c *Client) get(ctx context.Context, url string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// NodeStatus returns the node's status.
func (c *Client) NodeStatus(ctx context.Context) (*StatusResponse, error) {
	var resp StatusResponse
	if err := c.get(ctx, c.rpcURL+"/status", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LatestBlock returns the latest block info.
func (c *Client) LatestBlock(ctx context.Context) (*BlockResponse, error) {
	var resp BlockResponse
	if err := c.get(ctx, c.lcdURL+"/cosmos/base/tendermint/v1beta1/blocks/latest", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
