package merge

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
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

func MergeAll(repo, target string) ([]MergeResult, error) {
	worktrees, err := worktree.List(repo)
	if err != nil {
		return nil, fmt.Errorf("listing worktrees: %w", err)
	}

	if len(worktrees) == 0 {
		return nil, fmt.Errorf("no tracked worktrees found")
	}

	var results []MergeResult
	for _, wt := range worktrees {
		if wt.Branch == target {
			continue
		}
		result := mergeBranch(wt, target)
		results = append(results, result)
		if result.Error != nil && !result.Skipped {
			break
		}
	}
	return results, nil
}

func mergeBranch(wt worktree.Worktree, target string) MergeResult {
	result := MergeResult{Branch: wt.Branch}

	cmd := exec.Command("git", "merge", target)
	cmd.Dir = wt.Path
	output, err := cmd.CombinedOutput()
	if err != nil {
		if isConflict(output) {
			result.Conflict = true
			action := HandleConflict(wt.Branch)
			switch action {
			case ActionOpenVSCode:
				openEditor("code", wt.Path)
				waitForResolution(wt.Path)
			case ActionOpenCursor:
				openEditor("cursor", wt.Path)
				waitForResolution(wt.Path)
			case ActionShowCLI:
				showCLICommand(target)
				waitForResolution(wt.Path)
			case ActionSkip:
				result.Skipped = true
				abortMerge(wt.Path)
				return result
			case ActionAbortAll:
				result.Skipped = true
				abortMerge(wt.Path)
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

func showCLICommand(target string) {
	fmt.Printf("\nTo resolve the conflict, run:\n")
	fmt.Printf("  git merge %s\n", target)
	fmt.Printf("  # resolve conflicts, then:\n")
	fmt.Printf("  git add .\n")
	fmt.Printf("  git commit\n\n")
}

func waitForResolution(path string) {
	fmt.Print("\nPress Enter when conflicts are resolved (or 'a' to abort): ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "a" {
		abortMerge(path)
	} else {
		commitMerge(path)
	}
}

func abortMerge(path string) {
	cmd := exec.Command("git", "merge", "--abort")
	cmd.Dir = path
	_ = cmd.Run()
}

func commitMerge(path string) {
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = path
	_ = cmd.Run()
}

type ExfilWarning struct {
	Branch  string
	Message string
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
