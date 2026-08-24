package lightclient

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/qorechain/qorechain-lightnode/internal/client"
)

// Assurance describes how much a stored header is actually worth.
//
// It exists because the honest answer used to be "none" while the component was
// called a light client. Surfacing it means an operator can see which one they
// are running instead of inferring it from the name.
type Assurance string

const (
	// AssuranceTrusted means one endpoint was asked and believed. A malicious or
	// MITM'd RPC can fabricate everything the operator sees.
	AssuranceTrusted Assurance = "trusted-single-source"

	// AssuranceCorroborated means independent endpoints were asked for the same
	// height and returned the same block hash. This does not verify consensus
	// signatures: it raises the bar from "compromise one endpoint" to "compromise
	// every configured endpoint, consistently". It is not a light client.
	AssuranceCorroborated Assurance = "corroborated-across-sources"
)

// source is one endpoint the cross-check may consult.
type source struct {
	name string
	cli  *client.Client
}

// crossChecker asks several independent endpoints for the same height and only
// accepts a header they agree on.
//
// What this is NOT: verification of the commit signatures against the validator
// set, so it does not detect a chain where every configured endpoint is fed by
// the same compromised node, and it does not track validator set transitions or
// a trust period. Those need a real light client. See the package doc.
type crossChecker struct {
	primary   source
	witnesses []source
}

func newCrossChecker(primary *client.Client, witnessURLs []string) (*crossChecker, error) {
	cc := &crossChecker{primary: source{name: primary.RPCURL(), cli: primary}}

	seen := map[string]bool{canonicalHost(primary.RPCURL()): true}
	for _, raw := range witnessURLs {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			continue
		}
		if err := requireSafeScheme(addr); err != nil {
			return nil, err
		}
		// Two URLs pointing at the same host corroborate nothing: a compromised
		// endpoint would agree with itself. Reject rather than quietly counting
		// it, so a config that looks redundant is not.
		host := canonicalHost(addr)
		if seen[host] {
			return nil, fmt.Errorf(
				"witness %q resolves to the same host as another endpoint; witnesses must be independent", addr)
		}
		seen[host] = true
		cc.witnesses = append(cc.witnesses, source{name: addr, cli: client.New(addr, addr)})
	}
	return cc, nil
}

// requireSafeScheme rejects plaintext HTTP to anything but the local machine.
// A witness reached over http:// on the open internet can be rewritten in
// transit, which turns corroboration into theatre: the attacker answers as all
// of them.
func requireSafeScheme(addr string) error {
	u, err := url.Parse(addr)
	if err != nil {
		return fmt.Errorf("witness %q is not a valid URL: %w", addr, err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme != "http" {
		return fmt.Errorf("witness %q uses scheme %q; use https", addr, u.Scheme)
	}
	if isLocalHost(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("witness %q uses plaintext http to a remote host; use https", addr)
}

func isLocalHost(h string) bool {
	switch h {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	return strings.HasPrefix(h, "127.")
}

func canonicalHost(addr string) string {
	if u, err := url.Parse(addr); err == nil && u.Host != "" {
		return strings.ToLower(u.Host)
	}
	return strings.ToLower(addr)
}

// checkedHeader is a header and the assurance actually behind it.
type checkedHeader struct {
	Header    Header
	Assurance Assurance
	Sources   int
}

// disagreementError reports endpoints that returned different blocks for the
// same height. It is deliberately its own type: the caller must be able to tell
// "could not reach a witness" from "witnesses contradict each other", because
// only the second one means somebody is lying.
type disagreementError struct {
	Height int64
	ByHash map[string][]string
}

func (e *disagreementError) Error() string {
	hashes := make([]string, 0, len(e.ByHash))
	for h := range e.ByHash {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)

	var b strings.Builder
	fmt.Fprintf(&b, "endpoints disagree on block %d: ", e.Height)
	for i, h := range hashes {
		if i > 0 {
			b.WriteString("; ")
		}
		short := h
		if len(short) > 16 {
			short = short[:16] + "..."
		}
		fmt.Fprintf(&b, "%s from %s", short, strings.Join(e.ByHash[h], ", "))
	}
	return b.String()
}

// headerAt fetches one height from every configured endpoint and returns it only
// if they agree.
//
// A witness that cannot be reached is not a disagreement - networks fail - so it
// lowers the corroboration count rather than blocking the node. A witness that
// answers with a different block is a disagreement, and nothing is stored.
func (cc *crossChecker) headerAt(ctx context.Context, height int64) (checkedHeader, error) {
	type reply struct {
		name string
		resp *client.BlockAtResponse
		err  error
	}

	all := append([]source{cc.primary}, cc.witnesses...)
	replies := make([]reply, len(all))

	var wg sync.WaitGroup
	for i, s := range all {
		wg.Add(1)
		go func(i int, s source) {
			defer wg.Done()
			r, err := s.cli.BlockAt(ctx, height)
			replies[i] = reply{name: s.name, resp: r, err: err}
		}(i, s)
	}
	wg.Wait()

	if replies[0].err != nil {
		return checkedHeader{}, fmt.Errorf("primary %s: %w", replies[0].name, replies[0].err)
	}

	byHash := map[string][]string{}
	for _, r := range replies {
		if r.err != nil || r.resp == nil {
			continue
		}
		byHash[r.resp.Result.BlockID.Hash] = append(byHash[r.resp.Result.BlockID.Hash], r.name)
	}

	if len(byHash) > 1 {
		return checkedHeader{}, &disagreementError{Height: height, ByHash: byHash}
	}

	p := replies[0].resp.Result
	blockTime, _ := parseBlockTime(p.Block.Header.Time)

	agreeing := len(byHash[p.BlockID.Hash])
	assurance := AssuranceTrusted
	if agreeing > 1 {
		assurance = AssuranceCorroborated
	}

	return checkedHeader{
		Header: Header{
			Height:        height,
			Hash:          p.BlockID.Hash,
			Time:          blockTime,
			ValidatorHash: p.Block.Header.ValidatorsHash,
		},
		Assurance: assurance,
		Sources:   agreeing,
	}, nil
}
