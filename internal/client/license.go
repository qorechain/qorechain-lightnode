package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// FeatureLightNodeOperator is the licence feature the chain requires on the
// operator account before it accepts a light-node registration.
const FeatureLightNodeOperator = "lightnode_operator"

// LicenseStatus is the chain's answer about one (grantee, feature) pair.
type LicenseStatus struct {
	// Found is false when the chain has no grant at all for the pair.
	Found bool
	// Active is true when the grant exists and is not suspended.
	Active bool
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
			Active bool `json:"active"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return LicenseStatus{}, fmt.Errorf("decode licence check: %w", err)
		}
		return LicenseStatus{Found: true, Active: out.Active}, nil
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
		if out.LightNode.Address == "" {
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
