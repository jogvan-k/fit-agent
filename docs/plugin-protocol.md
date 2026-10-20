# CLI machine interface for the OpenClaw plugin

The CLI remains the sole owner of Intervals.icu requests, credential resolution,
DSL parsing, rendering and workspace mutation. This interface is opt-in and
backwards-compatible with existing presentation-oriented commands.

## Protocol discovery

`fit-agent integration-info --json` requires no profile, workspace or network.
It returns the CLI version, `protocol: 1` and feature strings. The plugin requires
`doctor-json`, `status-json`, `fetch-json`, `workspace-assertion` and
`guarded-sync-v1` in addition to a stable CLI version >=0.1.0. New CLI behavior
must use an explicit feature or protocol gate before the plugin relies on it.

All protocol results use exactly one JSON object on stdout:

```json
{"schemaVersion":1,"ok":true,"data":{}}
```

or a nonzero exit with:

```json
{"schemaVersion":1,"ok":false,"error":{"layer":"sync conflict","message":"Preview changed; preview again and confirm the matching result."}}
```

Errors omit wrapped HTTP bodies, athlete text, secret-store errors and raw config.
The plugin ignores even schema-valid error prose and maps the allowlisted layer
to fixed remediation. JSON mode suppresses verbose progress output. Partial
fetch/pull failures are errors, not successful approval. See the checked-in
[`plugin/schemas/`](../plugin/schemas/) files for every input/result shape.

## Commands

| Command | Behavior |
|---|---|
| `integration-info --json` | Read-only capability discovery |
| `doctor --json` | Read-only local profile, credential availability, workspace binding and timezone; no network |
| `status --json` | Read-only file recency, authored planned-workout counts/dates and coaching-file presence |
| `fetch --json --since 90d` | Existing cache + render orchestration with content-free statistics |
| `sync-workouts --json --from DATE --to DATE --dry-run` | Guarded remote-read preview, no cache/workspace writes |
| `sync-workouts --json --from DATE --to DATE --expect-preview HASH` | Replan against live inventory and apply only the identical approved plan |

`doctor`, `status` and `integration-info` always emit JSON; `--json` documents the
calling contract explicitly. Other commands retain their default human output.
Use `--profile NAME` when needed. The global `--workspace PATH` asserts that the
selected profile resolves to the same canonical path; it does not redirect or
rewrite profile configuration. `init --workspace` retains its existing meaning.

JSON sync requires 1–90 inclusive calendar dates; there is no default range or
`--since` shorthand. The Go guarded path uses the existing planner/applier/pull
renderer with a live in-memory inventory. It hashes CLI/profile/athlete/workspace
identity, dates, prune policy, all planned markdown bytes, confirmed baselines,
live events and planned actions. A stale hash or conflict causes rejection before
any writes. The plugin additionally requires a recent single-use session token,
user confirmation and another successful matching preview.

## Ownership and conflict baselines

Confirmed JSON sync atomically stores event snapshots at
`fit-agent/.cache/sync-baseline/<event-id>.json`. These files are machine-owned;
`fetch` does not modify them. They contain event data, never credentials, and are
never exposed in plugin output. Their separate lifetime is deliberate: updating
the ordinary downloaded cache must not erase evidence of remote edits.

A remote update/deletion, missing update baseline or duplicate identity requires
manual reconciliation. An explicitly confirmed `unchanged` action adopts an
existing reconciled workout into the baseline. Never edit a baseline or a
machine-managed `fit-agent:icu` block by hand. All ID stamping and block refreshes
go through the existing Go workspace code and atomic write helpers.

The plugin does not expose pruning; its delete counts are normally zero and
remote-only events are skips. The CLI can still preview a guarded `--prune`
operation directly, with that flag bound into its fingerprint. Remote writes
can be partial if transport or local persistence fails; inspect and re-preview
before retrying. There is no whole-calendar remote transaction or lock.
