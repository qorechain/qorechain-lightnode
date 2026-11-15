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

// StatusError is a non-200 answer other than 501. Its text is the one get
// always returned ("HTTP <code>: <body>"), so callers that print it are
// unchanged; callers that need the code can read it.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Code, e.Body) }

// Client connects to a QoreChain node via REST/RPC.
type Client struct {
	rpcURL  string
	lcdURL  string // REST API (typically :1317)
	httpCli *http.Client
}

// MaxResponseBytes bounds every response body the client reads. The largest
// real answer (a page of 200 validators) is well under 1 MiB; without a bound a
// malicious or intercepted endpoint could stream until the daemon ran out of
// memory. The signer uses the same bound.
const MaxResponseBytes = 4 << 20

// ErrResponseTooLarge is returned when a body exceeds MaxResponseBytes.
var ErrResponseTooLarge = errors.New("chain response larger than 4 MiB; refusing it")

// maxErrorBody is how much of an error body is kept in a StatusError.
const maxErrorBody = 512

// New creates a new chain client.
func New(rpcURL, lcdURL string) *Client {
	return &Client{
		rpcURL:  rpcURL,
		lcdURL:  lcdURL,
		httpCli: &http.Client{Timeout: 15 * time.Second, Transport: boundedTransport{http.DefaultTransport}},
	}
}

// boundedTransport caps every response body at MaxResponseBytes, so no reader
// in this package (io.ReadAll, json.Decoder) can be made to read without end.
type boundedTransport struct{ next http.RoundTripper }

func (t boundedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &boundedBody{rc: resp.Body, left: MaxResponseBytes}
	return resp, nil
}

type boundedBody struct {
	rc   io.ReadCloser
	left int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		// One more byte decides it: exactly MaxResponseBytes is allowed.
		var one [1]byte
		if n, _ := b.rc.Read(one[:]); n > 0 {
			return 0, ErrResponseTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.rc.Read(p)
	b.left -= int64(n)
	return n, err
}

func (b *boundedBody) Close() error { return b.rc.Close() }

// errorBody is the start of an error response, for messages.
func errorBody(body []byte) string {
	if len(body) > maxErrorBody {
		return string(body[:maxErrorBody]) + "..."
	}
	return string(body)
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
		return &StatusError{Code: resp.StatusCode, Body: errorBody(body)}
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
