# FitAgent Setup

Install and onboard [FitAgent](https://github.com/jogvan-k/fit-agent) for
Intervals.icu-powered fitness coaching in an OpenClaw workspace.

## What this skill does

- checks whether the `fit-agent` CLI is available;
- installs it through Homebrew or Go when needed;
- initialises the workspace through FitAgent's secure interactive setup;
- fetches an initial training history; and
- verifies that the workspace is ready for coaching, workout planning, and safe
  calendar syncing.

It is the recommended starting point for FitAgent. The skill does **not** make
training recommendations itself; it prepares a safe, usable workspace for an
agent and athlete to work from.

## Requirements

- An [Intervals.icu](https://intervals.icu) account and API key.
- An OpenClaw workspace in which to store the local `fit-agent/` directory.
- Either Homebrew (recommended on macOS or Linux) or Go 1.25+.

The skill never asks for an Intervals.icu API key in chat. During setup,
FitAgent requests it interactively and stores it using its own configuration
flow.

## Install FitAgent manually

The skill can guide this automatically. To do it manually:

```sh
brew tap jogvan-k/tap
brew install fit-agent
```

Or, with Go 1.25 or later:

```sh
go install github.com/jogvan-k/fit-agent/cmd/fit-agent@latest
```

Verify the installation:

```sh
fit-agent --version
```

## What setup changes

After `fit-agent init`, the selected workspace contains a `fit-agent/`
directory. FitAgent creates and refreshes machine-owned activity, wellness, and
cache data; the athlete and coaching agent use `ATHLETE-PROFILE.md`,
`TRAINING-PLAN.md`, and planned-workout files for coaching work.

Before any calendar update, preview the change first:

```sh
fit-agent sync-workouts --from YYYY-MM-DD --to YYYY-MM-DD --dry-run
```

Only run the non-dry-run sync after reviewing and approving the result.

## Privacy and safety

- Keep API keys and raw activity data out of chat, commits, and shared notes.
- Do not manually edit FitAgent's `activities/`, `wellness/`, or `.cache/`
  directories.
- FitAgent supports coaching workflows; it is not a medical diagnostic tool.
  Seek qualified care for injuries, acute symptoms, or health concerns.

## Links

- [FitAgent source and releases](https://github.com/jogvan-k/fit-agent)
- [Homebrew tap](https://github.com/jogvan-k/homebrew-tap)
- [Intervals.icu](https://intervals.icu)
