# TaskForce — Exhaustive Installation Guide

> This document covers every aspect of installing, configuring, and verifying TaskForce on your system. It is intentionally thorough — read it top-to-bottom on a fresh machine, or jump to the section you need.

---

## Table of Contents

1. [What TaskForce Is](#1-what-taskforce-is)
2. [System Requirements](#2-system-requirements)
3. [Installing Go](#3-installing-go)
4. [Installing TaskForce](#4-installing-taskforce)
   - 4.1 [Go Install (Recommended)](#41-go-install-recommended)
   - 4.2 [Shell Install Script](#42-shell-install-script)
   - 4.3 [Manual Build from Source](#43-manual-build-from-source)
   - 4.4 [Building a Specific Version](#44-building-a-specific-version)
   - 4.5 [Installing to a Custom Location](#45-installing-to-a-custom-location)
5. [Verifying the Installation](#5-verifying-the-installation)
6. [PATH Configuration](#6-path-configuration)
7. [Initial Configuration](#7-initial-configuration)
   - 7.1 [Config Levels and Merge Order](#71-config-levels-and-merge-order)
   - 7.2 [Profile Config (OS User)](#72-profile-config-os-user)
   - 7.3 [Project Config (Repo)](#73-project-config-repo)
   - 7.4 [Workspace Config (Local)](#74-workspace-config-local)
   - 7.5 [Explicit Config Override](#75-explicit-config-override)
8. [Full Configuration Reference](#8-full-configuration-reference)
   - 8.1 [Pipeline Config](#81-pipeline-config)
   - 8.2 [Relay Config](#82-relay-config)
   - 8.3 [Rescue Config](#83-rescue-config)
   - 8.4 [Scope Config](#84-scope-config)
   - 8.5 [Exfil Config](#85-exfil-config)
   - 8.6 [Runtime Config](#86-runtime-config)
   - 8.7 [Agents (Custom Harnesses)](#87-agents-custom-harnesses)
   - 8.8 [Hook Config](#88-hook-config)
   - 8.9 [Stage Config](#89-stage-config)
   - 8.10 [Placeholder Expansion](#810-placeholder-expansion)
   - 8.11 [Environment Variables](#811-environment-variables)
9. [Installing Agent Harnesses](#9-installing-agent-harnesses)
   - 9.1 [Codex](#91-codex)
   - 9.2 [Claude](#92-claude)
   - 9.3 [OpenCode](#93-opencode)
   - 9.4 [Gemini](#94-gemini)
   - 9.5 [Mimo](#95-mimo)
   - 9.6 [Custom Agents](#96-custom-agents)
10. [Daemon Setup](#10-daemon-setup)
    - 10.1 [How the Daemon Works](#101-how-the-daemon-works)
    - 10.2 [Starting the Daemon](#102-starting-the-daemon)
    - 10.3 [Stopping the Daemon](#103-stopping-the-daemon)
    - 10.4 [Checking Daemon Status](#104-checking-daemon-status)
    - 10.5 [Daemon State Directory](#105-daemon-state-directory)
11. [Workspace Switching](#11-workspace-switching)
12. [Running Your First Pipeline](#12-running-your-first-pipeline)
13. [Scope Hooks Setup](#13-scope-hooks-setup)
14. [Exfil Release Configuration](#14-exfil-release-configuration)
15. [Rescue Protocol Configuration](#15-rescue-protocol-configuration)
16. [TUI Dashboard](#16-tui-dashboard)
17. [Troubleshooting](#17-troubleshooting)
18. [Upgrading TaskForce](#18-upgrading-taskforce)
19. [Uninstalling TaskForce](#19-uninstalling-taskforce)
20. [Platform-Specific Notes](#20-platform-specific-notes)
21. [Security Considerations](#21-security-considerations)
22. [Frequently Asked Questions](#22-frequently-asked-questions)

---

## 1. What TaskForce Is

TaskForce is a Go-native AI development command center that coordinates software task intake, dispatch, implementation, review, and release handoff through a five-stage pipeline:

```text
Echo → Dispatch → Relay → Scope → Exfil
```

- **Echo** collects raw signals and normalizes them.
- **Dispatch** turns signals into structured task packets.
- **Relay** executes the implementation loop through Control (plan) and Build (implement), retrying with feedback when Scope rejects.
- **Scope** validates output with review hooks and approval rules.
- **Exfil** commits, pushes, opens PRs, or produces a handoff once Scope approves — pausing for an operator decision unless `--yes`/`--yolo`.

TaskForce provides:
- A terminal UI (TUI) dashboard with live pipeline monitoring
- A local daemon that owns all pipeline execution
- Pluggable AI agent harnesses (Codex, Claude, OpenCode, Gemini, Mimo, or any custom command)
- Layered JSON configuration (defaults, profile, project, workspace, explicit)
- Rescue protocol for handling agent refusals
- Run history, log streaming, and approval gates

TaskForce does NOT do the specialized work itself — it orchestrates your existing tools (Claude, Codex, `go test ./...`, `npm test`, `gh pr create`, etc.).

---

## 2. System Requirements

### Required

| Requirement | Minimum | Recommended |
| --- | --- | --- |
| **Operating System** | Linux, macOS, or Windows (WSL2) | Linux or macOS |
| **Go** | 1.24.2 or later | Latest stable Go release |
| **Disk Space** | ~20 MB for binary + deps | ~50 MB with test coverage artifacts |
| **RAM** | 64 MB for daemon + TUI | 256 MB for large repo orchestration |
| **Terminal** | Any terminal emulator | A terminal with 256-color support, 120+ columns wide |

### Recommended

| Requirement | Purpose |
| --- | --- |
| **Git** | Required for repo detection, exfil branching/committing, and most workflows |
| **GitHub CLI (`gh`)** | Required for PR creation via exfil |
| **jq** | Useful for inspecting TaskForce JSON config and run records |
| **At least one AI agent binary** | Codex, Claude CLI, OpenCode, Gemini CLI, or Mimo — TaskForce orchestrates these |

### Optional

| Requirement | Purpose |
| --- | --- |
| **Node.js / npm** | If your project uses npm-based scope hooks |
| **Python** | If your project uses Python-based scope hooks |
| **Make** | If your project uses Makefile-based commands |

### Terminal Size

The TUI dashboard adapts to terminal size:
- **Full layout**: 120+ columns, 40+ rows — all stage cards, spy view, and legend visible
- **Medium layout**: 80 columns — stage cards compress, legend wraps
- **Minimal layout**: Spy view shrinks to 6-line minimum, legend becomes a grid of at most 4 lines

---

## 3. Installing Go

TaskForce is written in Go and requires Go 1.24.2 or later. If you already have Go installed, skip to [Section 4](#4-installing-taskforce).

### 3.1 Linux (Debian/Ubuntu)

```sh
# Download and install Go
wget https://go.dev/dl/go1.24.2.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.24.2.linux-amd64.tar.gz

# Add Go to PATH (add to ~/.bashrc or ~/.zshrc for persistence)
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin

# Verify
go version
# Should output: go version go1.24.2 linux/amd64
```

### 3.2 Linux (Fedora/RHEL/CentOS)

```sh
# Using the official tarball (same as Debian/Ubuntu above)
wget https://go.dev/dl/go1.24.2.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.24.2.linux-amd64.tar.gz

export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
go version
```

### 3.3 Linux (Arch Linux)

```sh
sudo pacman -S go
go version
```

### 3.4 macOS (Intel)

```sh
# Using Homebrew
brew install go

# Or using the official installer
curl -LO https://go.dev/dl/go1.24.2.darwin-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.24.2.darwin-amd64.tar.gz

export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
go version
```

### 3.5 macOS (Apple Silicon)

```sh
# Using Homebrew (recommended — auto-detects architecture)
brew install go

# Or using the official installer
curl -LO https://go.dev/dl/go1.24.2.darwin-arm64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.24.2.darwin-arm64.tar.gz

export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
go version
```

### 3.6 Windows (WSL2)

```sh
# Inside WSL2 terminal
wget https://go.dev/dl/go1.24.2.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.24.2.linux-amd64.tar.gz

export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
go version
```

### 3.7 Windows (Native — PowerShell)

```powershell
# Download the MSI installer
Invoke-WebRequest -Uri "https://go.dev/dl/go1.24.2.windows-amd64.msi" -OutFile "$env:TEMP\go-installer.msi"
Start-Process msiexec.exe -Wait -ArgumentList "$env:TEMP\go-installer.msi"

# Restart PowerShell, then verify
go version
```

### 3.8 Using goenv (Version Manager)

```sh
# Install goenv
git clone https://github.com/syndbg/goenv.git ~/.goenv

# Add to ~/.bashrc or ~/.zshrc
export GOENV_ROOT="$HOME/.goenv"
export PATH="$GOENV_ROOT/bin:$PATH"
eval "$(goenv init -)"

# Install and use Go 1.24.2
goenv install 1.24.2
goenv global 1.24.2

go version
```

### 3.9 Verifying Go Installation

After installing Go, verify the GOPATH and GOROOT are correct:

```sh
go env GOPATH
# Typically: /home/<user>/go or /Users/<user>/go

go env GOROOT
# Typically: /usr/local/go

go env GOBIN
# Typically empty — binaries go to $GOPATH/bin
```

The `go install` command places binaries in `$GOPATH/bin` (or `$GOBIN` if set). Make sure this directory is in your `PATH`.

---

## 4. Installing TaskForce

### 4.1 Go Install (Recommended)

This is the simplest and most idiomatic way to install TaskForce:

```sh
go install github.com/thejorgg/taskforce/cmd/taskforce@latest
```

Or, if you have cloned the repository:

```sh
git clone https://github.com/thejorgg/taskforce.git
cd taskforce
go install ./cmd/taskforce
```

The binary is placed at `$GOPATH/bin/taskforce` (typically `~/go/bin/taskforce`).

**Advantages:**
- Automatic dependency resolution
- Version tracking via `go install`
- Easy to upgrade (`go install ...@latest`)
- No temporary build artifacts

### 4.2 Shell Install Script

The repository includes `install.sh` for a self-contained build-and-install:

```sh
git clone https://github.com/thejorgg/taskforce.git
cd taskforce
./install.sh
```

**Default install location:** `$HOME/.local/bin/taskforce`

**Custom install location:**

```sh
INSTALL_DIR=/usr/local/bin ./install.sh
```

**What install.sh does:**
1. Builds the binary with `go build -o taskforce ./cmd/taskforce`
2. Creates the install directory if it doesn't exist
3. Copies the binary to the target path
4. Makes it executable (`chmod +x`)
5. Cleans up the build artifact

**Requirements:**
- Go must be installed and in PATH
- Write access to the install directory

### 4.3 Manual Build from Source

For full control over the build process:

```sh
git clone https://github.com/thejorgg/taskforce.git
cd taskforce

# Download dependencies
go mod download

# Run tests to verify everything works
go test ./...

# Build the binary
go build -o taskforce ./cmd/taskforce

# Move to your preferred location
mv taskforce /usr/local/bin/taskforce
# or
mv taskforce ~/.local/bin/taskforce
# or any directory in your PATH

# Verify
taskforce version
```

### 4.4 Building a Specific Version

To build a specific Git tag or commit:

```sh
git clone https://github.com/thejorgg/taskforce.git
cd taskforce

# List available tags
git tag -l

# Checkout a specific version
git checkout v0.3

# Build
go build -o taskforce ./cmd/taskforce
```

### 4.5 Installing to a Custom Location

If you want TaskForce somewhere not in your PATH:

**Option A: Direct binary placement**

```sh
go build -o /opt/taskforce/bin/taskforce ./cmd/taskforce
export PATH="/opt/taskforce/bin:$PATH"
```

**Option B: Symlink**

```sh
go build -o taskforce ./cmd/taskforce
sudo mv taskforce /opt/taskforce
sudo ln -s /opt/taskforce/taskforce /usr/local/bin/taskforce
```

**Option C: Using GOBIN**

```sh
GOBIN=/opt/custom-bin go install ./cmd/taskforce
export PATH="/opt/custom-bin:$PATH"
```

---

## 5. Verifying the Installation

Run these commands to confirm TaskForce is correctly installed:

```sh
# Check the version
taskforce version
# Expected output: v0.3

# Check the help
taskforce help
# Should display the full usage text

# Run the doctor check
taskforce doctor
# Should report config, git, harness, and daemon health

# Run the smoke test (no TUI)
taskforce smoke --no-tui
# Should echo signals through the pipeline and print JSON output
```

### 5.1 Doctor Output Explained

`taskforce doctor` checks:

| Check | What it verifies |
| --- | ---|
| **Config** | Config files parse correctly at all levels |
| **Git** | `git` is installed and accessible |
| **Harnesses** | Configured agent binaries are in PATH |
| **Hooks** | Scope hook commands are available |
| **Daemon** | Daemon state directory is accessible |

### 5.2 Smoke Test Output

The smoke test runs an echo pipeline without requiring any AI agent:

```sh
taskforce smoke --config examples/taskforce.json --no-tui
```

Expected: JSON output showing the pipeline stages completing (Echo → Dispatch → Relay → Scope → Exfil) with simulated commands.

---

## 6. PATH Configuration

TaskForce must be in your `PATH` to be invoked as `taskforce`. Ensure the install directory is in your PATH.

### 6.1 Finding Where TaskForce Was Installed

```sh
which taskforce
# Common locations:
# ~/go/bin/taskforce          (go install)
# ~/.local/bin/taskforce      (install.sh default)
# /usr/local/bin/taskforce    (manual or install.sh with INSTALL_DIR)
```

### 6.2 Adding to PATH

If `which taskforce` returns nothing, add the install directory to your PATH:

**For bash:**

```sh
# Add to ~/.bashrc (persists across sessions)
echo 'export PATH="$HOME/go/bin:$HOME/.local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

**For zsh:**

```sh
# Add to ~/.zshrc (persists across sessions)
echo 'export PATH="$HOME/go/bin:$HOME/.local/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

**For fish:**

```fish
fish_add_path $HOME/go/bin
fish_add_path $HOME/.local/bin
```

**For PowerShell:**

```powershell
# Add to profile (persists across sessions)
$env:PATH += ";$env:USERPROFILE\go\bin;$env:USERPROFILE\.local\bin"
# Or add permanently via System Properties → Environment Variables
```

### 6.3 PATH Priority

If you have multiple Go binaries or multiple `taskforce` installs, ensure the correct one comes first in your PATH:

```sh
# Check which taskforce your shell finds
which -a taskforce
# The first result is the one that runs when you type 'taskforce'
```

---

## 7. Initial Configuration

### 7.1 Config Levels and Merge Order

TaskForce uses JSON configuration that merges in this order (later wins):

```text
defaults < profile < project < workspace < --config
```

| Level | Location | Purpose | Shareable? |
| --- | --- | --- | --- |
| **Defaults** | Built into TaskForce | Sensible out-of-box settings | N/A |
| **Profile** | `~/.config/taskforce/config.json` (Linux/macOS) or `%AppData%\taskforce\config.json` (Windows) | OS-user personal settings | No |
| **Project** | `<repo>/taskforce.json` | Repo-shared team config | Yes (commit to git) |
| **Workspace** | `<repo>/.taskforce/config.json` | Local checkout overrides | No (.gitignored) |
| **--config** | Any path via `--config PATH` | One-off command override | No |

### 7.2 Profile Config (OS User)

Profile config applies to all TaskForce invocations for your OS user. Create it:

```sh
taskforce init --level profile
```

This creates `~/.config/taskforce/config.json` with default values. Edit it for your personal preferences:

```sh
taskforce config show --level profile
```

**Example profile config:**

```json
{
  "relay": {
    "control": {
      "agent": "codex",
      "model": "openai/gpt-5"
    },
    "build": {
      "agent": "codex",
      "model": "openai/gpt-5"
    }
  },
  "runtime": {
    "timeout": "45m"
  }
}
```

**Platform-specific profile paths:**

| OS | Path |
| --- | --- |
| Linux | `~/.config/taskforce/config.json` |
| macOS | `~/Library/Application Support/taskforce/config.json` |
| Windows | `%AppData%\taskforce\config.json` |

### 7.3 Project Config (Repo)

Project config is shared across the team. Create it in your repo root:

```sh
taskforce init --level project
```

This creates `taskforce.json` in the current directory. **Commit this file to git.**

**Example project config:**

```json
{
  "relay": {
    "control": { "agent": "codex" },
    "build": { "agent": "opencode" }
  },
  "scope": {
    "hooks": [
      { "name": "tests", "argv": ["go", "test", "./..."] },
      { "name": "lint", "run": "golangci-lint run" }
    ]
  },
  "exfil": {
    "branch": "taskforce/{{task_id}}",
    "commit": true,
    "push": true,
    "pr": true
  }
}
```

### 7.4 Workspace Config (Local)

Workspace config is for local overrides that shouldn't be shared. It lives in `.taskforce/config.json` (gitignored).

```sh
taskforce init --level workspace
```

Or set individual values:

```sh
taskforce config set --level workspace relay.build.agent codex
taskforce config set --level workspace relay.build.model "anthropic/claude-sonnet-4"
```

### 7.5 Explicit Config Override

Pass `--config PATH` to any command to use a specific config file:

```sh
taskforce run --config /tmp/custom-config.json --signal "Fix the bug"
taskforce smoke --config examples/taskforce.json --no-tui
```

---

## 8. Full Configuration Reference

### 8.1 Pipeline Config

```json
{
  "pipeline": {
    "scout": {
      "enabled": false
    }
  }
}
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `pipeline.scout.enabled` | bool | `false` | Enable scout-style repo mapping before dispatch |

### 8.2 Relay Config

```json
{
  "relay": {
    "control": {
      "enabled": true,
      "agent": "codex",
      "model": "",
      "prompt": "Inspect the task and produce an implementation plan.",
      "run": "",
      "argv": [],
      "env": {},
      "work_dir": "",
      "timeout": "",
      "mutates": false
    },
    "build": {
      "enabled": true,
      "agent": "opencode",
      "model": "",
      "prompt": "Implement the approved plan and report changed files.",
      "run": "",
      "argv": [],
      "env": {},
      "work_dir": "",
      "timeout": "",
      "mutates": true
    },
    "retries": 1
  }
}
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `relay.control.agent` | string | `"codex"` | Harness adapter for the planning stage |
| `relay.control.model` | string | `""` | Model override for the plan harness |
| `relay.control.prompt` | string | (see above) | System prompt sent to the plan agent |
| `relay.control.run` | string | `""` | Direct shell command override (bypasses agent) |
| `relay.control.argv` | []string | `[]` | Direct argv override (bypasses agent) |
| `relay.control.env` | map | `{}` | Extra environment variables for the command |
| `relay.control.work_dir` | string | `""` | Working directory override |
| `relay.control.timeout` | string | `""` | Command timeout override |
| `relay.control.mutates` | bool | `false` | Whether this stage modifies the workspace |
| `relay.build.agent` | string | `"opencode"` | Harness adapter for the build stage |
| `relay.build.*` | — | — | Same fields as `relay.control` |
| `relay.retries` | int | `1` | Number of retry attempts when Scope rejects |

### 8.3 Rescue Config

```json
{
  "rescue": {
    "enabled": true,
    "agent": "codex",
    "model": "",
    "root": "/tmp/.relay-rescue",
    "max_attempts": 3,
    "triggers": [
      "safety", "security policy", "refuse", "refusal",
      "disallowed", "military", "weapon", "combat", "conflict"
    ],
    "mappings": {
      "TaskForce": "TFNeutralProject",
      "taskforce": "tfneutralproject",
      "command center": "TFNeutralConsole",
      "Dispatch": "TFNeutralIntake",
      "dispatch": "tfneutralintake",
      "Relay": "TFNeutralFlow",
      "relay": "tfneutralflow",
      "Control": "TFNeutralPlan",
      "control": "tfneutralplan",
      "Scope": "TFNeutralCheck",
      "scope": "tfneutralcheck",
      "Exfil": "TFNeutralHandoff",
      "exfil": "tfneutralhandoff",
      "spy views": "TFNeutralDetailViews",
      "Scout-style": "TFNeutralRepoMap",
      "operator": "TFNeutralUser",
      "target directory": "TFNeutralActiveDir"
    }
  }
}
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `rescue.enabled` | bool | `true` | Enable rescue protocol |
| `rescue.agent` | string | `"codex"` | Harness used for rescue attempts |
| `rescue.model` | string | `""` | Model override for rescue agent |
| `rescue.root` | string | `"/tmp/.relay-rescue"` | Where rescue copies are created |
| `rescue.max_attempts` | int | `3` | Maximum rescue retries |
| `rescue.triggers` | []string | (see above) | Text patterns that trigger rescue |
| `rescue.mappings` | map | (see above) | Terms rewritten in the rescue copy |

### 8.4 Scope Config

```json
{
  "scope": {
    "hooks": []
  }
}
```

Scope hooks are intentionally empty after init. Add repo-appropriate checks.

**Example — Go project:**

```json
{
  "scope": {
    "hooks": [
      { "name": "tests", "argv": ["go", "test", "./..."] },
      { "name": "vet", "argv": ["go", "vet", "./..."] },
      { "name": "lint", "run": "golangci-lint run --timeout 5m" }
    ]
  }
}
```

**Example — Node.js project:**

```json
{
  "scope": {
    "hooks": [
      { "name": "tests", "argv": ["npm", "test"] },
      { "name": "lint", "argv": ["npx", "eslint", "."] },
      { "name": "typecheck", "argv": ["npx", "tsc", "--noEmit"] }
    ]
  }
}
```

### 8.5 Exfil Config

```json
{
  "exfil": {
    "branch": "taskforce/{{task_id}}",
    "commit": true,
    "commit_message": "TaskForce: {{task_title}}",
    "push": false,
    "pr": false,
    "pr_title": "{{task_title}}",
    "pr_body": "Automated TaskForce handoff for {{task_id}}.",
    "hooks": []
  }
}
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `exfil.branch` | string | `"taskforce/{{task_id}}"` | Branch name template |
| `exfil.commit` | bool | `true` | Auto-commit changes |
| `exfil.commit_message` | string | `"TaskForce: {{task_title}}"` | Commit message template |
| `exfil.push` | bool | `false` | Auto-push to remote |
| `exfil.pr` | bool | `false` | Auto-create PR via `gh` |
| `exfil.pr_title` | string | `"{{task_title}}"` | PR title template |
| `exfil.pr_body` | string | (see above) | PR body template |
| `exfil.hooks` | []HookConfig | `[]` | Post-release hooks |

### 8.6 Runtime Config

```json
{
  "runtime": {
    "shell": "",
    "env": {},
    "timeout": "30m"
  }
}
```

| Field | Type | Default | Description |
| --- | ---| --- | --- |
| `runtime.shell` | string | `""` | Shell used for `run` commands (empty = system default) |
| `runtime.env` | map | `{}` | Global environment variables for all commands |
| `runtime.timeout` | string | `"30m"` | Default command timeout |

### 8.7 Agents (Custom Harnesses)

Define custom agents that relay stages can reference by name:

```json
{
  "agents": {
    "mytool": {
      "run": "mytool execute --prompt {{prompt}}",
      "argv": [],
      "plan": {
        "run": "mytool plan --prompt {{prompt}}",
        "argv": []
      },
      "build": {
        "run": "mytool apply --prompt {{prompt}} --model {{model}}",
        "argv": []
      },
      "env": {
        "MYTOOL_REPO": "{{repo}}"
      },
      "timeout": "20m"
    }
  }
}
```

| Field | Type | Description |
| ---| ---| ---|
| `agents.<name>.run` | string | Default command (used for both plan and build unless overridden) |
| `agents.<name>.argv` | []string | Default argv (used for both plan and build unless overridden) |
| `agents.<name>.plan.run` | string | Plan-mode command override |
| `agents.<name>.plan.argv` | []string | Plan-mode argv override |
| `agents.<name>.build.run` | string | Build-mode command override |
| `agents.<name>.build.argv` | []string | Build-mode argv override |
| `agents.<name>.env` | map | Extra environment variables |
| `agents.<name>.timeout` | string | Command timeout |

**Resolution priority for custom agents:**
1. Mode-specific command (plan/build) if defined
2. Default `run`/`argv` if defined
3. Error if neither is set

### 8.8 Hook Config

Used in scope hooks and exfil hooks:

```json
{
  "name": "tests",
  "run": "make test",
  "argv": ["go", "test", "./..."],
  "env": { "CGO_ENABLED": "1" },
  "work_dir": "packages/backend",
  "timeout": "10m",
  "required": true
}
```

| Field | Type | Default | Description |
| --- | ---| --- | --- |
| `name` | string | (required) | Hook identifier |
| `run` | string | `""` | Shell command string |
| `argv` | []string | `[]` | Direct argv (bypasses shell parsing) |
| `env` | map | `{}` | Extra environment variables |
| `work_dir` | string | `""` | Working directory override (supports `{{repo}}`) |
| `timeout` | string | `""` | Command timeout override |
| `required` | bool | `false` | If true, hook failure blocks the pipeline |

### 8.9 Stage Config

Shared structure used by relay control, relay build, and other stages:

| Field | Type | Description |
| --- | ---| --- |
| `enabled` | bool | Whether this stage runs |
| `agent` | string | Harness adapter name |
| `model` | string | Model override |
| `prompt` | string | System prompt |
| `run` | string | Direct shell command |
| `argv` | []string | Direct argv |
| `env` | map | Extra environment variables |
| `work_dir` | string | Working directory override |
| `timeout` | string | Command timeout |
| `mutates` | bool | Whether this stage modifies the workspace |

### 8.10 Placeholder Expansion

Placeholders in `run`, `argv`, and `env` values are expanded at runtime:

| Placeholder | Value |
| --- | ---|
| `{{prompt}}` | The rendered prompt for this stage |
| `{{model}}` | The configured model for this stage |
| `{{task_id}}` | Unique task identifier |
| `{{task_title}}` | Task title |
| `{{task_description}}` | Full task description |
| `{{repo}}` | Absolute path to the active repository |
| `{{mode}}` | `"plan"` or `"build"` |

**Example:**

```json
{
  "agents": {
    "mytool": {
      "argv": ["mytool", "run", "--repo", "{{repo}}", "--task", "{{task_id}}", "--prompt", "{{prompt}}"],
      "env": {
        "MYTOOL_MODEL": "{{model}}",
        "MYTOOL_WORKDIR": "{{repo}}/src"
      }
    }
  }
}
```

### 8.11 Environment Variables

TaskForce exports these environment variables to every harness command:

| Variable | Value |
| --- | ---|
| `TASKFORCE_TASK_ID` | Unique task identifier |
| `TASKFORCE_TASK_TITLE` | Task title |
| `TASKFORCE_TASK_DESCRIPTION` | Full task description |
| `TASKFORCE_STAGE` | Current pipeline stage name |
| `TASKFORCE_MODE` | `"plan"` or `"build"` |
| `TASKFORCE_PROMPT` | Rendered prompt |
| `TASKFORCE_MODEL` | Configured model |
| `TASKFORCE_REPO` | Absolute path to the active repository |

---

## 9. Installing Agent Harnesses

TaskForce orchestrates AI coding agents. Install at least one to use the full pipeline. Each agent has two modes: **plan** (read-only) and **build** (workspace-write).

### 9.1 Codex

Codex is the default planning agent and a common build agent.

```sh
# Install via npm (OpenAI Codex CLI)
npm install -g @openai/codex

# Verify
codex --version
```

**Codex modes in TaskForce:**
- **Plan**: `codex exec --skip-git-repo-check --sandbox read-only <prompt>`
- **Build**: `codex exec --skip-git-repo-check --sandbox workspace-write <prompt>`

**Configuration:**

```json
{
  "relay": {
    "control": { "agent": "codex", "model": "o3" },
    "build": { "agent": "codex", "model": "o3" }
  }
}
```

### 9.2 Claude

Anthropic's Claude CLI.

```sh
# Install via npm
npm install -g @anthropic-ai/claude-code

# Verify
claude --version
```

**Claude modes in TaskForce:**
- **Plan**: `claude -p <prompt> --permission-mode plan --output-format text`
- **Build**: `claude -p <prompt> --permission-mode acceptEdits --output-format text`

**Configuration:**

```json
{
  "relay": {
    "control": { "agent": "claude", "model": "claude-sonnet-4-20250514" },
    "build": { "agent": "claude", "model": "claude-sonnet-4-20250514" }
  }
}
```

### 9.3 OpenCode

OpenCode is the default build agent.

```sh
# Install (check OpenCode documentation for your platform)
# Typically installed via npm, go install, or direct binary download

# Verify
opencode --version
```

**OpenCode modes in TaskForce:**
- **Plan**: `opencode run <prompt>`
- **Build**: `opencode run <prompt>`

**Configuration:**

```json
{
  "relay": {
    "control": { "agent": "opencode" },
    "build": { "agent": "opencode", "model": "anthropic/claude-sonnet-4" }
  }
}
```

### 9.4 Gemini

Google's Gemini CLI.

```sh
# Install via npm
npm install -g @anthropic-ai/claude-code  # or gemini-specific package

# Verify
gemini --version
```

**Gemini modes in TaskForce:**
- **Plan**: `gemini -p <prompt>`
- **Build**: `gemini --yolo -p <prompt>`

**Configuration:**

```json
{
  "relay": {
    "control": { "agent": "gemini" },
    "build": { "agent": "gemini" }
  }
}
```

### 9.5 Mimo

Mimo agent with full permissions.

```sh
# Install (check Mimo documentation)
# Typically installed via npm or direct download

# Verify
mimo --version
```

**Mimo modes in TaskForce:**
- **Plan**: `mimo run --dangerously-skip-permissions <prompt>`
- **Build**: `mimo run --dangerously-skip-permissions <prompt>`

**Configuration:**

```json
{
  "relay": {
    "control": { "agent": "mimo" },
    "build": { "agent": "mimo" }
  }
}
```

### 9.6 Custom Agents

Any command-line tool can be a TaskForce agent. Define it in the `agents` config:

```json
{
  "agents": {
    "aider": {
      "plan": {
        "argv": ["aider", "--message", "{{prompt}}", "--no-git"]
      },
      "build": {
        "argv": ["aider", "--message", "{{prompt}}"]
      },
      "timeout": "30m"
    },
    "cursor": {
      "argv": ["cursor", "apply", "--prompt", "{{prompt}}"],
      "env": { "CURSOR_REPO": "{{repo}}" }
    }
  }
}
```

Then reference the custom agent in relay config:

```json
{
  "relay": {
    "control": { "agent": "aider" },
    "build": { "agent": "cursor" }
  }
}
```

---

## 10. Daemon Setup

TaskForce uses a local daemon process to manage pipeline execution, job queues, run state, and streamed output.

### 10.1 How the Daemon Works

The daemon is a background process that:
1. Runs under `.taskforce/` in the active repository
2. Processes queued jobs from `.taskforce/queue/`
3. Persists run state to `.taskforce/runs/<id>.json`
4. Streams stdout/stderr to `.taskforce/runs/<id>.jsonl`
5. Updates a heartbeat every 200ms in `.taskforce/daemon.json`
6. Handles approval gates via `.taskforce/approvals/<id>.json`

The daemon is automatically started when you run `taskforce` (opens TUI), `taskforce run`, or `taskforce daemon start`.

### 10.2 Starting the Daemon

```sh
# Start the daemon for the current repo
taskforce daemon start

# Start for a specific repo
taskforce daemon start --repo /path/to/your/repo
```

Output:
```
local daemon running · pid 12345 · heartbeat 2026-06-11T12:00:00Z
```

### 10.3 Stopping the Daemon

```sh
taskforce daemon stop
```

This sends SIGTERM to the daemon process, waits up to 5 seconds for graceful shutdown, then kills it if necessary.

### 10.4 Checking Daemon Status

```sh
taskforce daemon status
```

Output if running:
```
local daemon running · pid 12345 · heartbeat 2026-06-11T12:00:00Z
```

Output if stopped:
```
local daemon stopped
```

### 10.5 Daemon State Directory

The daemon creates `.taskforce/` under the active repo with this structure:

```text
.taskforce/
├── daemon.json          # Daemon state: PID, status, heartbeat
├── daemon.log           # Daemon log output
├── config.json          # Workspace-level config overrides
├── queue/               # Pending job files (claimed by daemon)
├── jobs/                # Active/completed job records
│   ├── <job-id>.json    # Job state and result
│   └── <job-id>.jsonl   # Streamed stdout/stderr events
├── runs/                # Pipeline run records
│   ├── <run-id>.json    # Run state and stage progress
│   └── <run-id>.jsonl   # Streamed output events
├── approvals/           # Release gate decision files
│   └── <run-id>.json    # approve/deny decision
└── runqueue/            # Pending run files
    └── <run-id>.json    # Queued run record
```

**Note:** `.taskforce/` is gitignored. It contains local state only.

---

## 11. Workspace Switching

TaskForce tracks the active repository and resumes it automatically.

### 11.1 Switching to a Repository

```sh
taskforce switch /path/to/your/repo
```

This:
1. Resolves the path to the repo root (walks up looking for `.git` or `taskforce.json`)
2. Persists the active repo in `~/.config/taskforce/state.json`
3. When you next run `taskforce` (no arguments), it opens the dashboard for that repo

### 11.2 Switching from the TUI

From the dashboard command input:

```text
switch /path/to/other/repo
cd /path/to/other/repo
```

This resets all run state, starts the new repo's daemon, and persists the selection.

### 11.3 Current Active Repo

```sh
# Check via state file
cat ~/.config/taskforce/state.json
```

Output:
```json
{
  "active_repo": "/home/user/projects/myapp"
}
```

---

## 12. Running Your First Pipeline

### 12.1 Smoke Test (No Agent Required)

Verify wiring without any AI agent:

```sh
taskforce smoke --no-tui
```

This runs echo commands through the full pipeline and prints JSON output.

### 12.2 Single Run (Detached)

Submit a task and print the run ID:

```sh
taskforce run --signal "Fix the broken login button" --detach
```

Then monitor it:

```sh
taskforce runs                    # List all runs
taskforce logs <run-id> --follow  # Stream output
```

### 12.3 Single Run (With TUI)

Open the dashboard and type a task:

```sh
taskforce
# Type: Fix the broken login button
# Press Enter
```

### 12.4 Local Run (No Daemon)

Run in-process without the daemon:

```sh
taskforce run --signal "Fix the broken login button" --local --no-tui
```

### 12.5 Auto-Approve Release

Skip the release gate approval:

```sh
taskforce run --signal "Fix the broken login button" --yes
# or
taskforce run --signal "Fix the broken login button" --yolo
```

### 12.6 Run from a File

```sh
echo "Fix the broken login button" > /tmp/signal.txt
taskforce run --signal-file /tmp/signal.txt
```

---

## 13. Scope Hooks Setup

Scope hooks validate pipeline output before release. Configure them for your project's language and toolchain.

### 13.1 Go Project

```json
{
  "scope": {
    "hooks": [
      { "name": "tests", "argv": ["go", "test", "./..."], "timeout": "10m" },
      { "name": "vet", "argv": ["go", "vet", "./..."] },
      { "name": "lint", "run": "golangci-lint run --timeout 5m" }
    ]
  }
}
```

### 13.2 Node.js Project

```json
{
  "scope": {
    "hooks": [
      { "name": "tests", "argv": ["npm", "test"], "timeout": "10m" },
      { "name": "lint", "argv": ["npx", "eslint", "."] },
      { "name": "typecheck", "argv": ["npx", "tsc", "--noEmit"] }
    ]
  }
}
```

### 13.3 Python Project

```json
{
  "scope": {
    "hooks": [
      { "name": "tests", "argv": ["python", "-m", "pytest", "-v"], "timeout": "10m" },
      { "name": "lint", "argv": ["ruff", "check", "."] },
      { "name": "typecheck", "argv": ["mypy", "."] }
    ]
  }
}
```

### 13.4 Monorepo with Subdirectories

Use `work_dir` to run hooks in specific subdirectories:

```json
{
  "scope": {
    "hooks": [
      { "name": "backend-tests", "argv": ["go", "test", "./..."], "work_dir": "backend" },
      { "name": "frontend-tests", "argv": ["npm", "test"], "work_dir": "packages/frontend" },
      { "name": "e2e", "argv": ["npm", "run", "test:e2e"], "work_dir": "tests/e2e" }
    ]
  }
}
```

### 13.5 Required vs Optional Hooks

By default, hooks are required (failure blocks the pipeline). Make a hook optional:

```json
{
  "name": "coverage",
  "argv": ["go", "test", "-cover", "./..."],
  "required": false
}
```

---

## 14. Exfil Release Configuration

Exfil runs after Scope approval. Configure what happens with the completed work.

### 14.1 Commit Only

```json
{
  "exfil": {
    "branch": "taskforce/{{task_id}}",
    "commit": true,
    "push": false,
    "pr": false
  }
}
```

### 14.2 Commit and Push

```json
{
  "exfil": {
    "branch": "taskforce/{{task_id}}",
    "commit": true,
    "push": true,
    "pr": false
  }
}
```

### 14.3 Full PR Workflow

```json
{
  "exfil": {
    "branch": "taskforce/{{task_id}}",
    "commit": true,
    "push": true,
    "pr": true,
    "commit_message": "TaskForce: {{task_title}}",
    "pr_title": "{{task_title}}",
    "pr_body": "Automated TaskForce handoff for {{task_id}}."
  }
}
```

Requires `gh` CLI to be installed and authenticated:

```sh
gh auth status
```

### 14.4 Post-Release Hooks

Run custom commands after exfil completes:

```json
{
  "exfil": {
    "hooks": [
      {
        "name": "notify",
        "run": "echo 'release complete' | mail -s 'TaskForce Release' team@example.com",
        "work_dir": "{{repo}}"
      },
      {
        "name": "deploy",
        "argv": ["./scripts/deploy.sh"],
        "timeout": "15m",
        "required": false
      }
    ]
  }
}
```

---

## 15. Rescue Protocol Configuration

The rescue protocol handles agent refusals by rerunning the failed step in a neutralized copy of the repo.

### 15.1 How Rescue Works

1. A relay command fails with text matching one of `rescue.triggers`
2. TaskForce copies the repo to `rescue.root/<repo-name>/`
3. Conflict-prone terms (from `rescue.mappings`) are rewritten with neutral tokens
4. The rescue agent reruns the failed stage in the neutralized copy
5. Successful edits are mapped back to the real checkout
6. This repeats up to `rescue.max_attempts` times

### 15.2 Default Rescue Config

```json
{
  "rescue": {
    "enabled": true,
    "agent": "codex",
    "root": "/tmp/.relay-rescue",
    "max_attempts": 3,
    "triggers": [
      "safety", "security policy", "refuse", "refusal",
      "disallowed", "military", "weapon", "combat", "conflict"
    ],
    "mappings": {
      "TaskForce": "TFNeutralProject",
      "taskforce": "tfneutralproject",
      "Exfil": "TFNeutralHandoff",
      "exfil": "tfneutralhandoff"
    }
  }
}
```

### 15.3 Customizing Triggers

Add or remove trigger patterns:

```json
{
  "rescue": {
    "triggers": [
      "safety", "security policy", "refuse",
      "cannot comply", "not authorized", "against policy"
    ]
  }
}
```

### 15.4 Customizing Mappings

Add project-specific term rewrites:

```json
{
  "rescue": {
    "mappings": {
      "TaskForce": "TFNeutralProject",
      "MySecretFeature": "NeutralFeature",
      "internal-api-key": "neutral-key-placeholder"
    }
  }
}
```

### 15.5 Disabling Rescue

```json
{
  "rescue": {
    "enabled": false
  }
}
```

---

## 16. TUI Dashboard

The terminal UI provides live pipeline monitoring and control.

### 16.1 Opening the Dashboard

```sh
taskforce
```

Opens the dashboard for the active repo (or current directory).

### 16.2 Key Bindings

| Key | Action |
| --- | --- |
| **Enter** | Dispatch the typed task |
| **Ctrl+D** | Open Dispatch spy view |
| **Ctrl+R** | Open Relay spy view |
| **Ctrl+S** | Open Scope spy view |
| **Ctrl+E** | Open Exfil spy view |
| **Ctrl+O** | Open Run History view |
| **Ctrl+P** | Open Settings view |
| **Ctrl+A** | Approve release gate |
| **Ctrl+Z** | Deny release gate |
| **Tab** | Cycle through views |
| **Esc** | Return to live feed |

### 16.3 Mouse Support

- Stage cards, legend entries, approval buttons, and run rows are clickable
- Mouse wheel scrolls the spy view

### 16.4 Responsive Layout

- Full layout: 120+ columns — all elements visible
- Compressed layout: 80 columns — cards compress, legend wraps
- Minimal layout: Spy view shrinks to 6 lines, legend becomes a grid

### 16.5 Settings from TUI

From the settings view (`Ctrl+P`):

```text
set workspace relay.build.agent codex
set profile relay.build.model openai/gpt-5
set workspace relay.build.argv ["opencode","run","{{prompt}}"]
unset workspace relay.build.argv
```

---

## 17. Troubleshooting

### 17.1 `taskforce: command not found`

TaskForce binary is not in your PATH.

```sh
# Find it
find ~ -name "taskforce" -type f 2>/dev/null

# Add to PATH (adjust path as needed)
export PATH="$HOME/go/bin:$PATH"
```

### 17.2 `go: command not found`

Go is not installed or not in PATH.

```sh
# Check if Go exists
ls /usr/local/go/bin/go

# Add to PATH
export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"
```

### 17.3 `taskforce doctor` Reports Missing Harnesses

Install the required agent binary (see [Section 9](#9-installing-agent-harnesses)):

```sh
# Example: install codex
npm install -g @openai/codex
```

### 17.4 Daemon Won't Start

Check if another daemon is running:

```sh
taskforce daemon status
```

If stale (wrong PID):

```sh
taskforce daemon stop
taskforce daemon start
```

If `.taskforce/daemon.json` is corrupted:

```sh
rm -rf .taskforce/daemon.json
taskforce daemon start
```

### 17.5 `config validation` Errors

Validate your config:

```sh
taskforce config check
```

Common issues:
- Empty agent name in `agents` config
- Agent not found: name not in built-in list and not defined in `agents`
- `rescue.agent` not set when rescue is enabled
- Empty mapping keys in rescue config

### 17.6 Pipeline Hangs

Check daemon logs:

```sh
cat .taskforce/daemon.log
```

Check run status:

```sh
taskforce runs show <run-id>
```

Kill stuck processes:

```sh
taskforce daemon stop
```

### 17.7 Approval Gate Stuck

A run paused at `awaiting_approval`:

```sh
# List runs to find the paused one
taskforce runs

# Approve or deny
taskforce approve <run-id>
taskforce deny <run-id> --reason "Tests failing"
```

### 17.8 Scope Hooks Fail

Check hook output:

```sh
taskforce logs <run-id> --follow
```

Verify hook commands exist:

```sh
which go
which npm
which pytest
```

### 17.9 Rescue Loop

If rescue keeps retrying and failing:

```sh
# Check rescue logs
cat /tmp/.relay-rescue/<repo-name>/.relay-rescue.log

# Disable rescue
taskforce config set --level workspace rescue.enabled false
```

### 17.10 Permission Denied on Install

```sh
# Use a user-writable location
INSTALL_DIR=~/.local/bin ./install.sh

# Or use go install (no sudo needed)
go install ./cmd/taskforce
```

---

## 18. Upgrading TaskForce

### 18.1 Go Install Method

```sh
go install github.com/thejorgg/taskforce/cmd/taskforce@latest
```

### 18.2 Git + Build Method

```sh
cd /path/to/taskforce
git pull origin main
go build -o taskforce ./cmd/taskforce
# Binary is updated in place if you built there, or:
go install ./cmd/taskforce
```

### 18.3 Install Script Method

```sh
cd /path/to/taskforce
git pull origin main
./install.sh
```

### 18.4 Verify Upgrade

```sh
taskforce version
# Should show the new version
```

---

## 19. Uninstalling TaskForce

### 19.1 Remove the Binary

```sh
# Find it
which taskforce

# Remove it
rm $(which taskforce)
```

Common locations:
- `~/go/bin/taskforce`
- `~/.local/bin/taskforce`
- `/usr/local/bin/taskforce`

### 19.2 Remove State (Optional)

```sh
# Remove workspace state for a specific repo
rm -rf /path/to/your/repo/.taskforce

# Remove global state
rm -rf ~/.config/taskforce
```

### 19.3 Remove Go Module Cache (Optional)

```sh
go clean -modcache
```

---

## 20. Platform-Specific Notes

### 20.1 Linux

- Default profile path: `~/.config/taskforce/config.json`
- Daemon uses POSIX signals (SIGTERM, SIGINT)
- Install script uses `/bin/sh` (POSIX-compatible)
- Recommended: use `~/.local/bin` for user installs

### 20.2 macOS

- Default profile path: `~/Library/Application Support/taskforce/config.json`
- On Apple Silicon, ensure Go is the ARM64 build for best performance
- Gatekeeper may block unsigned binaries — use `go install` or codesign the binary
- Install via Homebrew for Go: `brew install go`

### 20.3 Windows (WSL2)

- Runs natively in WSL2 — use Linux instructions
- Profile path follows Linux conventions inside WSL
- `.taskforce/` state is on the WSL filesystem
- Access from Windows via `\\wsl$\` if needed

### 20.4 Windows (Native / PowerShell)

- Default profile path: `%AppData%\taskforce\config.json`
- Use PowerShell or Git Bash for commands
- Daemon uses Windows process management
- Install via MSI installer or `go install`
- Paths use backslashes in config; JSON uses forward slashes

### 20.5 Docker

TaskForce can run inside a Docker container for isolated environments:

```dockerfile
FROM golang:1.24.2

RUN go install github.com/thejorgg/taskforce/cmd/taskforce@latest

WORKDIR /workspace
ENTRYPOINT ["taskforce"]
```

Build and run:

```sh
docker build -t taskforce .
docker run -it -v $(pwd):/workspace taskforce smoke --no-tui
```

### 20.6 CI/CD Integration

Use TaskForce in CI for automated pipeline runs:

```yaml
# GitHub Actions example
- name: Install TaskForce
  run: go install ./cmd/taskforce

- name: Run pipeline
  run: taskforce run --signal "Run test suite" --yes --no-tui --local
```

---

## 21. Security Considerations

### 21.1 Secrets in Config

**Never commit real secrets** in `taskforce.json` or examples. Use environment variables:

```json
{
  "runtime": {
    "env": {
      "MY_API_KEY": "{{env:MY_API_KEY}}"
    }
  }
}
```

Or set environment variables in your shell:

```sh
export MY_API_KEY="your-secret-key"
taskforce run --signal "Use the API" --no-tui
```

### 21.2 Destructive Commands

TaskForce gates destructive operations behind explicit approval:

- `--yes` / `--yolo` skip the release gate
- Exfil commands (commit, push, PR) pause as `awaiting_approval` unless flags are set
- The TUI shows approval buttons for paused runs

### 21.3 Daemon Security

- The daemon runs as your user — it has your filesystem permissions
- `.taskforce/` is local state — don't expose it over networks
- Daemon logs may contain command output — review before sharing

### 21.4 Rescue Protocol Safety

- Rescue copies are created in `/tmp/` (default) — ensure this is on a local filesystem
- Rescue rewrites conflict-prone terms to prevent agent refusals
- The mapping is transparent — check `rescue.mappings` to understand what's rewritten

### 21.5 Agent Permissions

Built-in agents have different permission levels:

| Agent | Plan Mode | Build Mode |
| --- | --- | --- |
| Codex | Read-only sandbox | Workspace-write sandbox |
| Claude | Permission mode: plan | Permission mode: acceptEdits |
| Gemini | Standard | --yolo (full access) |
| Mimo | --dangerously-skip-permissions | --dangerously-skip-permissions |

Choose agents appropriate for your trust model.

---

## 22. Frequently Asked Questions

### Q: Do I need all five agent types installed?

No. You need at least one agent for the Control (plan) stage and one for the Build stage. They can be the same agent.

### Q: Can I use TaskForce without any AI agent?

Yes. Use `taskforce smoke --no-tui` to test wiring, or set `run`/`argv` directly on relay stages to use any command.

### Q: What happens if the daemon dies mid-run?

The next daemon start marks orphaned runs as failed with `interrupted: daemon stopped`. The run's state and logs are preserved in `.taskforce/runs/`.

### Q: Can multiple terminals watch the same dashboard?

Yes. The TUI is a pure observer — it polls daemon state from disk. Multiple terminals can watch the same runs simultaneously.

### Q: How do I run TaskForce for a different repo?

```sh
taskforce switch /path/to/other/repo
taskforce  # Opens dashboard for that repo
```

### Q: Can I use TaskForce in a monorepo?

Yes. Use `work_dir` on hooks and stages to target specific subdirectories:

```json
{
  "scope": {
    "hooks": [
      { "name": "backend", "argv": ["go", "test", "./..."], "work_dir": "backend" },
      { "name": "frontend", "argv": ["npm", "test"], "work_dir": "frontend" }
    ]
  }
}
```

### Q: How do I check what config is active?

```sh
taskforce config show --level effective
```

### Q: Can I override config for a single command?

```sh
taskforce run --config /tmp/override.json --signal "Fix the bug"
```

### Q: Where are run logs stored?

In `.taskforce/runs/<run-id>.jsonl` — one JSON object per line, containing streamed stdout/stderr events.

### Q: How do I share my config with the team?

Put it in `taskforce.json` at the repo root and commit it. This is the project-level config.

### Q: Can I use TaskForce with pre-commit hooks?

Yes. Add TaskForce-related checks to your pre-commit config, or use scope hooks that run the same checks.

---

*This document covers TaskForce as of version v0.3. For the latest information, see the [README](README.md) and [docs/modules.md](docs/modules.md).*
