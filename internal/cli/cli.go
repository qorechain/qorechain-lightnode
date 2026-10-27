// Package cli holds the operator commands both editions ship: onboarding,
// keys, register, status and the chain read-outs.
//
// Until v3.1.7 these lived in cmd/lightnode-sx only. The UX binary had start
// and version, yet told a first-time user to run `lightnode-ux onboard`, a
// command it did not have. A desktop operator who downloaded only the UX build
// could watch the dashboard but could not create a key or register, so could
// never earn. Both binaries now carry the same commands, built from one place.
package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/qorechain/qorechain-lightnode/internal/config"
)

var (
	// CfgFile and HomeDir are bound to the root --config and --home flags.
	CfgFile string
	HomeDir string

	// Binary is the executable name used in the commands this package prints,
	// so a UX operator is told to run lightnode-ux, not lightnode-sx.
	Binary = "lightnode-sx"
	// NodeType is the edition, the default for `register --type`.
	NodeType = "sx"
	// Version is the release, set by each main.
	Version string
)

// DefaultHomeDir is where both editions keep config, keys and data.
func DefaultHomeDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".qorechain-lightnode")
}

// BindRootFlags adds --config and --home to root.
func BindRootFlags(root *cobra.Command) {
	defaultHome := DefaultHomeDir()
	root.PersistentFlags().StringVar(&CfgFile, "config", filepath.Join(defaultHome, "config.toml"), "path to config file")
	root.PersistentFlags().StringVar(&HomeDir, "home", defaultHome, "home directory for data and keys")
}

// LoadConfig reads the config file.
//
// A missing file means "not onboarded yet": run on defaults so the onboarding
// pointer and the key commands still work. Any other failure (unparseable
// TOML, an invalid operator_address) is an error the operator must see; it
// used to be swallowed, and the daemon then ran silently on defaults, on the
// wrong chain, with the wrong keyring backend.
func LoadConfig() (config.Config, error) {
	cfg, err := config.Load(CfgFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return cfg, err
		}
		cfg = config.DefaultConfig()
	}
	if HomeDir != "" {
		cfg.DataDir = HomeDir
	}
	// The version is the binary's. onboard writes the version of the build
	// that ran it into config.toml, so after an upgrade the file names the old
	// release, and /api/status reported it to QoreX as the node's version.
	if Version != "" {
		cfg.Version = Version
	}
	return cfg, nil
}

// Commands returns the operator commands shared by both editions.
func Commands() []*cobra.Command {
	return []*cobra.Command{
		statusCmd(),
		keysCmd(),
		registerCmd(),
		validatorsCmd(),
		delegationCmd(),
		rewardsCmd(),
		networkCmd(),
		selftestCmd(),
		onboardCmd(),
		updateCmd(),
	}
}
