# TaskForce

TaskForce is a Go-native AI development command center for moving software work through:

```text
Echo -> Dispatch -> Relay -> Scope -> Exfil
```

It is designed to be configurable, inspectable, and usable from a terminal. TaskForce owns orchestration, daemon state, run queues, TUI screens, logs, and release gates. Your existing tools still do the specialized work: Claude, Codex, opencode, Gemini, mimo, `go test ./...`, `npm test`, `gh pr create`, or any other command you configure.

## Pipeline

- **Echo** collects raw signals and normalizes them.
- **Dispatch** turns signals into structured task packets.
- **Relay** executes the implementation loop through **Control** (plan) and **Build** (implement), retrying with feedback when Scope rejects.
- **Scope** validates output with review hooks and approval rules.
- **Exfil** commits, pushes, opens PRs, or produces a handoff once Scope approves — pausing for an operator decision unless `--yes`/`--yolo`.

## Quick Start

```sh
go install ./cmd/taskforce
taskforce                       # open the interactive dashboard (resumes last active repo)
taskforce switch /path/to/repo  # switch the active TaskForce target directory
taskforce init --level project
taskforce config show
taskforce smoke --no-tui        # echo-only pipeline sanity check
taskforce run --signal "Fix the broken login button"
taskforce runs                  # list pipeline runs
taskforce logs <run-id> --follow
taskforce approve <run-id>      # answer a paused release gate
taskforce agents                # built-in + configured harnesses
taskforce doctor                # environment and config checks
```

## CLI

| Command | What it does |
| --- | --- |
| `taskforce` | Opens the dashboard and starts the repo daemon. Resumes the last active repo if `taskforce switch` was used. |
| `taskforce switch PATH` | Switches the active TaskForce target directory, resolves to the repo root, and persists the selection for future dashboard launches. |
| `taskforce run` | Submits a pipeline run to the daemon. `--signal`/`--signal-file` provide the task; `--detach` prints the run ID and exits; `--no-tui` watches in plain text; `--local` runs in-process without the daemon; `--yes`/`--yolo` skip the release gate. |
| `taskforce smoke` | Runs a built-in echo pipeline to verify wiring. |
| `taskforce runs [show <id>] [--json]` | Lists runs (newest first) or shows one run record. |
| `taskforce logs <id> [--follow]` | Streams a run's command output events. |
| `taskforce approve / deny <id> [--reason]` | Answers a run paused at the release gate. |
| `taskforce agents` | Shows built-in adapters and custom `agents` config with sample commands. |
| `taskforce doctor` | Checks config, git, harness binaries, hooks, and daemon health. |
| `taskforce daemon start\|status\|stop\|run` | Manages the repo daemon under `.taskforce/`. |
| `taskforce init / config` | Creates and edits layered config. |

Runs are owned by the daemon: submitting a run queues it under `.taskforce/runqueue/`, progress is persisted to `.taskforce/runs/<id>.json`, and streamed stdout/stderr lands in `.taskforce/runs/<id>.jsonl`. Submitting a run implicitly approves Relay's implementation commands; Exfil release commands pause the run as `awaiting_approval` until `taskforce approve|deny`, the TUI buttons, or `--yes` answer them.

## Dashboard

Running `taskforce` with no arguments opens the dashboard. It polls the daemon a few times per second, so any number of terminals can watch the same runs.

