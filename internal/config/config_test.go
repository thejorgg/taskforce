package config

import (
	"encoding/json"
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
	if len(cfg.Scope.Hooks) != 0 {
		t.Fatalf("default scope hooks = %#v, want none", cfg.Scope.Hooks)
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

func TestLoadEffectiveMergesProfileProjectWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	profile, err := ProfilePath()
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, profile, map[string]any{
		"relay": map[string]any{"build": map[string]any{"agent": "codex", "model": "openai/gpt-5"}},
	})
	writeJSON(t, filepath.Join(repo, "taskforce.json"), map[string]any{
		"scope": map[string]any{"hooks": []any{map[string]any{"name": "go", "argv": []any{"go", "test", "./..."}, "required": true}}},
	})
	writeJSON(t, filepath.Join(repo, ".taskforce", "config.json"), map[string]any{
		"relay": map[string]any{"build": map[string]any{"agent": "custom", "argv": []any{"my-agent", "--do-it"}}},
	})

	cfg, paths, err := LoadEffective(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if paths.Profile != profile {
		t.Fatalf("profile path = %q, want %q", paths.Profile, profile)
	}
	if cfg.Relay.Build.Agent != "custom" {
		t.Fatalf("build agent = %q", cfg.Relay.Build.Agent)
	}
	if got := cfg.Relay.Build.Argv; len(got) != 2 || got[0] != "my-agent" {
		t.Fatalf("build argv = %#v", got)
	}
	if cfg.Relay.Build.Model != "openai/gpt-5" {
		t.Fatalf("build model = %q", cfg.Relay.Build.Model)
	}
	if len(cfg.Scope.Hooks) != 1 || cfg.Scope.Hooks[0].Name != "go" {
		t.Fatalf("scope hooks = %#v", cfg.Scope.Hooks)
	}
}

func TestConfigSetUnsetValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SetValue(path, "relay.build.argv", `["opencode","run","--model","anthropic/claude-sonnet-4"]`); err != nil {
		t.Fatal(err)
	}
	if err := SetValue(path, "relay.build.mutates", `false`); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Relay.Build.Mutates {
		t.Fatal("mutates should be false after set")
	}
	if len(cfg.Relay.Build.Argv) != 4 {
		t.Fatalf("argv = %#v", cfg.Relay.Build.Argv)
	}
	if err := UnsetValue(path, "relay.build.argv"); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Relay.Build.Argv) != 0 {
		t.Fatalf("argv after unset = %#v", cfg.Relay.Build.Argv)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAgentsConfigMergeAndValidate(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "taskforce.json")
	body := `{"agents":{"aider":{"argv":["aider","--message","{{prompt}}"],"build":{"argv":["aider","--yes","--message","{{prompt}}"]}}}}`
	if err := os.WriteFile(project, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(project)
	if err != nil {
		t.Fatal(err)
	}
	agent, ok := cfg.Agents["aider"]
	if !ok {
		t.Fatal("agents.aider not parsed")
	}
	if agent.Build == nil || len(agent.Build.Argv) != 4 {
		t.Fatalf("agent build variant not parsed: %+v", agent)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid agents config rejected: %v", err)
	}
}

func TestValidateRejectsEmptyAgent(t *testing.T) {
	cfg := Default()
	cfg.Agents = map[string]AgentConfig{"broken": {}}
	if err := Validate(cfg); err == nil {
		t.Fatal("agent without any command must be rejected")
	}
	cfg.Agents = map[string]AgentConfig{"broken": {Plan: &AgentCommand{}}}
	if err := Validate(cfg); err == nil {
		t.Fatal("agent plan without run/argv must be rejected")
	}
}

func TestValidateRejectsUnknownRelayAgent(t *testing.T) {
	cfg := Default()
	cfg.Relay.Build.Agent = "nonexistent"
	if err := Validate(cfg); err == nil {
		t.Fatal("unknown relay agent must fail validation")
	}
	cfg.Agents = map[string]AgentConfig{"nonexistent": {Run: "mytool {{prompt}}"}}
	if err := Validate(cfg); err != nil {
		t.Fatalf("agent defined in registry should validate: %v", err)
	}
	cfg.Agents = nil
	cfg.Relay.Build.Run = "echo custom"
	if err := Validate(cfg); err != nil {
		t.Fatalf("stage run override should validate: %v", err)
	}
}
