package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// A node upgraded in place keeps the config.toml its first version wrote.
func TestTheBinaryVersionWinsOverTheConfigFile(t *testing.T) {
	dir := t.TempDir()
	CfgFile, HomeDir = filepath.Join(dir, "config.toml"), dir
	t.Cleanup(func() { CfgFile, HomeDir, Version = "", "", "" })
	if err := os.WriteFile(CfgFile, []byte("version = \"3.1.6\"\nchain_id = \"qorechain-vladi\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	Version = "3.1.7"
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != "3.1.7" {
		t.Fatalf("version = %q, want the binary's 3.1.7", cfg.Version)
	}
}
