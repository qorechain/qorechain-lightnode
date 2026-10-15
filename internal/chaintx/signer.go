package chaintx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	qpqc "github.com/qorechain/qorechain-pqc/go"
)

// ProofOfPossessionUpgrades are the plan names under which a network gained
// MsgRegisterPQCKeyV2.proof_of_possession. A network that applied none of them
// refuses the field as unknown. Kept as a list for the reason the sign-bytes
// switch is one: the same release can land under different names.
var ProofOfPossessionUpgrades = []string{"v3.2.3"}

// Result is the block's answer for a transaction.
type Result struct {
	TxHash string
	Height int64
	Code   uint32
	RawLog string
}

// Signer builds, signs, broadcasts and confirms transactions for one operator
// account against a chain's REST gateway.
type Signer struct {
	LCD     string // REST gateway, e.g. https://api.qore.host
	ChainID string
	Key     OperatorKey
	PQCPub  []byte // registered on the account; empty for an account without one
	PQCPriv []byte

	// GasPrice is the fee per unit of gas, in Denom.
	GasPrice float64
	Denom    string

	HTTP          *http.Client
	InclusionWait time.Duration
	Poll          time.Duration

	// mu makes submissions one at a time: they share the account sequence,
	// which the chain only advances once a transaction is in a block.
	mu sync.Mutex
	// signBytesV2 is cached once seen; a network never switches back.
	signBytesV2 bool
}

// NewSigner returns a signer with production defaults.
func NewSigner(lcd, chainID string, key OperatorKey, pqcPub, pqcPriv []byte) *Signer {
	return &Signer{
		LCD:           strings.TrimRight(lcd, "/"),
		ChainID:       chainID,
		Key:           key,
		PQCPub:        pqcPub,
		PQCPriv:       pqcPriv,
		GasPrice:      0.25,
		Denom:         "uqor",
		HTTP:          &http.Client{Timeout: 20 * time.Second},
		InclusionWait: 60 * time.Second,
		Poll:          2 * time.Second,
	}
}

// Address is the operator account.
func (s *Signer) Address() string { return s.Key.Address() }

// Submit signs msgs with the hybrid scheme (secp256k1 + ML-DSA-87), the form
// every transaction from an account with a PQC key must take, broadcasts it and
// returns once it is in a block.
func (s *Signer) Submit(ctx context.Context, gas uint64, msgs ...Any) (Result, error) {
	if len(s.PQCPriv) == 0 {
		return Result{}, errors.New("no post-quantum key: the chain refuses a classically signed transaction from this account")
	}
	return s.submit(ctx, gas, true, msgs)
}

// SubmitClassical signs with the secp256k1 key alone. The chain accepts that
// only for a transaction whose single message registers the account's first
// PQC key.
func (s *Signer) SubmitClassical(ctx context.Context, gas uint64, msg Any) (Result, error) {
	return s.submit(ctx, gas, false, []Any{msg})
}

func (s *Signer) submit(ctx context.Context, gas uint64, hybrid bool, msgs []Any) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	accNum, seq, err := s.account(ctx)
	if err != nil {
		return Result{}, err
	}
	tx, err := s.build(ctx, accNum, seq, gas, hybrid, msgs)
	if err != nil {
		return Result{}, err
	}
	hash, err := s.broadcast(ctx, tx)
	if err != nil {
		return Result{TxHash: hash}, err
	}
	return s.awaitInclusion(ctx, hash)
}

// build assembles TxRaw. The order of operations is the contract.
func (s *Signer) build(ctx context.Context, accNum, seq, gas uint64, hybrid bool, msgs []Any) ([]byte, error) {
	fee := []Coin{{Denom: s.Denom, Amount: strconv.FormatUint(uint64(math.Ceil(float64(gas)*s.GasPrice)), 10)}}
	auth := authInfo(s.Key.pubKeyAny(), seq, fee, gas)

	body := txBody(msgs, nil)
	if hybrid {
		version, err := s.signBytesVersion(ctx)
		if err != nil {
			return nil, err
		}
		// The PQC signature covers the body WITHOUT the extension...
		sb, err := qpqc.HybridSignBytes(version, s.ChainID, body, auth)
		if err != nil {
			return nil, err
		}
		pqcSig, err := qpqc.MLDSA87.Sign(s.PQCPriv, sb)
		if err != nil {
			return nil, fmt.Errorf("ml-dsa-87 sign: %w", err)
		}
		// ...and the classical signature covers the body WITH it.
		body = txBody(msgs, []Any{pqcExtension(pqcSig)})
	}
	sig := s.Key.sign(signDoc(body, auth, s.ChainID, accNum))
	return txRaw(body, auth, sig), nil
}

