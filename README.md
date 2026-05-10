# fit-agent

> Turn your AI agent into your personal fitness coach.

`fit-agent` is a Go CLI that hands your training data to an AI agent in a
form it can actually reason about — and lets the agent push tomorrow's
workout straight to your watch or bike computer.

It bridges [intervals.icu](https://intervals.icu) (where your devices
already sync) and an [OpenClaw](https://docs.openclaw.ai) workspace (where
your agent does its thinking).

> **Status:** pre-alpha. Design is set, code is being written. See
> [`agent-plan.md`](./agent-plan.md) for the v1 roadmap.

## What it does

- **Pulls everything you do** — activities, per-lap and per-interval
  metrics from the underlying `.fit` file, daily wellness (HRV, RHR,
  sleep, steps, stress) — from intervals.icu into a local workspace.
- **Renders it for an LLM** — concise, commented YAML for activity and
  wellness data; markdown for narrative files. No 174-field JSON dumps,
  no token-eating prose tables.
- **Keeps the originals** — raw icu JSON and raw `.fit` files live in
  `.cache/`, so the agent-facing files can always be regenerated.
- **Lets the agent plan and push** — bundled coaching skills teach an
  agent to interview you about goals, pick a methodology (Jack Daniels,
  80/20, Norwegian, etc.), build a multi-week plan, translate it into
  daily workouts, and push them to intervals.icu (and on to your device)
  with one command.
- **Stays out of your way** — agent-owned files (`ATHLETE-PROFILE.md`,
  `TRAINING-PLAN.md`, planned-workout intent) are never overwritten.
  Machine-owned data files are regenerated on every fetch.

## Workspace at a glance

```
my-coaching/
├── ATHLETE-PROFILE.md            # goals, history, constraints (you + agent)
├── TRAINING-PLAN.md              # the plan the coach builds with you
├── skills/                       # OpenClaw coaching skills (bundled)
│   ├── training-plan-coach/
│   ├── workout-builder/
│   └── training-session-coach/
└── fit-agent/
    ├── activities/2026-05-03.yaml      # today's session(s), with laps
    ├── wellness/2026-05.yaml           # the month's daily wellness
    ├── planned-workouts/2026-05-04.md         # agent-authored workout
    ├── planned-workouts/2026-05-06.4711.icu.md # read-only mirror of an icu-side workout
    └── .cache/                          # raw icu JSON + .fit files
```

## Commands (planned for v1)

```sh
fit-agent init                    # one-time setup; scaffolds the workspace
fit-agent fetch --since 30d       # pull activities + wellness + planned
fit-agent sync-workouts           # push agent-authored workouts and pull icu-side ones
fit-agent serve                   # poll intervals.icu on a cadence (daemon)
fit-agent setup-service           # install ~/.config/systemd/user/fit-agent.service
fit-agent remove-service          # tear it down again
```

Plus atomic subcommands for debugging and agent use:

```sh
fit-agent cache activity <id>     # download raw json + .fit only
fit-agent render activity <id>    # cache → YAML, no network
fit-agent fit laps <file.fit>     # inspect a parsed .fit file
fit-agent workout render <file>   # convert the fit-workout DSL
```

## How it works with your agent

1. `fit-agent init` creates the workspace and installs three coaching
   skills as [OpenClaw workspace skills](https://docs.openclaw.ai/tools/skills).
2. You open the workspace with your agent. The agent reads
   `ATHLETE-PROFILE.md` and recent `activities/` + `wellness/` data.
3. The **training-plan coach** skill walks you through goals and
   methodology, then writes `TRAINING-PLAN.md`.
4. The **workout-builder** skill turns the plan into concrete daily
   workouts under `planned-workouts/` and runs `fit-agent sync-workouts`,
   which pushes new files to intervals.icu and pulls back any workouts
   authored on icu (or by another device) as read-only `.icu.md`
   mirrors.
5. The **training-session coach** skill checks today's wellness against
   today's planned workout and recommends adjustments before you train.

The CLI never calls an LLM. The agent calls the CLI.

## Roadmap

- **v1** — three commands, intervals.icu only, manual `fetch`.
- **post-v1** — `fit-agent serve` polls intervals.icu on a cadence
  (15 min default, quiet-hours aware), wrapped by `setup-service` /
  `remove-service` for systemd-user installation. OpenClaw
  webhook integration so the agent is notified the moment new data lands,
  and full coaching prompts in the bundled skills.

## Built on

- [`muktihari/fit`](https://github.com/muktihari/fit) — Go FIT SDK.
- [intervals.icu API](https://intervals.icu/api-docs.html).
- [OpenClaw](https://docs.openclaw.ai) skill format.

## License

MIT — see [`LICENSE`](./LICENSE).
