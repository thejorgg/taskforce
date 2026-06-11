package merge

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/thejorgg/taskforce/internal/worktree"
)

type ConflictAction int

const (
	ActionOpenVSCode ConflictAction = iota
	ActionOpenCursor
	ActionShowCLI
	ActionSkip
	ActionAbortAll
)

type MergeResult struct {
	Branch   string
	Conflict bool
	Skipped  bool
	Error    error
}

func MergeAll(repo, target string) ([]MergeResult, []string, error) {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving repo path: %w", err)
	}

	if _, err := os.Stat(filepath.Join(absRepo, ".git")); err != nil {
		return nil, nil, fmt.Errorf("not a git repository: %s", absRepo)
	}

	worktrees, err := worktree.List(absRepo)
	if err != nil {
		return nil, nil, fmt.Errorf("listing worktrees: %w", err)
	}

	if len(worktrees) == 0 {
		return nil, nil, fmt.Errorf("no tracked worktrees found")
	}

	checkout := exec.Command("git", "checkout", target)
	checkout.Dir = absRepo
	output, err := checkout.CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("checking out target branch %s: %s", target, string(output))
	}

	var results []MergeResult
	var mergedBranches []string
	for _, wt := range worktrees {
		if wt.Branch == target {
			continue
		}
		result := mergeBranch(absRepo, wt, target)
		results = append(results, result)
		if !result.Skipped && result.Error == nil {
			mergedBranches = append(mergedBranches, wt.Branch)
		}
		if result.Error != nil && !result.Skipped {
			break
		}
	}
	return results, mergedBranches, nil
}

func mergeBranch(repo string, wt worktree.Worktree, target string) MergeResult {
	result := MergeResult{Branch: wt.Branch}

	cmd := exec.Command("git", "merge", wt.Branch)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	if err != nil {
		if isConflict(output) {
			result.Conflict = true
			action := HandleConflict(wt.Branch)
			switch action {
			case ActionOpenVSCode:
				openEditor("code", repo)
				waitForResolution(repo, wt.Branch, target)
			case ActionOpenCursor:
				openEditor("cursor", repo)
				waitForResolution(repo, wt.Branch, target)
			case ActionShowCLI:
				showCLICommand(wt.Branch)
				waitForResolution(repo, wt.Branch, target)
			case ActionSkip:
				result.Skipped = true
				if err := abortMerge(repo); err != nil {
					fmt.Printf("Warning: failed to abort merge: %v\n", err)
				}
				return result
			case ActionAbortAll:
				result.Skipped = true
				if err := abortMerge(repo); err != nil {
					fmt.Printf("Warning: failed to abort merge: %v\n", err)
				}
				result.Error = fmt.Errorf("abort all merges")
				return result
			}
		} else {
			result.Error = fmt.Errorf("merge failed: %s", string(output))
		}
	}
	return result
}

func isConflict(output []byte) bool {
	return strings.Contains(string(output), "CONFLICT") ||
		strings.Contains(string(output), "conflict")
}

func HandleConflict(branch string) ConflictAction {
	fmt.Printf("\nMerge conflict in branch %q\n", branch)
	fmt.Println("Options:")
	fmt.Println("  (V) Open in VS Code")
	fmt.Println("  (C) Open in Cursor")
	fmt.Println("  (S) Show CLI command")
	fmt.Println("  (X) Skip this branch")
	fmt.Println("  (A) Abort all merges")
	fmt.Print("\nChoose [V/C/S/X/A]: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToUpper(input))

	switch input {
	case "V":
		return ActionOpenVSCode
	case "C":
		return ActionOpenCursor
	case "S":
		return ActionShowCLI
	case "X":
		return ActionSkip
	case "A":
		return ActionAbortAll
	default:
		fmt.Println("Invalid choice, skipping branch")
		return ActionSkip
	}
}

func openEditor(editor, path string) {
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to open %s: %v\n", editor, err)
	}
}

func showCLICommand(branch string) {
	fmt.Printf("\nTo resolve the conflict, run:\n")
	fmt.Printf("  git merge %s\n", branch)
	fmt.Printf("  # resolve conflicts, then:\n")
	fmt.Printf("  git add .\n")
	fmt.Printf("  git commit -m \"Merge %s\"\n\n", branch)
}

func waitForResolution(path, branch, target string) {
	fmt.Print("\nPress Enter when conflicts are resolved (or 'a' to abort): ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "a" {
		if err := abortMerge(path); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	} else {
		if err := commitMerge(path, branch, target); err != nil {
			fmt.Printf("Error committing merge: %v\n", err)
		}
	}
}

func abortMerge(path string) error {
	cmd := exec.Command("git", "merge", "--abort")
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git merge --abort failed: %s", string(output))
	}
	return nil
}

func commitMerge(path, branch, target string) error {
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git add failed: %s", string(output))
	}

	cmd = exec.Command("git", "commit", "-m", fmt.Sprintf("Merge %s into %s", branch, target))
	cmd.Dir = path
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit failed: %s", string(output))
	}
	return nil
}

type ExfilWarning struct {
	Branch  string
	Message string
}

func storeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	return filepath.Join(home, ".local", "share", "taskforce")
}

func StoreRecentMerges(branches []string) error {
	dir := storeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating store dir: %w", err)
	}
	data := []byte(strings.Join(branches, "\n"))
	path := filepath.Join(dir, "recent-merges.txt")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing recent merges: %w", err)
	}
	return nil
}

func LoadRecentMerges() []string {
	path := filepath.Join(storeDir(), "recent-merges.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func CheckMergeWarning(branch string, recentMerges []string) *ExfilWarning {
	for _, m := range recentMerges {
		if m == branch {
			return &ExfilWarning{
				Branch:  branch,
				Message: fmt.Sprintf("Warning: Branch %q was recently part of a merge. Review changes before pushing.", branch),
			}
		}
	}
	return nil
}
