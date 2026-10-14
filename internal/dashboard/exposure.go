package dashboard

import (
	"fmt"
	"net"
	"strings"

	"github.com/qorechain/qorechain-lightnode/internal/netutil"
)

// isLoopback reports whether bindAddr keeps the listener on this machine.
//
// An empty host - ":8420" - is not loopback: Go binds every interface. That is
// the shape the default used to have, and the startup line still said
// "localhost", so an operator had no way to notice.
func isLoopback(bindAddr string) bool {
	host, _, err := net.SplitHostPort(bindAddr)
	if err != nil {
		// Unparseable: assume the worst rather than stay quiet.
		return false
	}
	if host == "" {
		return false
	}
	return netutil.IsLoopbackHost(host)
}

// DisplayURL renders the address the operator should actually open, instead of
// assuming localhost regardless of what the listener was bound to.
func DisplayURL(bindAddr string) string {
	host, port, err := net.SplitHostPort(bindAddr)
	if err != nil {
		return "http://" + bindAddr
	}
	switch {
	case host == "", host == "0.0.0.0", host == "::":
		return fmt.Sprintf("http://<this-host>:%s", port)
	case strings.Contains(host, ":"): // IPv6 literal
		return fmt.Sprintf("http://[%s]:%s", host, port)
	default:
		return fmt.Sprintf("http://%s:%s", host, port)
	}
}

// ExposureWarning returns a message when the dashboard is reachable from off
// the machine, and the empty string when it is not.
//
// The dashboard has no authentication. Every route is readable by anyone who
// can open the port: node status, the validators delegated to, accumulated
// rewards, the configured RPC endpoint and data directory. No private key is
// served - HandleSettings is deliberate about that - but on a shared VPS or an
// office network it is a map of the operator's position.
//
// Printed rather than refused: binding wider can be legitimate behind a reverse
// proxy that adds authentication. The operator should know they have done it.
func ExposureWarning(bindAddr string) string {
	return ExposureWarningWithToken(bindAddr, "")
}

// ExposureWarningWithToken is ExposureWarning, told whether a dashboard token
// is configured. An exposed dashboard WITH a token is a deliberate, defensible
// setup - it is how QoreX pairs with a node over a LAN - so it should not be
// shouted at in the same words as an exposed dashboard with nothing in front
// of it.
func ExposureWarningWithToken(bindAddr, token string) string {
	if isLoopback(bindAddr) {
		return ""
	}
	if token != "" {
		return fmt.Sprintf(`
  NOTE: the dashboard is bound to %s, reachable from outside this machine.
  A token is configured, so the API and WebSocket require it. Over plain http
  that token crosses the network in the clear: put it behind https or an SSH
  tunnel if the path is not one you trust.

`, bindAddr)
	}
	return fmt.Sprintf(`
  WARNING: the dashboard is bound to %s, which is reachable from outside
  this machine, and it has no authentication. Anyone who can open the port can
  read your node status, delegations, rewards and configuration.

  Set [dashboard] token in the config to require one, or bind to
  127.0.0.1:8420 unless it sits behind a reverse proxy that authenticates, or
  an SSH tunnel.

`, bindAddr)
}
