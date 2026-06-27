# TaskForce: Worktrees, Multi-Branch Merge, and Machine-Wide Daemon

## [S1] Problem
Managing multiple concurrent feature branches is cumbersome when they all share a single working tree. Developers must manually switch branches, risking context loss and accidental changes in the wrong branch. Additionally, the current per-repo daemon state in `.taskforce/` scales poorly for users managing many repositories simultaneously.

## [S2] Solution Overview
Introduce a dedicated worktree management system and a machine-wide daemon.
1. **Machine-Wide Daemon**: Relocate daemon state to `~/.local/share/taskforce/` to manage jobs across all repos.
2. **Worktree Commands**: Provide `taskforce worktree list`, `add`, and `remove` to isolate branches in separate directories.
3. **Merge-All Workflow**: A `taskforce merge-all` command that iterates through tracked worktrees, merging them into the current branch one by one.
4. **Interactive Conflict Resolution**: When a merge fails, TaskForce pauses and opens a modal (or prompts in CLI) to resolve using VS Code, Cursor, or command-line tools.

## [S3] Machine-Wide Daemon State
- **Location**: `~/.local/share/taskforce/`
- **Structure**:
  - `daemon.json`: Global daemon state (PID, status, heartbeat).
  - `repos/<repo-slug>/`: Per-repo job queues and logs, indexed by a hash or sanitized path of the repo.
- **Lifecycle**: `taskforce daemon start` now manages this global state. It scans registered repos for pending jobs.

## [S4] Worktree Management
Worktrees are stored in a central location: `~/.local/share/taskforce/worktrees/<branch-name>`.
- `taskforce worktree add <branch>`: Clones/creates a worktree from the current repo.
- `taskforce worktree list`: Shows all tracked worktrees and their sync status.
- `taskforce worktree remove <branch>`: Deletes the worktree and updates the registry.

## [S5] Merge-All & Conflict Resolution
- `taskforce merge-all --target <branch>` (defaults to current branch).
- **Workflow**:
  1. Identify all active worktrees.
  2. For each, attempt `git merge <branch>`.
  3. **Conflict handling**: If merge fails:
     - Display a "Merge Conflict" modal/prompt.
     - Options: "Open in VS Code", "Open in Cursor", "Show CLI command", "Skip", "Abort All".
     - TaskForce waits for the user to resolve the conflict in the external tool and commit.
- **Exfil Warning**: Before performing any exfil (push/PR) on a branch that has been part of a complex merge, display a confirmation warning.

## [S6] Exfil Integration
Exfil hooks (commit, push, PR) will be triggered from the machine-wide daemon. The `exfil.branch` configuration will now resolve against the worktree registry if the branch is managed by TaskForce.
