// Package rescue runs a fallback Relay harness in a temporary, neutralized
// copy of the repo when a primary harness appears to refuse the task.
package rescue

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/harness"
	"github.com/thejorgg/taskforce/internal/runner"
)

type Team struct {
	Config config.RescueConfig
	Agents map[string]config.AgentConfig
	Repo   string
	Runner runner.Runner
}

type Result struct {
	TempRepo string
	Command  domain.CommandSpec
	Run      domain.CommandResult
	Attempts int
	Skipped  bool
	Reason   string
}

type State struct {
	Repos map[string]RepoState `json:"repos"`
}

type RepoState struct {
	Repo     string            `json:"repo"`
	Mappings map[string]string `json:"mappings"`
}

func Triggered(cfg config.RescueConfig, result *domain.CommandResult) bool {
	if !cfg.Enabled || result == nil || result.ExitCode == 0 || result.Skipped {
		return false
	}
	text := strings.ToLower(result.Output() + "\n" + result.Error)
	for _, trigger := range cfg.Triggers {
		trigger = strings.ToLower(strings.TrimSpace(trigger))
		if trigger != "" && strings.Contains(text, trigger) {
			return true
		}
	}
	return false
}

func ActiveMappings(repo string) (map[string]string, bool, error) {
	state, err := loadState()
	if err != nil {
		return nil, false, err
	}
	entry, ok := state.Repos[repoKey(repo)]
	if !ok || len(entry.Mappings) == 0 {
		return nil, false, nil
	}
	out := make(map[string]string, len(entry.Mappings))
	for k, v := range entry.Mappings {
		out[k] = v
	}
	return out, true, nil
}

func StoreMappings(repo string, mappings map[string]string) error {
	if strings.TrimSpace(repo) == "" || len(mappings) == 0 {
		return nil
	}
	state, err := loadState()
	if err != nil {
		return err
	}
	if state.Repos == nil {
		state.Repos = map[string]RepoState{}
	}
	cp := make(map[string]string, len(mappings))
	for k, v := range mappings {
		cp[k] = v
	}
	state.Repos[repoKey(repo)] = RepoState{Repo: repo, Mappings: cp}
	return saveState(state)
}

func (t Team) Run(ctx context.Context, stage string, mode harness.Mode, task domain.TaskPacket, prompt string) (Result, error) {
	if !t.Config.Enabled {
		return Result{Skipped: true, Reason: "rescue disabled"}, nil
	}
	if strings.TrimSpace(t.Config.Agent) == "" {
		return Result{}, errors.New("rescue agent is empty")
	}
	stageCfg := config.StageConfig{
		Enabled: true,
		Agent:   t.Config.Agent,
		Model:   t.Config.Model,
		Prompt:  prompt,
		Mutates: mode == harness.ModeBuild,
	}
	return t.RunStage(ctx, stage+".rescue", mode, task, prompt, stageCfg, t.Config.Mappings)
}

func (t Team) RunStage(ctx context.Context, stage string, mode harness.Mode, task domain.TaskPacket, prompt string, stageCfg config.StageConfig, configuredMappings map[string]string) (Result, error) {
	if !t.Config.Enabled {
		return Result{Skipped: true, Reason: "rescue disabled"}, nil
	}
	tempRepo, mappings, err := t.prepare(configuredMappings)
	if err != nil {
		return Result{}, err
	}
	spec, err := harness.Resolve(harness.Request{
		Stage:  stage,
		Mode:   mode,
		Prompt: prompt,
		Task:   task,
		Config: stageCfg,
		Agents: t.Agents,
		Repo:   tempRepo,
	})
	if err != nil {
		return Result{TempRepo: tempRepo}, err
	}
	if err := StoreMappings(t.Repo, configuredMappings); err != nil {
		return Result{TempRepo: tempRepo, Command: spec}, err
	}
	spec.WorkDir = tempRepo
	rescueRunner := t.Runner
	rescueRunner.Options.Repo = tempRepo
	attempts := t.Config.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var run domain.CommandResult
	for attempt := 1; attempt <= attempts; attempt++ {
		run = rescueRunner.Run(ctx, spec)
		out := Result{TempRepo: tempRepo, Command: spec, Run: run, Attempts: attempt}
		if run.ExitCode == 0 && !run.Skipped {
			if err := restore(tempRepo, t.Repo, mappings); err != nil {
				return out, err
			}
			return out, nil
		}
		if ctx.Err() != nil {
			return out, nil
		}
	}
	return Result{TempRepo: tempRepo, Command: spec, Run: run, Attempts: attempts}, nil
}

