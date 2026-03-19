package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config defines the light node configuration.
type Config struct {
	// Node identity
	NodeType string `toml:"node_type"` // "sx" or "ux"
	Version  string `toml:"version"`

	// Chain connection
	ChainID  string `toml:"chain_id"`
	RPCAddr  string `toml:"rpc_addr"`
	GRPCAddr string `toml:"grpc_addr"`

	// Light client
	TrustPeriod   string   `toml:"trust_period"` // e.g. "168h" (7 days)
	TrustHeight   int64    `toml:"trust_height"`
	TrustHash     string   `toml:"trust_hash"`
	MaxClockDrift string   `toml:"max_clock_drift"` // e.g. "10s"
	PrimaryAddr   string   `toml:"primary_addr"`    // primary RPC for light client
	WitnessAddrs  []string `toml:"witness_addrs"`   // witness RPCs

	// Storage
	DataDir string `toml:"data_dir"` // default ~/.qorechain-lightnode

	// Keyring
	KeyringBackend string `toml:"keyring_backend"` // "file" or "os"
	KeyName        string `toml:"key_name"`

	// Staking & Delegation
	Delegation DelegationConfig `toml:"delegation"`

	// Telemetry
	Telemetry TelemetryConfig `toml:"telemetry"`

	// Dashboard (UX only)
	Dashboard DashboardConfig `toml:"dashboard"`

	// Logging
	LogLevel  string `toml:"log_level"`  // debug, info, warn, error
	LogFormat string `toml:"log_format"` // text, json
}

// DelegationConfig defines staking configuration.
type DelegationConfig struct {
	AutoCompound     bool     `toml:"auto_compound"`
	CompoundInterval string   `toml:"compound_interval"` // e.g. "1h"
	MinRewardClaim   string   `toml:"min_reward_claim"`  // minimum uqor to trigger claim
	Validators       []string `toml:"validators"`        // validator addresses
	SplitWeights     []int    `toml:"split_weights"`     // weights for multi-validator split
	RebalanceEnabled bool     `toml:"rebalance_enabled"`
	MinReputation    float64  `toml:"min_reputation"` // minimum reputation score
}

// TelemetryConfig defines monitoring intervals.
type TelemetryConfig struct {
	Enabled            bool   `toml:"enabled"`
	ValidatorInterval  string `toml:"validator_interval"`  // e.g. "30s"
	NetworkInterval    string `toml:"network_interval"`    // e.g. "15s"
	BridgeInterval     string `toml:"bridge_interval"`     // e.g. "60s"
	TokenomicsInterval string `toml:"tokenomics_interval"` // e.g. "60s"
}

// DashboardConfig defines UX dashboard settings.
type DashboardConfig struct {
	Enabled  bool   `toml:"enabled"`
	BindAddr string `toml:"bind_addr"` // e.g. ":8420"
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		NodeType:       "sx",
		Version:        "2.6.0",
		ChainID:        "qorechain-diana",
		RPCAddr:        "http://localhost:26657",
		GRPCAddr:       "localhost:9090",
		TrustPeriod:    "168h",
		MaxClockDrift:  "10s",
		PrimaryAddr:    "http://localhost:26657",
		DataDir:        defaultDataDir(),
		KeyringBackend: "file",
		KeyName:        "operator",
		Delegation: DelegationConfig{
			AutoCompound:     true,
			CompoundInterval: "1h",
			MinRewardClaim:   "1000000", // 1 QOR
			RebalanceEnabled: true,
			MinReputation:    0.5,
		},
		Telemetry: TelemetryConfig{
			Enabled:            true,
			ValidatorInterval:  "30s",
			NetworkInterval:    "15s",
			BridgeInterval:     "60s",
			TokenomicsInterval: "60s",
		},
		Dashboard: DashboardConfig{
			Enabled:  false,
			BindAddr: ":8420",
		},
		LogLevel:  "info",
		LogFormat: "text",
	}
}

func defaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".qorechain-lightnode")
}

// Load reads config from a TOML file.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("reading config: %w", err)
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

// Save writes config to a TOML file.
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}
