package dashboard

import (
	"fmt"
	"net"
	"strings"
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
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
	if isLoopback(bindAddr) {
		return ""
	}
	return fmt.Sprintf(`
  WARNING: the dashboard is bound to %s, which is reachable from outside
  this machine, and it has no authentication. Anyone who can open the port can
  read your node status, delegations, rewards and configuration.

  Bind it to 127.0.0.1:8420 unless it sits behind a reverse proxy that
  authenticates, or an SSH tunnel.

`, bindAddr)
}
