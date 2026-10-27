// Package update announces new light node releases and installs them.
//
// The node holds the operator's hot key, so a binary it installs on its own
// must be one QoreChain released, not merely one the download host served. The
// manifest (latest.json) is only a hint that a release exists. What the node
// trusts is SHA256SUMS of that release, signed with the release key whose
// public half is compiled in below; the binary must match its line there, and
// it must run and report the expected version before it replaces this one.
//
// Anyone who can write to the download host or its cache can serve any
// manifest and any binary. Without the release key they cannot make the node
// install it.
package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// releaseKeys are the Ed25519 public keys that may sign a release. A list, so a
// new key can be added one release before the old one is retired.
var releaseKeys = []string{
	releaseKey2026,
}

// extraReleaseKey lets a test build trust a throwaway key
// (-ldflags "-X .../internal/update.extraReleaseKey=<base64>"). Release builds
// leave it empty.
var extraReleaseKey string

// SignedContext is prepended to every signed message, so a release signature
// cannot be replayed as a signature over anything else, nor across networks.
const SignedContext = "qorechain-lightnode-release-v1"

// DefaultBase is where releases are published, per network.
const DefaultBase = "https://download.qore.host"

// maxBinary bounds a download; the binaries are ~14 MB.
const maxBinary = 96 << 20

// Network maps a chain id to the release channel that serves it. Unknown
// chains (devnets, local tests) get no updates.
func Network(chainID string) string {
	switch chainID {
	case "qorechain-vladi":
		return "mainnet"
	case "qorechain-diana":
		return "testnet"
	}
	return ""
}

// Manifest is the part of latest.json the updater reads.
type Manifest struct {
	Network       string `json:"network"`
	Version       string `json:"version"`
	MinCompatible string `json:"minCompatible"`
	Notes         string `json:"notes"`
}

// SignedMessage is what the release key signs for one release.
func SignedMessage(network, version string, sums []byte) []byte {
	var b bytes.Buffer
	b.WriteString(SignedContext + "\n" + network + "\n" + version + "\n")
	b.Write(sums)
	return b.Bytes()
}

// Verify checks a base64 signature over the release's SHA256SUMS.
func Verify(network, version string, sums, sig []byte) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errors.New("release signature is malformed")
	}
	msg := SignedMessage(network, version, sums)
	keys := releaseKeys
	if extraReleaseKey != "" {
		keys = append(append([]string(nil), keys...), extraReleaseKey)
	}
	for _, k := range keys {
		pub, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(pub, msg, raw) {
			return nil
		}
	}
	return errors.New("release signature does not verify against any trusted release key")
}

// Newer reports whether version a is greater than b (dotted numbers, an
// optional leading v). Anything unparseable is never newer.
func Newer(a, b string) bool {
	pa, oka := parse(a)
	pb, okb := parse(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func parse(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

// ArtifactName is this build's file name in a release.
func ArtifactName(edition string) string {
	name := fmt.Sprintf("lightnode-%s-%s-%s", edition, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// Updater checks one release channel for one binary.
type Updater struct {
	Base    string // DefaultBase unless testing
	Network string
	Edition string // "sx" or "ux"
	Current string
	HTTP    *http.Client
}

func (u *Updater) channel() string {
	return strings.TrimRight(u.Base, "/") + "/" + u.Network + "/lightnode"
}

// Check reads the manifest.
func (u *Updater) Check(ctx context.Context) (*Manifest, error) {
	body, err := u.fetch(ctx, u.channel()+"/latest.json", 1<<20)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if m.Network != u.Network {
		return nil, fmt.Errorf("manifest is for %q, this node follows %q", m.Network, u.Network)
	}
	if _, ok := parse(m.Version); !ok {
		return nil, fmt.Errorf("manifest version %q is not a version", m.Version)
	}
	return &m, nil
}

// Download fetches, authenticates and self-tests the binary of version and
// leaves it executable next to dst, returning its path. Nothing is replaced.
func (u *Updater) Download(ctx context.Context, version, dst string) (string, error) {
	dir := u.channel() + "/v" + strings.TrimPrefix(version, "v")
	sums, err := u.fetch(ctx, dir+"/SHA256SUMS", 1<<16)
	if err != nil {
		return "", err
	}
	sig, err := u.fetch(ctx, dir+"/SHA256SUMS.sig", 1<<10)
	if err != nil {
		return "", fmt.Errorf("release %s is not signed yet: %w", version, err)
	}
	if err := Verify(u.Network, strings.TrimPrefix(version, "v"), sums, sig); err != nil {
		return "", err
	}
	name := ArtifactName(u.Edition)
	want, err := sumFor(sums, name)
	if err != nil {
		return "", err
	}
	// The URL is built here, from the channel and the signed file name; the
	// manifest's own URLs are not followed.
	bin, err := u.fetch(ctx, dir+"/"+name, maxBinary)
	if err != nil {
		return "", err
	}
	got := sha256.Sum256(bin)
	if hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("%s does not match its signed checksum", name)
	}

	staged := dst + ".new"
	if err := os.WriteFile(staged, bin, 0o755); err != nil {
		return "", fmt.Errorf("writing %s: %w", staged, err)
	}
	if err := selfTest(ctx, staged, version); err != nil {
		os.Remove(staged)
		return "", err
	}
	return staged, nil
}

func sumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("the signed release has no %s", name)
}

// selfTest runs the staged binary's version command: a binary that does not
// start on this machine, or is not the version it was published as, never
// replaces the running one.
func selfTest(ctx context.Context, path, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("the new binary does not run here: %v: %s", err, bytes.TrimSpace(out))
	}
	// "lightnode-sx v3.1.12": the last word, exactly. A substring test would
	// take v3.1.12 for v3.1.1.
	f := strings.Fields(string(out))
	if len(f) == 0 || f[len(f)-1] != "v"+strings.TrimPrefix(version, "v") {
		return fmt.Errorf("the new binary reports %q, expected v%s", strings.TrimSpace(string(out)), version)
	}
	return nil
}

func (u *Updater) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "qorechain-lightnode/"+u.Current)
	hc := u.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("GET %s: larger than %d bytes", url, limit)
	}
	return body, nil
}

// Executable is the running binary, symlinks resolved.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// CanReplace says whether this process may replace its own binary, and if not,
// why. Containers are updated by pulling a new image; a binary on a read-only
// path, or one `go run` built, belongs to whoever put it there.
func CanReplace(exe string) (bool, string) {
	for _, f := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(f); err == nil {
			return false, "running in a container: pull the new image"
		}
	}
	if strings.Contains(exe, string(filepath.Separator)+"go-build") {
		return false, "a go run build"
	}
	probe, err := os.CreateTemp(filepath.Dir(exe), ".lightnode-update-probe-*")
	if err != nil {
		return false, "cannot write next to " + exe
	}
	probe.Close()
	os.Remove(probe.Name())
	return true, ""
}

// Swap puts the staged binary in place of exe and keeps the previous one as
// exe.old, so an operator can go back by hand.
func Swap(exe, staged string) error {
	old := exe + ".old"
	os.Remove(old)
	if runtime.GOOS == "windows" {
		// A running .exe cannot be overwritten, but it can be renamed.
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(staged, exe); err != nil {
			os.Rename(old, exe) //nolint:errcheck // best effort: put the old one back
			return err
		}
		return nil
	}
	if err := copyFile(exe, old); err != nil {
		return fmt.Errorf("keeping the previous binary: %w", err)
	}
	return os.Rename(staged, exe) // atomic on the same filesystem
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}