// signBytesVersion asks the network which hybrid sign-bytes it verifies. Every
// plan name in qpqc.SignBytesV2Upgrades is asked: testnet took the switch as
// "v3.1.98" and mainnet as "v3.2.0", and asking one name misreads the other
// network. A failed query is an error, never a guess.
func (s *Signer) signBytesVersion(ctx context.Context) (int, error) {
	if s.signBytesV2 {
		return qpqc.SignBytesV2, nil
	}
	applied, err := s.appliedHeights(ctx, qpqc.SignBytesV2Upgrades)
	if err != nil {
		return 0, fmt.Errorf("reading the sign-bytes version: %w", err)
	}
	v := qpqc.SignBytesVersionForApplied(s.ChainID, applied)
	if v == qpqc.SignBytesV2 {
		s.signBytesV2 = true
	}
	return v, nil
}

// SupportsProofOfPossession reports whether the network knows
// MsgRegisterPQCKeyV2.proof_of_possession. Networks created after the
// legacy pair carry every feature from genesis.
func (s *Signer) SupportsProofOfPossession(ctx context.Context) (bool, error) {
	legacy := false
	for _, c := range qpqc.LegacySignBytesChains {
		if c == s.ChainID {
			legacy = true
		}
	}
	if !legacy {
		return true, nil
	}
	applied, err := s.appliedHeights(ctx, ProofOfPossessionUpgrades)
	if err != nil {
		return false, err
	}
	for _, h := range applied {
		if h > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (s *Signer) appliedHeights(ctx context.Context, names []string) (map[string]int64, error) {
	out := make(map[string]int64, len(names))
	for _, name := range names {
		var resp struct {
			Height string `json:"height"`
		}
		if err := s.getJSON(ctx, "/cosmos/upgrade/v1beta1/applied_plan/"+name, &resp); err != nil {
			return nil, err
		}
		h, err := strconv.ParseInt(resp.Height, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("applied_plan/%s: unreadable height %q", name, resp.Height)
		}
		out[name] = h
	}
	return out, nil
}

// ErrNoAccount means the chain has never seen the address: it holds no funds.
var ErrNoAccount = errors.New("the operator account does not exist on chain yet: send it some QOR first")

// account reads account_number and sequence. Vesting and other wrapped
// account types nest the base account; it is found wherever it sits.
func (s *Signer) account(ctx context.Context) (uint64, uint64, error) {
	var resp struct {
		Account json.RawMessage `json:"account"`
	}
	err := s.getJSON(ctx, "/cosmos/auth/v1beta1/accounts/"+s.Address(), &resp)
	if err != nil {
		var he *httpError
		if errors.As(err, &he) && he.status == http.StatusNotFound {
			return 0, 0, ErrNoAccount
		}
		return 0, 0, err
	}
	num, seq, ok := findBaseAccount(resp.Account)
	if !ok {
		return 0, 0, fmt.Errorf("unreadable account record: %s", truncate(string(resp.Account), 200))
	}
	return num, seq, nil
}

func findBaseAccount(raw json.RawMessage) (num, seq uint64, ok bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return 0, 0, false
	}
	if an, has := m["account_number"]; has {
		var a, q string
		_ = json.Unmarshal(an, &a)
		if sq, has := m["sequence"]; has {
			_ = json.Unmarshal(sq, &q)
		}
		n, err1 := strconv.ParseUint(a, 10, 64)
		sv := uint64(0)
		if q != "" {
			v, err := strconv.ParseUint(q, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			sv = v
		}
		return n, sv, err1 == nil
	}
	for _, key := range []string{"base_account", "base_vesting_account"} {
		if inner, has := m[key]; has {
			if n, q, ok := findBaseAccount(inner); ok {
				return n, q, true
			}
		}
	}
	return 0, 0, false
}

// broadcast sends the transaction and returns its hash once the mempool has
// accepted it.
func (s *Signer) broadcast(ctx context.Context, tx []byte) (string, error) {
	reqBody, _ := json.Marshal(map[string]string{
		"tx_bytes": base64.StdEncoding.EncodeToString(tx),
		"mode":     "BROADCAST_MODE_SYNC",
	})
	var resp struct {
		TxResponse struct {
			TxHash string `json:"txhash"`
			Code   uint32 `json:"code"`
			RawLog string `json:"raw_log"`
		} `json:"tx_response"`
	}
	if err := s.postJSON(ctx, "/cosmos/tx/v1beta1/txs", reqBody, &resp); err != nil {
		return "", fmt.Errorf("broadcast: %w", err)
	}
	r := resp.TxResponse
	if r.Code != 0 {
		return r.TxHash, fmt.Errorf("rejected by the chain: code %d: %s", r.Code, r.RawLog)
	}
	return r.TxHash, nil
}

// awaitInclusion waits for the transaction to be in a block. The mempool's
// code 0 is not the block's: a registration passed it on the devnet and
// failed in the block with code 13.
func (s *Signer) awaitInclusion(ctx context.Context, hash string) (Result, error) {
	deadline := time.Now().Add(s.InclusionWait)
	for {
		var resp struct {
			TxResponse struct {
				Height string `json:"height"`
				TxHash string `json:"txhash"`
				Code   uint32 `json:"code"`
				RawLog string `json:"raw_log"`
			} `json:"tx_response"`
		}
		err := s.getJSON(ctx, "/cosmos/tx/v1beta1/txs/"+hash, &resp)
		if err == nil && resp.TxResponse.TxHash != "" {
			r := resp.TxResponse
			h, _ := strconv.ParseInt(r.Height, 10, 64)
			res := Result{TxHash: r.TxHash, Height: h, Code: r.Code, RawLog: r.RawLog}
			if r.Code != 0 {
				return res, fmt.Errorf("failed in the block: code %d: %s", r.Code, r.RawLog)
			}
			return res, nil
		}
		if time.Now().After(deadline) {
			return Result{TxHash: hash}, fmt.Errorf("broadcast %s but not in a block after %s", hash, s.InclusionWait)
		}
		select {
		case <-ctx.Done():
			return Result{TxHash: hash}, ctx.Err()
		case <-time.After(s.Poll):
		}
	}
}

type httpError struct {
	status int
	body   string
}

func (e *httpError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.status, e.body) }

func (s *Signer) getJSON(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.LCD+path, nil)
	if err != nil {
		return err
	}
	return s.do(req, out)
}

