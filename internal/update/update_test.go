package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testKey is trusted for the duration of a test, as a test build would via
// extraReleaseKey.
func testKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	prev := extraReleaseKey
	extraReleaseKey = base64.StdEncoding.EncodeToString(pub)
	t.Cleanup(func() { extraReleaseKey = prev })
	return priv
}

func sign(k ed25519.PrivateKey, network, version string, sums []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(k, SignedMessage(network, version, sums))) + "\n")
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"3.1.11", "3.1.10", true},
		{"3.1.10", "3.1.9", true}, // numeric, not text
		{"3.2.0", "3.1.99", true},
		{"v3.1.11", "3.1.11", false},
		{"3.1.10", "3.1.11", false},
		{"3.1.10.1", "3.1.10", true},
		{"", "3.1.10", false},
		{"3.1.x", "3.1.10", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSignatureBindsNetworkVersionAndSums(t *testing.T) {
	k := testKey(t)
	sums := []byte("abc  lightnode-sx-linux-amd64\n")
	sig := sign(k, "mainnet", "3.1.12", sums)
	if err := Verify("mainnet", "3.1.12", sums, sig); err != nil {
		t.Fatalf("good signature refused: %v", err)
	}
	for name, err := range map[string]error{
		"other network": Verify("testnet", "3.1.12", sums, sig),
		"other version": Verify("mainnet", "3.1.13", sums, sig),
		"other sums":    Verify("mainnet", "3.1.12", []byte("abd  lightnode-sx-linux-amd64\n"), sig),
		"garbage":       Verify("mainnet", "3.1.12", sums, []byte("not base64!")),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	_, stranger, _ := ed25519.GenerateKey(rand.Reader)
	if Verify("mainnet", "3.1.12", sums, sign(stranger, "mainnet", "3.1.12", sums)) == nil {
		t.Error("a key that is not a release key was accepted")
	}
}

func TestTheCompiledReleaseKeyIsWellFormed(t *testing.T) {
	for _, k := range releaseKeys {
		if b, err := base64.StdEncoding.DecodeString(k); err != nil || len(b) != ed25519.PublicKeySize {
			t.Fatalf("release key %q is not a base64 Ed25519 public key", k)
		}
	}
}

// release serves one release the way download.qore.host lays it out.
type release struct {
	network, version string
	bin              []byte
	sums, sig        []byte // nil sig = not signed yet
}

func (r *release) server(t *testing.T) *httptest.Server {
	t.Helper()
	dir := "/" + r.network + "/lightnode/v" + r.version + "/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/" + r.network + "/lightnode/latest.json":
			fmt.Fprintf(w, `{"network":%q,"version":%q,"minCompatible":"3.1.9","notes":"test notes"}`, r.network, r.version)
		case dir + "SHA256SUMS":
			w.Write(r.sums) //nolint:errcheck
		case dir + "SHA256SUMS.sig":
			if r.sig == nil {
				http.NotFound(w, req)
				return
			}
			w.Write(r.sig) //nolint:errcheck
		case dir + ArtifactName("sx"):
			w.Write(r.bin) //nolint:errcheck
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeBinary is a script that answers `version` like a release would.
func fakeBinary(reports string) []byte {
	return []byte("#!/bin/sh\necho 'lightnode-sx v" + reports + "'\n")
}

func newRelease(k ed25519.PrivateKey, version string, bin []byte) *release {
	sum := sha256.Sum256(bin)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + ArtifactName("sx") + "\n" +
		strings.Repeat("0", 64) + "  lightnode-ux-linux-amd64\n")
	return &release{network: "testnet", version: version, bin: bin, sums: sums, sig: sign(k, "testnet", version, sums)}
}

func installTarget(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "lightnode-sx")
	if err := os.WriteFile(exe, fakeBinary("3.1.11"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func unixOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
}

func TestDownloadInstallsASignedRelease(t *testing.T) {
	unixOnly(t)
	k := testKey(t)
	rel := newRelease(k, "3.1.12", fakeBinary("3.1.12"))
	u := &Updater{Base: rel.server(t).URL, Network: "testnet", Edition: "sx", Current: "3.1.11"}

	m, err := u.Check(context.Background())
	if err != nil || m.Version != "3.1.12" || !Newer(m.Version, u.Current) {
		t.Fatalf("check: %+v, %v", m, err)
	}
	exe := installTarget(t)
	staged, err := u.Download(context.Background(), m.Version, exe)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if err := Swap(exe, staged); err != nil {
		t.Fatalf("swap: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(rel.bin) {
		t.Fatal("the binary was not replaced")
	}
	if got, _ := os.ReadFile(exe + ".old"); string(got) != string(fakeBinary("3.1.11")) {
		t.Fatal("the previous binary was not kept as .old")
	}
}

func TestDownloadRefuses(t *testing.T) {
	unixOnly(t)
	k := testKey(t)
	_, stranger, _ := ed25519.GenerateKey(rand.Reader)
	cases := map[string]func(r *release){
		"unsigned release":         func(r *release) { r.sig = nil },
		"signed by an unknown key": func(r *release) { r.sig = sign(stranger, r.network, r.version, r.sums) },
		"signed for mainnet":       func(r *release) { r.sig = sign(k, "mainnet", r.version, r.sums) },
		"binary swapped after signing": func(r *release) {
			r.bin = fakeBinary("3.1.12") // same version, different bytes
			r.bin = append(r.bin, '#')
		},
		"binary reports another version": func(r *release) {
			*r = *newRelease(k, r.version, fakeBinary("3.1.10"))
		},
		"binary reports a longer version": func(r *release) {
			*r = *newRelease(k, r.version, fakeBinary("3.1.123"))
		},
		"binary does not run": func(r *release) {
			*r = *newRelease(k, r.version, []byte("\x7fELF not really"))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rel := newRelease(k, "3.1.12", fakeBinary("3.1.12"))
			mutate(rel)
			u := &Updater{Base: rel.server(t).URL, Network: "testnet", Edition: "sx", Current: "3.1.11"}
			exe := installTarget(t)
			if _, err := u.Download(context.Background(), "3.1.12", exe); err == nil {
				t.Fatal("installed")
			} else {
				t.Log(err)
			}
			if _, err := os.Stat(exe + ".new"); err == nil {
				t.Fatal("a refused binary was left staged")
			}
			if got, _ := os.ReadFile(exe); string(got) != string(fakeBinary("3.1.11")) {
				t.Fatal("the running binary was touched")
			}
		})
	}
}

func TestManifestForAnotherNetworkIsRefused(t *testing.T) {
	k := testKey(t)
	rel := newRelease(k, "3.1.12", fakeBinary("3.1.12"))
	rel.network = "mainnet"
	srv := rel.server(t)
	// A testnet node pointed at a channel whose manifest says mainnet.
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"network":"mainnet","version":"3.1.12"}`) //nolint:errcheck
	})
	u := &Updater{Base: srv.URL, Network: "testnet", Edition: "sx", Current: "3.1.11"}
	if _, err := u.Check(context.Background()); err == nil {
		t.Fatal("accepted another network's manifest")
	}
}

func TestNotifyModeAnnouncesAndInstallsNothing(t *testing.T) {
	k := testKey(t)
	rel := newRelease(k, "3.1.12", fakeBinary("3.1.12"))
	u := &Updater{Base: rel.server(t).URL, Network: "testnet", Edition: "sx", Current: "3.1.11"}
	firstCheck = time.Millisecond
	t.Cleanup(func() { firstCheck = 2 * time.Minute })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Loop(ctx, u, "notify", slog.New(slog.NewTextHandler(io.Discard, nil)), func(string) { t.Error("restart called in notify mode") })
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !CurrentStatus().UpdateAvailable && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	s := CurrentStatus()
	if !s.UpdateAvailable || s.Latest != "3.1.12" || s.Notes != "test notes" || s.Mandatory || s.CanInstall {
		t.Fatalf("status %+v", s)
	}
}
