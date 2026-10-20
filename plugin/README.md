# FitAgent for OpenClaw

`@jogvan-k/fit-agent` exposes six typed tools backed by your locally installed
`fit-agent` Go CLI. Runtime plugin ID: **`fit-agent`**. Catalog category:
**Scheduling**. Source: [jogvan-k/fit-agent](https://github.com/jogvan-k/fit-agent),
package directory `plugin/`. The CLI owns all Intervals.icu requests, credentials,
workout parsing, rendering, and workspace writes.

## Requirements and installation

- macOS or Linux, Intel/AMD64 or ARM64. Windows is not supported by plugin v1
  because protected setup uses a local controlling terminal (`/dev/tty`).
- Node **24.16+ within Node 24**, or **26.1+**.
- OpenClaw **2026.10.5**, including plugin API **2026.10.5**. This exact host
  version is the minimum and the tested compatibility target. Other releases
  need a compatibility test before widening the peer/API range.
- Stable `fit-agent >=0.1.0` **and integration protocol 1**. Version alone is
  insufficient: `integration-info --json` must advertise `doctor-json`,
  `status-json`, `fetch-json`, `workspace-assertion`, and `guarded-sync-v1`.
  Older released CLIs can fail this gate even when their version is >=0.1.0.

Install the CLI yourself on the **Gateway host**:

```sh
brew tap jogvan-k/tap
brew install fit-agent
fit-agent --version
fit-agent integration-info --json
```

If the release does not yet include protocol 1, build this repository revision
with Go 1.25+ (the explicit version stamp permits development compatibility testing):

```sh
go build -ldflags '-X github.com/jogvan-k/fit-agent/internal/cli.Version=0.1.0' -o bin/fit-agent ./cmd/fit-agent
```

Configure `binaryPath` to the absolute path of that binary. The plugin never
downloads or silently installs a CLI.

Once the manually authenticated first ClawHub publication is available:

```sh
openclaw plugins install @jogvan-k/fit-agent
openclaw plugins enable fit-agent
openclaw plugins inspect fit-agent --runtime --json
```

Before that publication, install from a checkout:

```sh
cd plugin
npm ci
npm run build
openclaw plugins install --link .
```

ClawHub publication and public install verification are release-operator steps;
source validation or a publish dry-run does not establish a public listing.
The existing standalone `fit-agent-setup` skill remains a discoverable onboarding
companion, separately published and not bundled as plugin runtime logic.

## Configuration and permissions

Merge this example into your OpenClaw configuration, using your own absolute path:

```json
{
  "plugins": {
    "allow": ["fit-agent"],
    "entries": {
      "fit-agent": {
        "enabled": true,
        "config": {
          "workspace": "/home/you/.openclaw/workspace",
          "timeoutMs": 120000,
          "diagnosticVerbosity": "quiet"
        }
      }
    }
  },
  "tools": { "allow": ["fit-agent"] }
}
```

Preserve existing allowlist entries. `doctor` and `status` are required tools;
the four side-effect/network tools are optional and require tool-policy opt-in.
Plugin tools are unavailable in sandboxed sessions: they do not escape a sandbox
to execute on the Gateway host. Use a trusted local, unsandboxed agent session.

| Config | Default | Validation |
|---|---|---|
| `binaryPath` | executable `fit-agent` on PATH | Absolute override; resolves symlinks; executable regular file |
| `workspace` | active agent workspace | Absolute path (`~/` accepted); resolves root symlinks |
| `profile` | CLI selection: environment → workspace pointer → `default` | 1–64 letters/digits/underscore/dot/hyphen; starts alphanumeric |
| `timeoutMs` | 120000 | Integer 1000–600000, per subprocess |
| `diagnosticVerbosity` | `quiet` | `normal` logs tool name and success/error layer only; never CLI verbose/private output |

The selected CLI profile must point to the exact canonical workspace. The new
global CLI `--workspace` flag asserts this binding instead of redirecting writes.
Workspace inspection is metadata-only, checks the actual current Go layout, and
rejects nested symlinks, special files, unsafe write permissions, and unexpected
owners. It creates nothing. Root symlinks are supported for diagnostic accuracy.
Inspection is bounded at 50,000 entries; planned markdown is limited to 8 MiB;
each sync is limited to 500 actions.

The host user needs executable access to the CLI, read/write access to their
workspace, access to the OS keyring/config, and HTTPS access to Intervals.icu
through the CLI. No API key belongs in plugin config or a tool call.

## Tools and onboarding

1. Ask **“Is FitAgent ready?”** → `fit_agent_doctor {}`. It checks the binary,
   feature gate, workspace shape and local credential availability without a
   network request. Missing binary diagnostics include the Homebrew commands.
2. `fit_agent_init {}` runs the CLI’s existing protected interactive form on
   the host’s controlling terminal. It returns only created scaffold paths on
   success. All terminal I/O bypasses the agent. In a headless Gateway, follow
   the returned argument vector in a private terminal on the host:
   `fit-agent init --workspace /your/workspace` (optionally `--profile-name NAME`).
   Then run doctor again. Never paste keys in chat. `{ "dryRun": true }` uses
   the CLI’s interactive dry-run and creates no files.
3. `fit_agent_refresh {}` runs `fetch --since 90d --json`. Override with e.g.
   `{ "since": "30d" }`; accepted history is 1–366 days. It returns counts, not
   raw fitness payloads. It performs remote reads and machine-owned local writes.
4. `fit_agent_status {}` returns activity/wellness recency, data file modification
   time, authored workout count, up to 30 upcoming dates (athlete-local calendar),
   and athlete-profile/training-plan presence. It performs local reads only and
   makes no coaching recommendations. A usable CLI profile is required.
5. Preview an explicitly bounded range:

   ```json
   { "from": "2026-11-01", "to": "2026-11-07" }
   ```

   `fit_agent_preview_workout_sync` calls
   `sync-workouts --from ... --to ... --dry-run --json`. Ranges contain 1–90
   inclusive calendar dates, from years 2000–2099. It reports action kind, date,
   event ID, all six action counts (create/update/delete/unchanged/skip/conflict),
   a deterministic fingerprint and a single-use confirmation token. Names,
   descriptions and raw cache data are omitted. Inspect the local planned files
   when more detail is needed. The plugin does not enable `--prune`; remote-only
   events are skips, and delete counts are normally zero.
6. **Present that exact preview to the user.** Only after explicit confirmation
   (or an existing explicit standing preference to push after successful preview)
   call `fit_agent_sync_workouts`:

   ```json
   {
     "from": "2026-11-01",
     "to": "2026-11-07",
     "confirmed": true,
     "confirmationToken": "<64-character token from the matching preview>"
   }
   ```

   This is the only plugin tool that writes the remote calendar. It also updates
   local machine-managed files and stamps returned IDs through the Go CLI.

## Confirmation and conflicts

Tokens expire after **10 minutes**, are consumed even when a sync attempt fails,
and are bound to the agent/session incarnation/requester, binary path, workspace,
profile and exact dates. Tokens are memory-only; reload/restart invalidates them.
There is no plugin option to disable confirmation. A boolean alone is insufficient.
The calling agent is responsible for obtaining human confirmation: a token proves
a matching preview occurred, not that a human read it.

Before pushing, the plugin re-previews and compares fingerprints. The CLI receives
`--expect-preview` and independently verifies the same plan immediately before
applying it. The CLI digest includes CLI version, profile/athlete/workspace,
range/prune policy, **all planned markdown bytes** (including drafts and managed
blocks), durable sync baselines, live inventory, and planned actions. The plugin
adds its own identity/range binding. Any changed input invalidates approval.
All markdown is hashed because the Go planner selects by frontmatter date, not
filename; editing an unrelated draft can therefore require a fresh preview.

The guarded CLI writes last-confirmed snapshots under
`fit-agent/.cache/sync-baseline/<event-id>.json`. `fetch` does not modify these.
A changed or deleted remote workout, missing baseline for an update, duplicate
identity, malformed CLI JSON, unknown action, or mismatched schema fails closed.
No confirmation token is issued for a conflict preview.

For an existing calendar without confirmed baselines, reconcile the agent-owned
workout intent with the remote workout yourself. Once the preview reports
`unchanged`, explicitly confirm that sync to establish its baseline. Subsequent
local edits can be previewed as updates. To retain local intent over conflicting
remote changes, resolve that decision using the CLI/calendar directly, then
reconcile and establish a fresh unchanged baseline. The plugin never edits
`fit-agent:icu` blocks or automatically resolves a conflict.

Remote HTTP APIs cannot atomically lock a whole calendar against edits made after
the final inventory read. Run one calendar writer at a time. The plugin serializes
its operations per workspace. Concurrent external editors/CLI processes are not
covered by that in-process lock. Failed or cancelled writes can be partial: check
the calendar and obtain a fresh preview before retrying.

## Privacy, diagnostics, and troubleshooting

- Processes use executable paths and literal argument arrays, **never a shell**.
- Credential input uses only the CLI terminal prompt. The plugin never reads the
  keyring/config contents. Child environments are allowlisted and omit
  `FIT_AGENT_API_KEY`, model-provider keys, `NODE_OPTIONS`, and `ICU_BASE_URL`.
- stdout/stderr capture is capped at 256 KiB combined. Stderr is redacted and
  retained only in in-memory failure diagnostics; agents receive fixed safe
  messages and allowlisted error layers. Unexpected JSON is never returned.
- Tool results are capped at 64 KiB and bounded by schemas (500 sync actions, 30 upcoming dates).
  No raw activity, wellness or cache contents are returned, and nothing is logged
  to an external service by this package.
- Timeouts/cancellation terminate the CLI process group. After a failed write,
  inspect the local CLI/calendar before retrying.

| Error layer | Next action |
|---|---|
| `binary` | Install/upgrade the CLI; check absolute path, executable bit, version and protocol gate |
| `workspace` | Run doctor; correct profile binding, missing scaffold, file type or ownership/permissions |
| `profile/config` | Run interactive `fit-agent init` in a private terminal |
| `Intervals authentication` | Reconfigure the CLI’s credentials interactively |
| `network` | Check connectivity/rate limiting; inspect state before retrying writes |
| `validation` | Check input/schema versions; run the CLI locally for private details |
| `sync conflict` | Reconcile manually and present a new preview for confirmation |

The CLI can safely fall back to a mode-0600 config file when the OS keyring is
unavailable; that existing CLI warning appears in the private setup terminal.
Doctor verifies local credential availability, not whether the server accepts it.

## Development and validation

```sh
cd plugin
npm ci
npm run test
npm run typecheck
npm run lint
npm run validate
npm exec -- clawhub package validate . --openclaw-version 2026.10.5 --out .validation --runtime --allow-execute --no-mock-sdk
npm exec -- clawhub package publish . --owner jogvan-k --source-repo jogvan-k/fit-agent --source-path plugin --dry-run
npm pack --dry-run
```

Use the pinned local ClawHub CLI; older skills-only releases do not implement
`package validate` or `package publish`. Fake-executable tests exercise each tool,
the full onboarding flow on a PTY, failures, redaction, invalid output, version
gating, cancellation, argv, fingerprints and stale/replayed confirmation. Python 3
is a test-only requirement for PTY fixtures. Go tests use `httptest.Server` and
verify actual guarded create/update/delete, conflict rejection and cache ownership.

Runtime inspection uses `--no-mock-sdk` to load the real pinned SDK and Zod.
The inspector's default dependency mocks do not implement Zod's ISO date API.

Input/result schemas and the manifest are generated from `src/schemas.ts` by
`npm run build`; semantic calendar/range refinements are enforced at runtime in
addition to JSON Schema validation. Commit regenerated schemas with source changes.

## Rollback and uninstall

Disable immediately:

```sh
openclaw plugins disable fit-agent
```

Remove the plugin:

```sh
openclaw plugins uninstall fit-agent
```

Remove any manually added `plugins.load.paths` entry and tool allowlist entries.
For rollback, install a previously reviewed **published** exact package version:
`openclaw plugins install @jogvan-k/fit-agent@VERSION` and re-enable it. All pending
confirmation tokens are lost on plugin replacement. Workspace data, the installed
Go CLI, credentials and already-pushed remote events are retained. Uninstalling
does not undo calendar mutations; manage those deliberately with the calendar/CLI.

To remove the separately installed CLI: `brew uninstall fit-agent`. See the
repository release documentation before deleting a workspace or CLI profile.
No uninstall step should expose or print stored credentials.

## Release ownership

The first publication is **manually authenticated** by the `jogvan-k` owner.
See [`docs/plugin-release.md`](https://github.com/jogvan-k/fit-agent/blob/main/docs/plugin-release.md)
for validation, security-check/public-install verification, and the subsequent
GitHub Actions trusted-publishing setup. No registry credential belongs in this
repository or plugin config.