func (s *Signer) postJSON(ctx context.Context, path string, body []byte, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.LCD+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.do(req, out)
}

func (s *Signer) do(req *http.Request, out interface{}) error {
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return &httpError{status: resp.StatusCode, body: truncate(string(b), 300)}
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Amount parses a uqor amount.
func Amount(s string) (*big.Int, bool) { return new(big.Int).SetString(s, 10) }

// RegisteredPQCKey returns the post-quantum public key registered on the
// operator account, and whether there is one.
func (s *Signer) RegisteredPQCKey(ctx context.Context) ([]byte, bool, error) {
	var resp struct {
		Found   bool `json:"found"`
		Account *struct {
			PublicKey string `json:"public_key"`
		} `json:"account"`
	}
	if err := s.getJSON(ctx, "/qorechain/pqc/v1/account/"+s.Address(), &resp); err != nil {
		return nil, false, err
	}
	if !resp.Found || resp.Account == nil {
		return nil, false, nil
	}
	pub, err := base64.StdEncoding.DecodeString(resp.Account.PublicKey)
	if err != nil {
		return nil, true, fmt.Errorf("unreadable registered key: %w", err)
	}
	return pub, true, nil
}

// AccountExists reports whether the chain knows the operator account.
func (s *Signer) AccountExists(ctx context.Context) (bool, error) {
	_, _, err := s.account(ctx)
	if errors.Is(err, ErrNoAccount) {
		return false, nil
	}
	return err == nil, err
}