func (t Team) prepare(configuredMappings map[string]string) (string, map[string]string, error) {
	if strings.TrimSpace(t.Repo) == "" {
		return "", nil, errors.New("rescue repo is empty")
	}
	if len(configuredMappings) == 0 {
		configuredMappings = t.Config.Mappings
	}
	mappings := materializeMappings(configuredMappings)
	root := t.Config.Root
	if strings.TrimSpace(root) == "" {
		root = filepath.Join(os.TempDir(), ".relay-rescue")
	}
	tempRepo := filepath.Join(root, filepath.Base(t.Repo))
	if err := os.RemoveAll(tempRepo); err != nil {
		return "", nil, err
	}
	if err := copyTree(t.Repo, tempRepo); err != nil {
		return "", nil, err
	}
	if err := ensureNoReplacementCollisions(tempRepo, mappings); err != nil {
		return "", nil, err
	}
	if err := rewriteTree(tempRepo, mappings); err != nil {
		return "", nil, err
	}
	return tempRepo, mappings, nil
}

func repoKey(repo string) string {
	abs, err := filepath.Abs(repo)
	if err == nil {
		repo = abs
	}
	sum := sha1.Sum([]byte(repo))
	return hex.EncodeToString(sum[:])
}

func statePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "taskforce", "rescue-state.json"), nil
}

func loadState() (State, error) {
	path, err := statePath()
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Repos: map[string]RepoState{}}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if state.Repos == nil {
		state.Repos = map[string]RepoState{}
	}
	return state, nil
}

func saveState(state State) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func materializeMappings(configured map[string]string) map[string]string {
	out := make(map[string]string, len(configured))
	keys := make([]string, 0, len(configured))
	for key := range configured {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		base := strings.TrimSpace(configured[key])
		if base == "" {
			base = "TFNeutral"
		}
		sum := sha1.Sum([]byte(key + "\x00" + base))
		out[key] = fmt.Sprintf("%s__tfrescue_%02d_%s", base, i, hex.EncodeToString(sum[:])[:8])
	}
	return out
}

func restore(tempRepo, repo string, mappings map[string]string) error {
	inverse := make(map[string]string, len(mappings))
	for original, neutral := range mappings {
		inverse[neutral] = original
	}
	return filepath.WalkDir(tempRepo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == tempRepo {
			return nil
		}
		rel, err := filepath.Rel(tempRepo, path)
		if err != nil {
			return err
		}
		if shouldSkip(rel, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(repo, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if utf8.Valid(data) {
			data = []byte(rewriteString(string(data), inverse))
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode().Perm())
	})
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == src {
			return os.MkdirAll(dst, 0o755)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if shouldSkip(rel, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func ensureNoReplacementCollisions(root string, mappings map[string]string) error {
	replacements := make([]string, 0, len(mappings))
	for _, replacement := range mappings {
		replacements = append(replacements, replacement)
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if shouldSkip(rel, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return nil
		}
		text := string(data)
		for _, replacement := range replacements {
			if strings.Contains(text, replacement) {
				return fmt.Errorf("rescue mapping collision: %q already exists in %s", replacement, rel)
			}
		}
		return nil
	})
}

func rewriteTree(root string, mappings map[string]string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if shouldSkip(rel, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return nil
		}
		next := rewriteString(string(data), mappings)
		if next == string(data) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(next), info.Mode().Perm())
	})
}

func rewriteString(text string, mappings map[string]string) string {
	keys := make([]string, 0, len(mappings))
	for key := range mappings {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) == len(keys[j]) {
			return keys[i] < keys[j]
		}
		return len(keys[i]) > len(keys[j])
	})
	for _, key := range keys {
		text = strings.ReplaceAll(text, key, mappings[key])
	}
	return text
}

func shouldSkip(rel string, d fs.DirEntry) bool {
	name := d.Name()
	if name == ".git" || name == ".taskforce" {
		return true
	}
	return strings.HasPrefix(rel, ".git"+string(filepath.Separator)) ||
		strings.HasPrefix(rel, ".taskforce"+string(filepath.Separator))
}