- Type a task and press **enter** to dispatch a pipeline run.
- Type `switch /path/to/repo` or `cd /path/to/repo` and press **enter** to switch the dashboard to a different repo (resets run state and starts the new repo's daemon).
- **ctrl+d / ctrl+r / ctrl+s / ctrl+e** open the dispatch, relay, scope, and exfil spy views; **ctrl+o** opens run history; **ctrl+p** opens settings; **tab** cycles views; **esc** returns to the live feed.
- Settings screen rows are selectable: **↑/↓** navigate, **enter** opens a dropdown for agent and boolean config values, and **esc** closes the dropdown or returns to the feed.
- Artifact file paths from signals and task packets appear in the settings screen; **double-click** a file to open a confirmation modal, then press **enter** to open it with `$VISUAL`/`$EDITOR` (falls back to `xdg-open`, `open`, or `notepad` by platform).
- Stage cards, legend entries, approval buttons, run rows, and settings rows are clickable; the mouse wheel scrolls the spy view.
- When a run pauses at the release gate, an approval bar appears: **ctrl+a** approves, **ctrl+z** denies (also clickable).
- The bottom legend wraps to as many lines as fit; on short terminals the spy view shrinks down to 6 lines, after which the legend switches to an equally distributed left-to-right grid of at most 4 lines.

## Configuration

TaskForce uses JSON because it is easy to inspect with `jq`. Effective config is merged in this order:

```text
defaults < profile < project < workspace < --config
```

- **profile**: OS user config, such as `~/.config/taskforce/config.json` or `%AppData%\taskforce\config.json`.
- **project**: shareable repo config, usually `taskforce.json`.
- **workspace**: local repo config at `.taskforce/config.json`.
- **--config**: optional explicit override path for one command.

```json
{
  "relay": {
    "control": { "agent": "codex", "prompt": "Inspect the task and produce an implementation plan." },
    "build": { "agent": "opencode", "prompt": "Implement the approved plan and report changed files." }
  },
  "rescue": {
    "enabled": true,
    "agent": "codex",
    "root": "/tmp/.relay-rescue",
    "max_attempts": 3
  },
  "scope": {
    "hooks": []
  },
  "exfil": { "branch": "taskforce/{{task_id}}", "commit": true, "push": false, "pr": false }
}
```

Scope hooks are intentionally empty after init. Add repo-appropriate checks when configuring a project, for example `{"name":"tests","argv":["go","test","./..."]}` for Go or `{"name":"tests","argv":["npm","test"]}` for a Node project.

Hooks and stages accept an optional `work_dir` field to override the directory where the command runs:

```json
{ "name": "tests", "argv": ["npm", "test"], "work_dir": "packages/frontend" }
```

## Pluggable Harnesses

Built-in adapters: `claude`, `codex`, `opencode`, `gemini`, and `mimo`. Each has a plan variant (read-only) and a build variant (allowed to edit the workspace). Set `agent` and optionally `model` on `relay.control` / `relay.build` to pick one. The `mimo` adapter runs `mimo run --dangerously-skip-permissions`.

Any other command can be plugged in through the `agents` registry:

```json
{
  "agents": {
    "mytool": {
      "plan":  { "argv": ["mytool", "plan", "--prompt", "{{prompt}}"] },
      "build": { "argv": ["mytool", "apply", "--prompt", "{{prompt}}", "--model", "{{model}}"] },
      "env": { "MYTOOL_REPO": "{{repo}}" },
      "timeout": "20m"
    }
  },
  "relay": { "build": { "agent": "mytool" } }
}
```

Placeholders expanded in `run`, `argv`, and `env`: `{{prompt}}`, `{{model}}`, `{{task_id}}`, `{{task_title}}`, `{{task_description}}`, `{{repo}}`, `{{mode}}`. The harness also exports `TASKFORCE_TASK_ID`, `TASKFORCE_TASK_TITLE`, `TASKFORCE_MODE`, and related environment variables. Setting `run` or `argv` directly on a relay stage bypasses agents entirely and runs your command as-is.

## Rescue Protocol

If a Relay command fails with configured refusal or safety text, the default rescue team runs the failed Relay step again with `codex` inside a temporary copy at `/tmp/.relay-rescue/<repo-name>`. The copy rewrites configured conflict-prone terms to per-run neutral tokens, runs the rescue harness from that neutralized working directory up to `rescue.max_attempts`, then maps successful edits back into the real checkout before Scope hooks run. Replacement tokens are materialized with a run-specific suffix and checked for collisions before the rescue harness starts.

Once rescue starts for a repo, TaskForce stores that repo's configured mappings in `$HOME/.local/taskforce/rescue-state.json`. Later Relay runs for the same repo read that state and run Control/Build in a mapped temporary copy from the start, even before another refusal happens.

From the dashboard settings screen (`ctrl+p`), use arrow keys and enter to interact with dropdowns for agent and boolean config values, or type commands such as:

```text
set workspace relay.build.agent codex
set profile relay.build.model openai/gpt-5
set workspace relay.build.argv ["opencode","run","{{prompt}}"]
unset workspace relay.build.argv
```

See [docs/modules.md](docs/modules.md) for module details and [examples/taskforce.json](examples/taskforce.json) for a full sample.
