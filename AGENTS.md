# AGENTS.md — working in the fit-agent repo

Instructions for AI agents (and humans) contributing to `fit-agent`.

## What this repo is

`fit-agent` is a Go CLI that bridges an AI coaching agent and
[intervals.icu](https://intervals.icu). The CLI fetches activity / wellness
data, parses `.fit` files, renders them into agent-friendly YAML, and
pushes planned workouts back to intervals.icu. Read `agent-plan.md` first;
it is the source of truth for design decisions, file formats, and the v1
task checklist.

## Ground rules

- **Read `agent-plan.md` before changing anything design-relevant.** If
  what you are about to do contradicts the plan, update the plan in the
  same change and explain why.
- **The CLI never calls an LLM.** It is invoked *by* an agent. Do not add
  LLM SDKs or prompts to the CLI itself. Coaching prompts live in
  `internal/templates/skills/`.
- **The workspace has owners.** Some files are agent-owned
  (`ATHLETE-PROFILE.md`, `TRAINING-PLAN.md`, `skills/**`,
  `planned-workouts/*.md`); some are machine-owned (`activities/*.yaml`,
  `wellness/*.yaml`, `.cache/**`). Code that writes to the workspace must
  respect the ownership table in `agent-plan.md` §4.
- **Agent-owned files are never overwritten** by `fetch`. If you find
  yourself wanting to, stop — the design is wrong.
- **`.cache/` is the source of truth.** Agent-facing YAML/markdown can
  always be regenerated from `.cache/`. Never delete or rewrite cache
  files outside of explicit cache commands.

## Project layout

See `agent-plan.md` §3. Briefly:

```
cmd/fit-agent/         # cobra entry point
internal/
  config/              # XDG config + keyring
  icu/                 # intervals.icu HTTP client
  fitparse/            # muktihari/fit wrapper
  workspace/           # paths, atomic writes, ownership
  render/              # icu+fit -> YAML/markdown
  workoutdsl/          # fit-workout DSL <-> icu description
  templates/           # go:embed templates for `init`
  cli/                 # one file per command
testdata/              # icu json + fit fixtures + golden output
```

Add new packages under `internal/` unless there is a clear reason to expose
them. We don't have external consumers in v1.

## Commands and their composition

- `fit-agent fetch` is the convenience wrapper. Internally it is
  `cache all` followed by `render all`.
- Atomic subcommands exist for debugging and for the agent:
  `cache`, `render`, `fit`, `workout`, `push-workouts`. See §8 of the plan.
- Every command that mutates state must support `--dry-run`.

## Data formats

| File | Format | Owner |
|---|---|---|
| `activities/YYYY-MM-DD.yaml` | YAML, multi-doc, one per activity | machine |
| `wellness/YYYY-MM.yaml` | YAML, map by date | machine |
| `planned-workouts/YYYY-MM-DD.md` | markdown + ```fit-workout``` fence | shared |
| `ATHLETE-PROFILE.md`, `TRAINING-PLAN.md`, `README.md`, `skills/**/SKILL.md` | markdown | agent |
| `.cache/**` | raw icu JSON + raw FIT bytes | machine |

YAML data files always start with a header comment describing units and
the path to the corresponding cache file. Don't drop the header.

## Build & test

The Makefile is the canonical entry point; everything wraps `go` so it
also works without make.

```sh
make build       # -> bin/fit-agent (with version stamped from `git describe`)
make test        # go test ./...
make check       # fmt + vet + test (run before pushing)
make lint        # golangci-lint run
make tidy        # go mod tidy
make clean       # rm -rf bin
```

Without make:

```sh
go build -o bin/fit-agent ./cmd/fit-agent
go test ./...
go test -race ./...                  # what CI runs
go test -cover ./...                 # quick coverage glance
go test -run TestRateLimit ./internal/icu -v   # focused
./bin/fit-agent --help
```

Coverage target on data-shaping packages (`icu`, `fitparse`, `render`,
`workoutdsl`) is 80% — see the Testing section below.

Golden tests under `internal/render` regenerate with:

```sh
go test ./internal/render -update    # then eyeball the diff before committing
```

CI (`.github/workflows/ci.yml`) runs `go mod tidy -diff`, `go build`,
`go vet`, `go test -race -coverprofile`, and `golangci-lint run` on every
push and PR. Match it locally with `make check && make lint`.

## Coding standards

- Go 1.25+ (whatever the CI matrix pins).
- `gofmt` and `goimports` clean; `golangci-lint run` clean.
- Public APIs documented; doc comments start with the identifier name.
- Errors wrapped with `fmt.Errorf("...: %w", err)`. No `panic` outside
  `main` or test setup.
- Use `context.Context` on anything that does I/O.
- Time: always work in athlete-local TZ from `/athlete/{id}`. Store
  ISO-8601 with offset in YAML. Never assume UTC.
- File writes are atomic: `os.CreateTemp` + `os.Rename`. Use the helper
  in `internal/workspace`.
- Permissions: config file `0600`, workspace files `0644`,
  workspace dirs `0755`. Never weaken.
- Avoid third-party deps unless they replace meaningful work. The plan
  pins the allowed set (§13).

## Testing

- `go test ./...` is the baseline gate.
- Table-driven tests preferred.
- HTTP code uses `httptest.Server`; do not hit real intervals.icu in tests.
- Renderers use golden files: `testdata/icu/*.json` +
  `testdata/fit/*.fit` → `testdata/workspace/*.{yaml,md}`. Update goldens
  with `go test ./internal/render -update`. Inspect the diff before
  committing — golden churn is how regressions hide.
- A real `.fit` sample is expected at `testdata/fit/sample-intervals.fit`
  (see `~/icu/activity.fit`); copy it into `testdata/` rather than
  reading from outside the repo.
- Coverage target on data-shaping packages (`icu`, `fitparse`, `render`,
  `workoutdsl`) is 80%.

## Secrets

- The intervals.icu API key is **never** logged, written to stdout, or
  committed. Tests use a dummy key.
- Default storage is the OS keyring (`zalando/go-keyring`). Fallback to
  `${XDG_CONFIG_HOME}/fit-agent/config.toml` (mode `0600`) is allowed but
  must emit a visible warning.
- The workspace contains no secrets. `.fit-agent.toml` only stores a
  profile name. Tests should fail if a key leaks into the workspace tree.

## Skill templates

- Workspace skill templates live at
  `internal/templates/skills/<name>/SKILL.md` and are copied verbatim by
  `init` into `<workspace>/skills/<name>/`.
- They follow the [OpenClaw skill format](https://docs.openclaw.ai/tools/skills):
  YAML frontmatter with at least `name` and `description`.
- The three v1 skills are `training-plan-coach`, `workout-builder`, and
  `training-session-coach`. See plan §9 for the pipeline they form.
- When updating the `fit-workout` DSL, update both
  `workout-builder/SKILL.md` and `internal/workoutdsl` in the same change.

## Pull request expectations

- One concern per PR. Plan refactors and behavior changes go in separate
  PRs.
- If you check off boxes in `agent-plan.md` §15, do it in the same commit
  that lands the work.
- Update `README.md` and `docs/` when commands, flags, or file formats
  change.
- New external dependencies require a one-line justification in the PR
  description, and an update to plan §13.

## Common workflows for an agent working here

- **Adding a new icu endpoint.** Implement the typed client method in
  `internal/icu`, add an `httptest`-backed test, then surface it via a
  `cache` subcommand if the agent should be able to invoke it directly.
- **Changing YAML output.** Update the renderer in `internal/render`,
  regenerate goldens with `-update`, eyeball the diff,
  update `docs/workspace.md` if user-visible.
- **Extending the workout DSL.** Update tokenizer + parser in
  `internal/workoutdsl`, add round-trip and fixture tests, update
  `internal/templates/skills/workout-builder/SKILL.md`, and bump any
  examples in `docs/`.
- **Debugging a single activity.** `fit-agent cache activity <id>`, then
  `fit-agent fit laps .cache/activities/<id>.fit` and
  `fit-agent render activity <id>` to confirm output.

## Anti-patterns

- Storing secrets in the workspace.
- Writing markdown tables for lap data (use YAML; see plan §10).
- Round-tripping numeric data through prose strings.
- Calling intervals.icu from tests.
- Adding LLM/agent logic to the CLI.
- Editing machine-owned files by hand expecting them to survive `fetch`.
- Bypassing the atomic write helper.
