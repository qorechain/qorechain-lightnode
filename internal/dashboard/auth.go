package dashboard

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Bearer-token access control for the dashboard API.
//
// Why this exists: QoreX, the wallet that pairs with this node, has always had
// a token in its pairing record and sends it as `Authorization: Bearer <token>`
// on every call (LightNodePairing.token, packages/core/src/lightnode). Its
// cleartext rule is built around that token - it refuses plaintext http to a
// public host only when a token is configured, on the stated grounds that "the
// token is what the cleartext rule protects".
//
// This node never read the header. So an operator who set a token in QoreX,
// deliberately bound the dashboard beyond loopback to pair it, and believed the
// token restricted access, was serving every REST route and the WebSocket to
// anyone who could reach the port. The data is read-only and holds no key
// material, but node status, earnings and the delegation split were readable,
// and the control the other half of the product advertises did not exist here.
//
// Default is unchanged: with no token configured, nothing is required, so the
// overwhelmingly common loopback-only setup behaves exactly as before. The
// token only becomes mandatory once the operator sets one.
//
// Static assets are deliberately NOT behind the token. The UI is served to a
// browser that has no way to attach a bearer header to a page load; it is the
// data routes that matter, and the UI is useless without them.

// requireToken wraps h so it answers only to requests carrying the right
// bearer token. An empty token disables the check.
func requireToken(token string, h http.Handler) http.Handler {
	if token == "" {
		return h
	}
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !tokenMatches(want, r.Header.Get("Authorization")) {
			// A bare 401 with the scheme, and nothing about what was wrong:
			// "no token" and "wrong token" must look identical from outside.
			w.Header().Set("WWW-Authenticate", `Bearer realm="qorechain-lightnode"`)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// tokenMatches reports whether an Authorization header carries the wanted
// bearer token. The comparison is constant-time, so a wrong token cannot be
// recovered a byte at a time from response timing.
func tokenMatches(want []byte, header string) bool {
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	got := []byte(strings.TrimSpace(header[len(prefix):]))
	// ConstantTimeCompare returns 0 for unequal lengths, which already leaks
	// nothing beyond the length, and the length of a configured token is not
	// the secret.
	return subtle.ConstantTimeCompare(got, want) == 1
}
