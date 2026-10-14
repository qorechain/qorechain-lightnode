// Package netutil holds the small network checks more than one package needs,
// so they cannot drift apart.
package netutil

import (
	"net"
	"strings"
)

// IsLoopbackHost reports whether host names this machine's loopback interface.
//
// host is a bare hostname or IP literal, as url.URL.Hostname and
// net.SplitHostPort return it (no port, no brackets). Only "localhost" and IP
// literals the standard library classifies as loopback qualify. A DNS name that
// merely starts with "127." - 127.evil.com, 127.0.0.1.nip.io - is an ordinary
// remote name that resolves wherever its owner points it, so it is not.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
