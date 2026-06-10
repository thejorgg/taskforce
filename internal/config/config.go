package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type Config struct {
	Pipeline PipelineConfig `json:"pipeline"`
	Relay    RelayConfig    `json:"relay"`
	Scope    ScopeConfig    `json:"scope"`
	Exfil    ExfilConfig    `json:"exfil"`
	Runtime  RuntimeConfig  `json:"runtime"`
}

type PipelineConfig struct {
	Scout StageConfig `json:"scout"`
}

type RelayConfig struct {
	Control StageConfig `json:"control"`
	Build   StageConfig `json:"build"`
	Retries int         `json:"retries"`
}

type ScopeConfig struct {
	Hooks []HookConfig `json:"hooks"`
}

type ExfilConfig struct {
	Branch        string       `json:"branch"`
	Commit        bool         `json:"commit"`
	CommitMessage string       `json:"commit_message"`
	Push          bool         `json:"push"`
	PR            bool         `json:"pr"`
	PRTitle       string       `json:"pr_title"`
	PRBody        string       `json:"pr_body"`
	Hooks         []HookConfig `json:"hooks"`
}

type RuntimeConfig struct {
	Shell   string            `json:"shell"`
	Env     map[string]string `json:"env"`
	Timeout string            `json:"timeout"`
}

type StageConfig struct {
	Enabled bool              `json:"enabled"`
	Agent   string            `json:"agent"`
	Prompt  string            `json:"prompt"`
	Run     string            `json:"run"`
	Argv    []string          `json:"argv"`
	Env     map[string]string `json:"env"`
	Timeout string            `json:"timeout"`
	Mutates bool              `json:"mutates"`
}

type HookConfig struct {
	Name     string            `json:"name"`
	Run      string            `json:"run"`
	Argv     []string          `json:"argv"`
	Env      map[string]string `json:"env"`
	Timeout  string            `json:"timeout"`
	Required bool              `json:"required"`
}

func Default() Config {
	return Config{
		Pipeline: PipelineConfig{
			Scout: StageConfig{Enabled: false},
		},
		Relay: RelayConfig{
			Control: StageConfig{
				Enabled: true,
				Agent:   "codex",
				Prompt:  "Inspect the task and produce an implementation plan.",
			},
			Build: StageConfig{
				Enabled: true,
				Agent:   "opencode",
				Prompt:  "Implement the approved plan and report changed files.",
				Mutates: true,
			},
			Retries: 1,
		},
		Scope: ScopeConfig{
			Hooks: []HookConfig{
				{Name: "lint", Run: "npm run lint", Required: true},
				{Name: "tests", Run: "npm test", Required: true},
			},
		},
		Exfil: ExfilConfig{
			Branch:        "taskforce/{{task_id}}",
			Commit:        true,
			CommitMessage: "TaskForce: {{task_title}}",
			Push:          false,
			PR:            false,
			PRTitle:       "{{task_title}}",
			PRBody:        "Automated TaskForce handoff for {{task_id}}.",
		},
		Runtime: RuntimeConfig{
			Timeout: "30m",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

func WriteDefault(path string) error {
	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func Validate(cfg Config) error {
	for _, hook := range cfg.Scope.Hooks {
		if hook.Name == "" {
			return errors.New("scope hook missing name")
		}
		if hook.Run == "" && len(hook.Argv) == 0 {
			return fmt.Errorf("scope hook %q must set run or argv", hook.Name)
		}
	}
	for _, hook := range cfg.Exfil.Hooks {
		if hook.Name == "" {
			return errors.New("exfil hook missing name")
		}
		if hook.Run == "" && len(hook.Argv) == 0 {
			return fmt.Errorf("exfil hook %q must set run or argv", hook.Name)
		}
	}
	return nil
}
