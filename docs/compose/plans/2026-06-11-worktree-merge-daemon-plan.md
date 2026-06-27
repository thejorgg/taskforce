# Worktrees, Multi-Branch Merge, and Machine-Wide Daemon Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use compose:subagent (recommended) or compose:execute to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a machine-wide daemon, worktree management, and a merge-all workflow with interactive conflict resolution.

**Architecture:**
1.  **Daemon Relocation**: Move daemon state from `.taskforce/` to `~/.local/share/taskforce/`.
2.  **Worktree Package**: Create a new `internal/worktree` package to manage git worktrees in a central directory.
3.  **Merge Workflow**: Create a new `internal/merge` package to handle the merge-all logic and conflict detection.
4.  **CLI Integration**: Add `taskforce worktree` and `taskforce merge` commands.

**Tech Stack:** Go, Git CLI (`exec.Command`), `os.UserHomeDir`.

---
### Task 1: Machine-Wide Daemon State

**Covers:** [S3]

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `cmd/taskforce/main.go`

- [ ] **Step 1: Update `daemon.dir` to use a global path**

```go
// internal/daemon/daemon.go
func dir(repo string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to local .taskforce if home is unavailable
		return filepath.Join(repo, ".taskforce")
	}
	// Use a sanitized repo name for the global dir
	repoSlug := strings.ReplaceAll(strings.TrimPrefix(repo, "/"), "/", "_")
	return filepath.Join(home, ".local", "share", "taskforce", "repos", repoSlug)
}
```

- [ ] **Step 2: Update daemon run loop to scan multiple repos**

Modify `processPending` to scan the global `repos/` directory for pending jobs across all tracked repos instead of just the current one.

- [ ] **Step 3: Run `go test ./internal/daemon/...` to verify existing tests still pass.**

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "refactor: move daemon state to machine-wide directory"
```

---

### Task 2: Worktree Management

**Covers:** [S4]

**Files:**
- Create: `internal/worktree/worktree.go`
- Create: `internal/worktree/worktree_test.go`
- Create: `cmd/taskforce/worktreecmd.go`

- [ ] **Step 1: Write tests for worktree operations**

```go
// internal/worktree/worktree_test.go
func TestAddWorktree(t *testing.T) {
	// Test creating a worktree
}

func TestListWorktrees(t *testing.T) {
	// Test listing tracked worktrees
}
```

- [ ] **Step 2: Implement `internal/worktree/worktree.go`**

Create functions: `Add(repo, branch)`, `List(repo)`, `Remove(repo, branch)` using `git worktree add/list/remove`.

- [ ] **Step 3: Implement CLI commands in `cmd/taskforce/worktreecmd.go`**

Add `taskforce worktree add <branch>`, `list`, and `remove <branch>` subcommands.

- [ ] **Step 4: Run tests and verify CLI.**

- [ ] **Step 5: Commit**

```bash
git add internal/worktree/ cmd/taskforce/worktreecmd.go
git commit -m "feat: add worktree management commands"
```

---

### Task 3: Merge-All Workflow

**Covers:** [S5]

**Files:**
- Create: `internal/merge/merge.go`
- Create: `cmd/taskforce/mergecmd.go`

- [ ] **Step 1: Implement `internal/merge/merge.go`**

Implement a `MergeAll(repo, target)` function that:
1. Calls `worktree.List`.
2. Iterates and runs `git merge`.
3. Returns an error or status indicating if a conflict occurred.

- [ ] **Step 2: Implement conflict detection and modal/CLI prompt**

```go
func HandleConflict(branch string) {
	fmt.Printf("Merge conflict in %s. Open in (V)S Code, (C)ursor, or (S)kip? ", branch)
	// logic to open editor
}
```

- [ ] **Step 3: Implement `cmd/taskforce/mergecmd.go`**

Add `taskforce merge-all --target <branch>` command.

- [ ] **Step 4: Add Exfil warning in `internal/exfil/exfil.go`**

Check if the branch was recently part of a complex merge before running exfil.

- [ ] **Step 5: Run full test suite `go test ./...`.**

- [ ] **Step 6: Commit**

```bash
git add internal/merge/ cmd/taskforce/mergecmd.go internal/exfil/exfil.go
git commit -m "feat: add merge-all workflow with conflict resolution"
```
