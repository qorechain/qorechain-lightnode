package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// FeatureLightNodeOperator is the licence feature the chain requires on the
// operator account before it accepts a light-node registration.
const FeatureLightNodeOperator = "lightnode_operator"

// LicenseStatus is the chain's answer about one (grantee, feature) pair.
type LicenseStatus struct {
	// Found is false when the chain has no grant at all for the pair.
	Found bool
	// Active is true when the grant exists, is not suspended and - from chain
	// v3.2.3 - has not expired at the height the query was served at.
	Active bool
	// Suspended and ExpiresAt say WHY a found grant is inactive. Before
	// v3.2.3 the only cause was suspension, so an inactive licence was
	// reported as "suspended"; from v3.2.3 an expired grant also reads
	// active=false while it waits for the sweep, and the remedies differ - an
	// expired licence is renewed, a suspended one goes to support. ExpiresAt is
	// a block height; 0 means the grant never expires.
	Suspended bool
	ExpiresAt int64
}

// Expired reports whether an inactive grant is inactive because it ran out
// rather than because it was suspended.
func (s LicenseStatus) Expired() bool {
	return s.Found && !s.Active && !s.Suspended && s.ExpiresAt > 0
}

// LicenseCheck asks the chain whether grantee holds the feature licence.
//
// The chain answers "no license" as a not-found error rather than as an empty
// document, so this is not a plain get: a 404 is a real answer (no licence),
// not a failure, and is reported as Found=false with a nil error.
func (c *Client) LicenseCheck(ctx context.Context, grantee, feature string) (LicenseStatus, error) {
	url := fmt.Sprintf("%s/qorechain/license/v1/check/%s/%s", c.lcdURL, grantee, feature)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return LicenseStatus{}, err
	}
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return LicenseStatus{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		var out struct {
			License struct {
				Suspended bool `json:"suspended"`
				// int64 fields arrive as JSON strings from the REST gateway.
				ExpiresAt json.Number `json:"expires_at"`
			} `json:"license"`
			Active bool `json:"active"`
		}
		dec := json.NewDecoder(strings.NewReader(string(body)))
		dec.UseNumber()
		if err := dec.Decode(&out); err != nil {
			return LicenseStatus{}, fmt.Errorf("decode licence check: %w", err)
		}
		exp, _ := strconv.ParseInt(strings.Trim(string(out.License.ExpiresAt), `"`), 10, 64)
		return LicenseStatus{
			Found:     true,
			Active:    out.Active,
			Suspended: out.License.Suspended,
			ExpiresAt: exp,
		}, nil
	case http.StatusNotFound:
		if strings.Contains(string(body), "no license") {
			return LicenseStatus{}, nil
		}
		return LicenseStatus{}, fmt.Errorf("HTTP 404: %s", string(body))
	case http.StatusNotImplemented:
		return LicenseStatus{}, ErrEndpointUnavailable
	default:
		return LicenseStatus{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
}

// LightNodeRegistration reports whether operator has a registered light node,
// and returns the record when it does. An operator with no node is a normal
// answer (registered=false, nil error), not a failure.
func (c *Client) LightNodeRegistration(ctx context.Context, operator string) (registered bool, node *LightNodeQueryResponse, err error) {
	url := fmt.Sprintf("%s/qorechain/lightnode/v1/node/%s", c.lcdURL, operator)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, nil, err
	}
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return false, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		var out LightNodeQueryResponse
		if err := json.Unmarshal(body, &out); err != nil {
			return false, nil, fmt.Errorf("decode light node: %w", err)
		}
		// Both halves of the answer: the chain sets found=false for an
		// operator with no node, and an empty address means the same thing.
		// Reading only one of them is how the previous decoder reported every
		// registered node as unregistered.
		if !out.Found || out.LightNode.Address == "" {
			return false, nil, nil
		}
		return true, &out, nil
	case http.StatusNotFound:
		return false, nil, nil
	case http.StatusNotImplemented:
		return false, nil, ErrEndpointUnavailable
	default:
		return false, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
}

// DeriveLCDURL converts an RPC URL to the REST/LCD URL of the same node: the
// RPC port (26657) becomes the LCD port (1317), and a public "rpc." host
// becomes its "api." sibling (rpc.qore.host -> api.qore.host). Anything else
// is returned unchanged; set api_addr in the config for such endpoints.
func DeriveLCDURL(rpcAddr string) string {
	if strings.Contains(rpcAddr, "26657") {
		return strings.Replace(rpcAddr, "26657", "1317", 1)
	}
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(rpcAddr, scheme+"rpc.") {
			return scheme + "api." + strings.TrimPrefix(rpcAddr, scheme+"rpc.")
		}
	}
	return rpcAddr
}
