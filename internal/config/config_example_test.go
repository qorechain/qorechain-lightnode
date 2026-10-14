package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// Every key in the shipped example must land on a field. Until 3.1.6
// log_level and log_format sat after the [dashboard] header, so TOML read them
// as dashboard settings and an operator who changed them saw no effect.
func TestExampleConfigHasNoStrayKeys(t *testing.T) {
	var cfg Config
	md, err := toml.DecodeFile("../../config.example.toml", &cfg)
	if err != nil {
		t.Fatalf("decoding config.example.toml: %v", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		t.Fatalf("keys in config.example.toml that no field reads: %v", undecoded)
	}
	if cfg.LogLevel != "info" || cfg.LogFormat != "text" {
		t.Errorf("log_level=%q log_format=%q, want info/text read at the top level", cfg.LogLevel, cfg.LogFormat)
	}
}
