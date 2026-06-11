# Repository Guidelines

## Project Structure & Module Organization

TaskForce is a Go CLI module (`github.com/thejorgg/taskforce`) and should behave as a local AI development command center. The entry point lives in `cmd/taskforce/main.go`. Core packages are under `internal/`: `echo`, `dispatch`, `relay`, `scope`, and `exfil` mirror the pipeline described in `README.md`, while `config`, `harness`, `runner`, `daemon`, `orchestration`, `domain`, and `tui` support configuration levels, pluggable agent harnesses, daemon-owned process execution, data types, and terminal UI. Tests are colocated with their packages as `*_test.go`. Reference docs live in `docs/`, and sample configuration lives in `examples/taskforce.json`.

## Build, Test, and Development Commands

- `go test ./...`: run the full unit test suite.
- `go test -cover ./...`: run tests with package coverage summaries.
- `go run ./cmd/taskforce version`: verify the CLI builds and prints its version.
- `go run ./cmd/taskforce smoke --config examples/taskforce.json --no-tui`: run the smoke pipeline and print JSON output.
- `go install ./cmd/taskforce`: install the local CLI binary.

Use `taskforce init --level project` for shareable repo config, `taskforce init --level profile` for the OS user config, and `taskforce config set --level workspace ...` for local checkout overrides. Generated runtime state such as `.taskforce/`, `coverage.out`, `dist/`, and the built `taskforce` binary should stay untracked.

## Coding Style & Naming Conventions

Format all Go code with `gofmt`; use tabs for indentation as produced by the tool. Keep package names short, lowercase, and aligned with their directory names. Exported identifiers should have clear Go doc comments when they are part of a package API. Prefer small structs and explicit option types, following existing patterns such as `runner.Options` and `orchestration.Options`.

## Testing Guidelines

Use Go's standard `testing` package. Place tests next to the implementation as `internal/<package>/<package>_test.go`, and name test functions `TestBehaviorOrCase`. Add focused tests for config parsing, config-level precedence, daemon job/log behavior, orchestration behavior, runner command handling, and any change that affects mutating stages such as branch, commit, push, or PR creation.

## Commit & Pull Request Guidelines

This repository has no existing commit history yet, so use a simple imperative convention going forward, for example `Add smoke pipeline tests` or `Fix config validation errors`. Keep commits focused and mention affected pipeline stages when helpful. Pull requests should include a short summary, test results such as `go test ./...`, linked issues when applicable, and screenshots or terminal output for TUI-visible changes.

## Security & Configuration Tips

Do not commit real secrets in `taskforce.json` or examples. Prefer environment variables through profile/workspace runtime settings, and keep destructive commands gated behind `--yes`, `--yolo`, or explicit config review. `.taskforce/` is local command-center state: workspace config, daemon heartbeat, queued jobs, and stdout/stderr logs belong there.
