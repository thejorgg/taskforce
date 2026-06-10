package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLoadValidateDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "taskforce.json")
	if err := WriteDefault(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Relay.Control.Agent != "codex" {
		t.Fatalf("control agent = %q", cfg.Relay.Control.Agent)
	}
	if err := Validate(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatal("default config should be newline-terminated")
	}
}
