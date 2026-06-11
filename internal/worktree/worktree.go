package worktree

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const registryFileName = "registry.json"

type Worktree struct {
	Branch   string    `json:"branch"`
	Path     string    `json:"path"`
	AddedAt  time.Time `json:"added_at"`
	Parent   string    `json:"parent"`
}

type Registry struct {
	Worktrees []Worktree `json:"worktrees"`
}

func registryDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	return filepath.Join(home, ".local", "share", "taskforce", "worktrees")
}

func registryPath() string {
	return filepath.Join(registryDir(), registryFileName)
}

func LoadRegistry() (*Registry, error) {
	reg := &Registry{}
	data, err := os.ReadFile(registryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return reg, nil
		}
		return nil, fmt.Errorf("reading registry: %w", err)
	}
	if err := json.Unmarshal(data, reg); err != nil {
		return nil, fmt.Errorf("parsing registry: %w", err)
	}
	return reg, nil
}

func SaveRegistry(reg *Registry) error {
	if err := os.MkdirAll(registryDir(), 0o755); err != nil {
		return fmt.Errorf("creating registry dir: %w", err)
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling registry: %w", err)
	}
	if err := os.WriteFile(registryPath(), data, 0o644); err != nil {
		return fmt.Errorf("writing registry: %w", err)
	}
	return nil
}

func GitWorktreePath(branch string) string {
	return filepath.Join(registryDir(), branch)
}

func Add(repo, branch string) error {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return fmt.Errorf("resolving repo path: %w", err)
	}

	if _, err := os.Stat(filepath.Join(absRepo, ".git")); err != nil {
		return fmt.Errorf("not a git repository: %s", absRepo)
	}

	worktreePath := GitWorktreePath(branch)
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("worktree already exists at %s", worktreePath)
	}

	if err := os.MkdirAll(registryDir(), 0o755); err != nil {
		return fmt.Errorf("creating worktrees dir: %w", err)
	}

	cmd := exec.Command("git", "worktree", "add", worktreePath, branch)
	cmd.Dir = absRepo
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add failed: %s\n%s", err, string(output))
	}

	reg, err := LoadRegistry()
	if err != nil {
		return err
	}

	reg.Worktrees = append(reg.Worktrees, Worktree{
		Branch:  branch,
		Path:    worktreePath,
		AddedAt: time.Now(),
		Parent:  absRepo,
	})

	return SaveRegistry(reg)
}

func List(repo string) ([]Worktree, error) {
	reg, err := LoadRegistry()
	if err != nil {
		return nil, err
	}

	result := make([]Worktree, 0, len(reg.Worktrees))
	for _, wt := range reg.Worktrees {
		if _, err := os.Stat(wt.Path); os.IsNotExist(err) {
			continue
		}
		result = append(result, wt)
	}

	return result, nil
}

func Remove(repo, branch string) error {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return fmt.Errorf("resolving repo path: %w", err)
	}

	reg, err := LoadRegistry()
	if err != nil {
		return err
	}

	found := -1
	for i, wt := range reg.Worktrees {
		if wt.Branch == branch {
			found = i
			break
		}
	}

	if found == -1 {
		return fmt.Errorf("worktree for branch %q not found in registry", branch)
	}

	wt := reg.Worktrees[found]
	if _, err := os.Stat(wt.Path); err == nil {
		cmd := exec.Command("git", "worktree", "remove", wt.Path)
		cmd.Dir = absRepo
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git worktree remove failed: %s\n%s", err, string(output))
		}
	}

	reg.Worktrees = append(reg.Worktrees[:found], reg.Worktrees[found+1:]...)
	return SaveRegistry(reg)
}

func FormatList(worktrees []Worktree) string {
	if len(worktrees) == 0 {
		return "No tracked worktrees."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%-30s %-40s %-25s\n", "BRANCH", "PATH", "ADDED"))
	b.WriteString(strings.Repeat("-", 95) + "\n")
	for _, wt := range worktrees {
		b.WriteString(fmt.Sprintf("%-30s %-40s %-25s\n", wt.Branch, wt.Path, wt.AddedAt.Format("2006-01-02 15:04:05")))
	}
	return b.String()
}
