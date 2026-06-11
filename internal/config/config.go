package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Pipeline PipelineConfig         `json:"pipeline"`
	Agents   map[string]AgentConfig `json:"agents,omitempty"`
	Relay    RelayConfig            `json:"relay"`
	Scope    ScopeConfig            `json:"scope"`
	Exfil    ExfilConfig            `json:"exfil"`
	Runtime  RuntimeConfig          `json:"runtime"`
}

// AgentConfig defines a custom harness that Relay stages can reference by
// name. Run/Argv are used for both plan and build modes unless a mode-specific
// command is set. Placeholders {{prompt}}, {{model}}, {{task_id}},
// {{task_title}}, {{task_description}}, {{repo}}, and {{mode}} expand in run
// strings and argv elements; commands also receive TASKFORCE_* environment
// variables for shell-safe access to the same values.
type AgentConfig struct {
	Run     string            `json:"run,omitempty"`
	Argv    []string          `json:"argv,omitempty"`
	Plan    *AgentCommand     `json:"plan,omitempty"`
	Build   *AgentCommand     `json:"build,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Timeout string            `json:"timeout,omitempty"`
}

// AgentCommand is a mode-specific command override inside an AgentConfig.
type AgentCommand struct {
	Run  string   `json:"run,omitempty"`
	Argv []string `json:"argv,omitempty"`
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
	Model   string            `json:"model"`
	Prompt  string            `json:"prompt"`
	Run     string            `json:"run"`
	Argv    []string          `json:"argv"`
	Env     map[string]string `json:"env"`
	WorkDir string            `json:"work_dir,omitempty"`
	Timeout string            `json:"timeout"`
	Mutates bool              `json:"mutates"`
}

type HookConfig struct {
	Name     string            `json:"name"`
	Run      string            `json:"run"`
	Argv     []string          `json:"argv"`
	Env      map[string]string `json:"env"`
	WorkDir  string            `json:"work_dir,omitempty"`
	Timeout  string            `json:"timeout"`
	Required bool              `json:"required"`
}

type Level string

const (
	LevelProfile   Level = "profile"
	LevelProject   Level = "project"
	LevelWorkspace Level = "workspace"
	LevelEffective Level = "effective"
)

type Paths struct {
	Profile   string
	Project   string
	Workspace string
	Explicit  string
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
		Scope: ScopeConfig{Hooks: []HookConfig{}},
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

func LoadEffective(repo, explicit string) (Config, Paths, error) {
	paths, err := DiscoverPaths(repo, explicit)
	if err != nil {
		return Config{}, paths, err
	}
	base, err := marshalMap(Default())
	if err != nil {
		return Config{}, paths, err
	}
	for _, path := range []string{paths.Profile, paths.Project, paths.Workspace, paths.Explicit} {
		if path == "" {
			continue
		}
		next, ok, err := readMap(path)
		if err != nil {
			return Config{}, paths, err
		}
		if !ok {
			continue
		}
		base = mergeMap(base, next)
	}
	data, err := json.Marshal(base)
	if err != nil {
		return Config{}, paths, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, paths, err
	}
	return cfg, paths, nil
}

func DiscoverPaths(repo, explicit string) (Paths, error) {
	var paths Paths
	profile, err := ProfilePath()
	if err != nil {
		return paths, err
	}
	paths.Profile = profile
	root, err := repoRoot(repo)
	if err != nil {
		return paths, err
	}
	if root != "" {
		paths.Project = filepath.Join(root, "taskforce.json")
		paths.Workspace = filepath.Join(root, ".taskforce", "config.json")
	}
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return paths, err
		}
		paths.Explicit = abs
	}
	return paths, nil
}

func ProfilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "taskforce", "config.json"), nil
}

func PathForLevel(paths Paths, level Level) (string, error) {
	switch level {
	case LevelProfile:
		return paths.Profile, nil
	case LevelProject:
		return paths.Project, nil
	case LevelWorkspace:
		return paths.Workspace, nil
	case LevelEffective:
		return "", errors.New("effective config is read-only")
	default:
		return "", fmt.Errorf("unknown config level %q", level)
	}
}

func WriteLevelDefault(path string) error {
	if path == "" {
		return errors.New("empty config path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return WriteDefault(path)
}

func WriteDefault(path string) error {
	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func Show(path string, cfg Config) ([]byte, error) {
	var data []byte
	var err error
	if path != "" {
		if _, statErr := os.Stat(path); statErr == nil {
			data, err = os.ReadFile(path)
		} else if errors.Is(statErr, os.ErrNotExist) {
			data = []byte("{}\n")
		} else {
			err = statErr
		}
	} else {
		data, err = json.MarshalIndent(cfg, "", "  ")
		if err == nil {
			data = append(data, '\n')
		}
	}
	if err != nil {
		return nil, err
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}
	return data, nil
}

func SetValue(path, dotted, raw string) error {
	doc, err := loadEditable(path)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		value = raw
	}
	parts := splitPath(dotted)
	if len(parts) == 0 {
		return errors.New("missing config path")
	}
	setPath(doc, parts, value)
	return writeMap(path, doc)
}

func UnsetValue(path, dotted string) error {
	doc, err := loadEditable(path)
	if err != nil {
		return err
	}
	parts := splitPath(dotted)
	if len(parts) == 0 {
		return errors.New("missing config path")
	}
	unsetPath(doc, parts)
	return writeMap(path, doc)
}

// builtinAgents mirrors the adapters in internal/harness; harness imports
// config, so the names are duplicated here for validation.
var builtinAgents = map[string]bool{
	"claude":   true,
	"codex":    true,
	"opencode": true,
	"gemini":   true,
}

func Validate(cfg Config) error {
	for name, agent := range cfg.Agents {
		if strings.TrimSpace(name) == "" {
			return errors.New("agents entry has an empty name")
		}
		hasDefault := agent.Run != "" || len(agent.Argv) > 0
		hasPlan := agent.Plan != nil && (agent.Plan.Run != "" || len(agent.Plan.Argv) > 0)
		hasBuild := agent.Build != nil && (agent.Build.Run != "" || len(agent.Build.Argv) > 0)
		if !hasDefault && !hasPlan && !hasBuild {
			return fmt.Errorf("agent %q must set run, argv, plan, or build", name)
		}
		if agent.Plan != nil && !hasPlan {
			return fmt.Errorf("agent %q plan must set run or argv", name)
		}
		if agent.Build != nil && !hasBuild {
			return fmt.Errorf("agent %q build must set run or argv", name)
		}
	}
	for _, stage := range []struct {
		name string
		cfg  StageConfig
	}{{"relay.control", cfg.Relay.Control}, {"relay.build", cfg.Relay.Build}} {
		agent := strings.TrimSpace(stage.cfg.Agent)
		if agent == "" || stage.cfg.Run != "" || len(stage.cfg.Argv) > 0 {
			continue
		}
		if _, ok := cfg.Agents[agent]; ok {
			continue
		}
		if !builtinAgents[agent] {
			return fmt.Errorf("%s.agent %q is not a built-in adapter and is not defined in agents", stage.name, agent)
		}
	}
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

func marshalMap(cfg Config) (map[string]any, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(data, &out)
}

func readMap(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return out, true, nil
}

func loadEditable(path string) (map[string]any, error) {
	if path == "" {
		return nil, errors.New("empty config path")
	}
	doc, ok, err := readMap(path)
	if err != nil {
		return nil, err
	}
	if !ok {
		doc = map[string]any{}
	}
	return doc, nil
}

func writeMap(path string, doc map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func mergeMap(base, overlay map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		if left, ok := out[k].(map[string]any); ok {
			if right, ok := v.(map[string]any); ok {
				out[k] = mergeMap(left, right)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func splitPath(path string) []string {
	parts := strings.Split(strings.TrimSpace(path), ".")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func setPath(doc map[string]any, parts []string, value any) {
	cursor := doc
	for _, part := range parts[:len(parts)-1] {
		next, ok := cursor[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			cursor[part] = next
		}
		cursor = next
	}
	cursor[parts[len(parts)-1]] = value
}

func unsetPath(doc map[string]any, parts []string) {
	cursor := doc
	for _, part := range parts[:len(parts)-1] {
		next, ok := cursor[part].(map[string]any)
		if !ok {
			return
		}
		cursor = next
	}
	delete(cursor, parts[len(parts)-1])
}

func repoRoot(repo string) (string, error) {
	if repo == "" {
		repo = "."
	}
	dir, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, "taskforce.json")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Abs(repo)
		}
		dir = parent
	}
}
