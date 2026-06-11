# TaskForce Modules

## Echo

Echo accepts raw input from a CLI string or a file and converts it into a normalized signal. A signal records the source, content, artifact paths, creation time, and stable ID.

## Dispatch

Dispatch converts a signal into a task packet. The default implementation classifies basic categories, assigns severity and priority, generates acceptance criteria, and marks empty signals as not actionable.

## Relay

Relay is the execution core. It has two subcomponents:

- **Control** plans work in read-only mode (plan harness variant).
- **Build** implements work with workspace-write access (build harness variant).

When Scope rejects a build, Relay retries up to `relay.retries` extra attempts, feeding the failure output back into Control so the next plan can react to it.

Both stages resolve their command through the harness registry (see below). Scout-style repo mapping is optional and is configured separately from the canonical five pipeline names.

When a Relay planning or build command fails with configured safety/refusal text, the rescue protocol can rerun the failed step in a temporary neutralized copy of the repo before Scope starts. The default rescue team uses `codex`, writes the copy under `/tmp/.relay-rescue/<repo-name>`, rewrites configured conflict-prone terms to per-run neutral tokens, retries up to `rescue.max_attempts`, and restores successful edits back into the original checkout through the inverse mapping.

## Scope

Scope validates Relay output. It runs user-configured hooks such as `go test ./...`, `npm test`, or repo-specific review commands, collects output, and returns approved, rejected, or needs-revision. New configs leave Scope hooks empty so init does not guess the repo's language or toolchain.

## Exfil

Exfil only runs after Scope approval. It can create/switch a branch, commit, push, open a PR using `gh`, and run custom release hooks. In daemon-owned runs, every `exfil.*` mutating command pauses the run as `awaiting_approval` until an operator approves or denies it (unless the run was submitted with `--yes`/`--yolo`).

## Harnesses

`internal/harness` resolves which command a relay stage runs, in priority order:

1. Stage-level `run` / `argv` override — the command is used as-is.
2. A custom agent from the `agents` config map, using its `plan` or `build` variant when present, else its default `run`/`argv`.
3. A built-in adapter: `claude`, `codex`, `opencode`, `gemini`, `mimo`. Plan variants run read-only (e.g. codex read-only sandbox, `claude --permission-mode plan`); build variants may edit the workspace (codex workspace-write, `claude --permission-mode acceptEdits`, `gemini --yolo`, `mimo run --dangerously-skip-permissions`).

Defaults when no agent is configured: `codex` for Control, `opencode` for Build.

Placeholders expanded in `run`, `argv`, and `env` values: `{{prompt}}`, `{{model}}`, `{{task_id}}`, `{{task_title}}`, `{{task_description}}`, `{{repo}}`, `{{mode}}`. The resolved command also receives `TASKFORCE_*` environment variables describing the task and stage.

## Command Configuration

TaskForce merges configuration from defaults, the profile config, the project config, the workspace config, and an optional explicit `--config` path. Profile config lives in the OS user config directory under `taskforce/config.json`. Project config is the shareable `taskforce.json`. Workspace config is local state at `.taskforce/config.json`.

Most commands can be simple shell strings:

```json
{ "name": "check", "run": "make check" }
```

For commands that should avoid shell parsing, use argv:

```json
{ "name": "tests", "argv": ["go", "test", "./..."] }
```

Relay stages select a harness with `agent` and an optional `model`:

```json
{ "agent": "opencode", "model": "anthropic/claude-sonnet-4" }
```

Set `run` or `argv` to bypass built-ins and use a custom harness command directly.

## Rescue Protocol

`rescue` config controls the fallback path used when a Relay command fails with one of `rescue.triggers` in stdout, stderr, or the command error. When enabled, TaskForce copies the active repo to `rescue.root`, applies `rescue.mappings` in that copy using materialized collision-checked replacement tokens, reruns the failed Relay stage with `rescue.agent` up to `rescue.max_attempts`, and restores successful edits through the inverse mapping before Scope hooks run.

Starting rescue also records the repo and configured mapping table in `$HOME/.local/taskforce/rescue-state.json`. Future runs for that repo read the stored mappings and run Relay Control/Build in a neutralized temporary copy immediately, without waiting for another safety/refusal failure.

The default rescue config is enabled, uses `codex`, and watches for safety, security-policy, refusal, disallowed-content, military, weapon, combat, and conflict wording. The mapping table can be narrowed or extended in profile, project, or workspace config.

## Daemon, Runs, and Logs

The local daemon owns all pipeline execution. `taskforce run` (and the TUI) submit a run record under `.taskforce/runqueue/`; the daemon claims it, executes the pipeline, and persists progress atomically to `.taskforce/runs/<id>.json` after every stage transition. Streamed stdout/stderr from relay agents, scope hooks, and exfil commands is appended to `.taskforce/runs/<id>.jsonl`.

Approval gates work through `.taskforce/approvals/<id>.json` decision files written by `taskforce approve|deny` or the TUI; the paused run polls for the decision and resumes or skips the gated commands. If the daemon dies mid-run, the next daemon start marks orphaned runs as failed (`interrupted: daemon stopped`).

Single ad-hoc commands still flow through the job queue (`.taskforce/queue/` → `.taskforce/jobs/`), which the dashboard also reads.

## TUI

`internal/tui` renders the dashboard: a header, the five stage cards, a "spy" viewport showing the selected stage view (live feed, dispatch, relay, scope, exfil, runs, settings), an optional release-gate approval bar, the command input, and a key legend. The TUI is a pure observer — it polls daemon files a few times per second and submits runs/decisions back through the daemon, so multiple terminals can watch the same repo.

From the command input, `switch /path/to/repo` or `cd /path/to/repo` switches the dashboard to a different repo. The TUI resolves the path to the repo root, resets all run state, starts the new repo's daemon, and persists the active repo so the next `taskforce` launch opens it automatically.

Layout is responsive: the legend wraps at item boundaries while space allows; on short terminals the spy view shrinks to a 6-line minimum, after which the legend switches to a left-to-right grid of at most 4 lines with equally distributed columns. The same `frame()` geometry drives both rendering and mouse hit-testing, so stage cards, legend items, approval buttons, and run rows are clickable.

## Workspace and Switching

`internal/workspace` provides directory resolution and persisted state:

- `Resolve(path)` makes a path absolute, verifies it is a directory, and walks upward to find the repo root (`.git` or `taskforce.json`).
- `LoadState()` / `SaveState()` manage `~/.config/taskforce/state.json`, which stores the last active `active_repo` path.
- `taskforce switch PATH` resolves and persists the active repo; `taskforce` with no arguments resumes it.

## Working Directories

Hooks and stages accept an optional `work_dir` field. When set, the command runs in that directory instead of the repo root. The `{{repo}}` placeholder is expanded in `work_dir` values. This lets users run hooks or agents in subdirectories while the active TaskForce repo remains stable.

```json
{ "name": "tests", "argv": ["npm", "test"], "work_dir": "packages/frontend" }
```
