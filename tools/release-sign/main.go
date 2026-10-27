// Command release-sign makes and uses the light node release key.
//
//	release-sign keygen <seed-file>          new key: 32-byte seed (base64) to <seed-file>, mode 600; prints the public key
//	release-sign sign <network> <version> <SHA256SUMS> > SHA256SUMS.sig
//	release-sign verify <network> <version> <SHA256SUMS> <SHA256SUMS.sig>
//
// sign reads the seed from QORE_RELEASE_SIGNING_KEY (base64). It is not part of
// any release build.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/qorechain/qorechain-lightnode/internal/update"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "release-sign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: keygen <seed-file> | sign <network> <version> <SHA256SUMS> | verify <network> <version> <SHA256SUMS> <sig>")
	}
	switch args[0] {
	case "keygen":
		if len(args) != 2 {
			return fmt.Errorf("keygen <seed-file>")
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(args[1], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.WriteString(base64.StdEncoding.EncodeToString(priv.Seed()) + "\n"); err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(pub))
		return nil
	case "sign":
		if len(args) != 4 {
			return fmt.Errorf("sign <network> <version> <SHA256SUMS>")
		}
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("QORE_RELEASE_SIGNING_KEY")))
		if err != nil || len(seed) != ed25519.SeedSize {
			return fmt.Errorf("QORE_RELEASE_SIGNING_KEY must hold the base64 32-byte seed")
		}
		sums, err := os.ReadFile(args[3])
		if err != nil {
			return err
		}
		sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), update.SignedMessage(args[1], args[2], sums))
		fmt.Println(base64.StdEncoding.EncodeToString(sig))
		return nil
	case "verify":
		if len(args) != 5 {
			return fmt.Errorf("verify <network> <version> <SHA256SUMS> <sig>")
		}
		sums, err := os.ReadFile(args[3])
		if err != nil {
			return err
		}
		sig, err := os.ReadFile(args[4])
		if err != nil {
			return err
		}
		if err := update.Verify(args[1], args[2], sums, sig); err != nil {
			return err
		}
		fmt.Println("OK: signed by a trusted release key")
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}
