# FitAgent for OpenClaw

FitAgent turns your [Intervals.icu](https://intervals.icu) training data into a
local workspace that an OpenClaw agent can use to help with training plans,
daily workout decisions, and structured workouts.

This plugin adds safe, typed FitAgent tools to OpenClaw. It is designed to keep
you in control: it can inspect and refresh your data freely, but any calendar
change is previewed first and requires confirmation before it is pushed.

## What you can do

After setup, ask your agent things like:

- “Is my training workspace ready?”
- “Refresh my recent Intervals.icu data.”
- “How has my training load and recovery looked this week?”
- “Build a plan for my next race.”
- “Create next week’s workouts and show me what will change.”
- “Move Friday’s session to Saturday.”

The plugin provides six tools behind those requests:

| Tool | What it does |
| --- | --- |
| `fit_agent_doctor` | Checks the CLI, workspace, profile, and local setup. |
| `fit_agent_init` | Starts FitAgent’s secure interactive setup. |
| `fit_agent_refresh` | Pulls recent Intervals.icu data into the workspace. |
| `fit_agent_status` | Shows data freshness and upcoming workout state. |
| `fit_agent_preview_workout_sync` | Shows planned calendar changes without pushing them. |
| `fit_agent_sync_workouts` | Pushes a previously previewed, confirmed calendar change. |

## Before you install

You need:

- an OpenClaw Gateway running **2026.10.5 or newer**;
- an [Intervals.icu](https://intervals.icu) account; and
- the FitAgent command-line tool installed on the same machine as the Gateway.

Install the CLI with Homebrew:

```sh
brew tap jogvan-k/tap
brew install fit-agent
```

Or with Go 1.25 or newer:

```sh
go install github.com/jogvan-k/fit-agent/cmd/fit-agent@latest
```

Check that it is available:

```sh
fit-agent --version
```

## Install the plugin

```sh
openclaw plugins install 'clawhub:@jogvan-k/fit-agent@0.1.4' --accept-capabilities
```

Then ask your agent to run a FitAgent doctor check. If you have not created a
FitAgent workspace yet, ask it to set one up.

## First-time setup

FitAgent stores data in your OpenClaw workspace. During setup it creates a
`fit-agent/` directory containing your athlete profile, training plan, recent
activities, wellness data, and planned workouts.

The first setup uses an **interactive terminal prompt** for your Intervals.icu
API key. The key is not entered into chat, plugin configuration, or a Markdown
file.

If your Gateway is headless, run this in a private terminal on the Gateway host:

```sh
fit-agent init
```

Then ask the agent to refresh data:

```sh
fit-agent fetch --since 90d
```

You can also install the separate **FitAgent Setup** skill from ClawHub for a
guided onboarding flow.

## How calendar changes stay safe

The plugin does not silently push workouts.

1. Your agent first runs a **preview** for an explicit date range.
2. You review the proposed creates and changes.
3. The agent asks for confirmation.
4. The plugin pushes only that exact previewed range.

The confirmation expires after ten minutes and cannot be reused. If planned
workouts, the selected profile, or the remote calendar changes in between, the
plugin refuses to push and asks for a fresh preview.

For safety, the plugin currently creates new workouts and confirms unchanged
ones. It intentionally refuses plugin-driven edits or deletions of existing
remote events because Intervals.icu does not yet provide an event-version check
that would prevent an intervening remote edit from being overwritten.

## Privacy and data ownership

- Your Intervals.icu API key stays in FitAgent’s protected local setup.
- Raw activities, wellness data, and caches remain local to your workspace.
- The plugin returns compact summaries, not raw training history.
- FitAgent never calls an LLM itself; OpenClaw agents call the local CLI.
- The plugin is a coaching aid, not medical advice. Seek qualified care for
  injuries, acute symptoms, or health concerns.

## Troubleshooting

**“FitAgent is not ready”**
: Check the CLI with `fit-agent --version`, then run `fit-agent init` in the
  workspace you want to use.

**“Workspace is incomplete”**
: Run the doctor tool first. It will identify missing setup files without
  changing anything.

**“Confirmation is stale”**
: Preview the date range again and confirm the new preview.

**“Sync conflict”**
: Review the workout in Intervals.icu and the local planned-workout file, resolve
  the intended version deliberately, then preview again.

## Advanced configuration

Most users do not need configuration. If your FitAgent binary or workspace is
outside the normal Gateway environment, configure the plugin with an absolute
`binaryPath`, `workspace`, or FitAgent `profile` in OpenClaw’s plugin settings.

For development, release operations, complete configuration schemas, and the
trusted-publishing workflow, see the repository:

- [FitAgent source and documentation](https://github.com/jogvan-k/fit-agent)
- [Plugin protocol](https://github.com/jogvan-k/fit-agent/blob/main/docs/plugin-protocol.md)
- [Plugin release guide](https://github.com/jogvan-k/fit-agent/blob/main/docs/plugin-release.md)
