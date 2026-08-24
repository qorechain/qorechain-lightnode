package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrEndpointUnavailable is returned when a chain REST route is not exposed
// (HTTP 501 Not Implemented) — e.g. an optional module whose gRPC-gateway
// routes are not registered. Telemetry collectors treat this as "skip", not a
// hard error, so an operator without those optional REST routes sees a clean log.
var ErrEndpointUnavailable = errors.New("chain REST endpoint not available")

// Client connects to a QoreChain node via REST/RPC.
type Client struct {
	rpcURL  string
	lcdURL  string // REST API (typically :1317)
	httpCli *http.Client
}

// New creates a new chain client.
func New(rpcURL, lcdURL string) *Client {
	return &Client{
		rpcURL:  rpcURL,
		lcdURL:  lcdURL,
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
	if resp.StatusCode == http.StatusNotImplemented {
		return ErrEndpointUnavailable
	}
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

// BlockAt returns the block at a specific height over the consensus RPC, which
// carries the block id hash. The REST gateway used by LatestBlock does not.
func (c *Client) BlockAt(ctx context.Context, height int64) (*BlockAtResponse, error) {
	var resp BlockAtResponse
	url := fmt.Sprintf("%s/block?height=%d", c.rpcURL, height)
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, err
	}
	if resp.Result.BlockID.Hash == "" {
		return nil, fmt.Errorf("block %d: response carried no block id hash", height)
	}
	return &resp, nil
}

// RPCURL reports the endpoint this client talks to, for messages that have to
// name which source disagreed.
func (c *Client) RPCURL() string { return c.rpcURL }
