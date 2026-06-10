# TaskForce

TaskForce is a Go-native AI development automation harness for moving software work through:

```text
Echo -> Dispatch -> Relay -> Scope -> Exfil
```

It is designed to be configurable, inspectable, and usable from a terminal. TaskForce owns orchestration, state, TUI, logs, and release gates. Your existing tools still do the specialized work: Claude, Codex, opencode, `npm run lint`, `go test ./...`, `gh pr create`, or any other command you configure.

## Pipeline

- **Echo** collects raw signals and normalizes them.
- **Dispatch** turns signals into structured task packets.
- **Relay** executes the implementation loop through **Control** and **Build**.
- **Scope** validates output with review hooks and approval rules.
- **Exfil** commits, pushes, opens PRs, or produces a handoff once Scope approves.

## Quick Start

```sh
go install ./cmd/taskforce
taskforce
taskforce init
jq . taskforce.json
taskforce smoke --no-tui
taskforce run --signal "Fix the broken login button" --repo . --no-tui --yes
```

Running `taskforce` with no arguments opens the interactive dashboard. Use `--yolo` only when you want configured mutating stages to run without confirmation.

## Configuration

TaskForce uses JSON because it is easy to inspect with `jq`.

```json
{
  "pipeline": {
    "scout": { "enabled": false }
  },
  "relay": {
    "control": {
      "agent": "codex",
      "prompt": "Inspect the task and produce an implementation plan."
    },
    "build": {
      "agent": "opencode",
      "prompt": "Implement the approved plan and report changed files."
    }
  },
  "scope": {
    "hooks": [
      { "name": "lint", "run": "npm run lint" },
      { "name": "tests", "run": "npm test" }
    ]
  },
  "exfil": {
    "branch": "taskforce/{{task_id}}",
    "commit": true,
    "push": false,
    "pr": false
  }
}
```

Hook commands use the platform shell by default (`/bin/sh -c` on Unix, `cmd /C` on Windows). Advanced commands can use argv form instead.

See [docs/modules.md](docs/modules.md) for module details.
