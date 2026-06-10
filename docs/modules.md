# TaskForce Modules

## Echo

Echo accepts raw input from a CLI string or a file and converts it into a normalized signal. A signal records the source, content, artifact paths, creation time, and stable ID.

## Dispatch

Dispatch converts a signal into a task packet. The default implementation classifies basic categories, assigns severity and priority, generates acceptance criteria, and marks empty signals as not actionable.

## Relay

Relay is the execution core. It has two subcomponents:

- **Control** plans work. It can call a configured command or a built-in agent adapter such as Codex or Claude.
- **Build** implements work. It can call a configured command or a built-in agent adapter such as opencode.

Scout-style repo mapping is optional and is configured separately from the canonical five pipeline names.

## Scope

Scope validates Relay output. It can run simple hooks such as `npm run lint` or `go test ./...`, collect output, and return approved, rejected, or needs-revision.

## Exfil

Exfil only runs after Scope approval. It can create/switch a branch, commit, push, and open a PR using `gh` when configured.

## Command Configuration

Most commands should be simple shell strings:

```json
{ "name": "lint", "run": "npm run lint" }
```

For commands that should avoid shell parsing, use argv:

```json
{ "name": "tests", "argv": ["go", "test", "./..."] }
```

