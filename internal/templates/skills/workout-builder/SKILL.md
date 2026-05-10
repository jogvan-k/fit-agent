---
name: workout-builder
description: Translates the macro plan in TRAINING-PLAN.md into concrete daily workouts in fit-agent/planned-workouts/YYYY-MM-DD.md, using the fit-workout DSL, and syncs them with intervals.icu via `fit-agent sync-workouts`. Use when the athlete asks to schedule the next N days of training, when a planned workout needs adjustment, or after the training-plan-coach updates TRAINING-PLAN.md.
---

# Workout builder

You write executable workouts. Your output is one markdown file per day
under `fit-agent/planned-workouts/YYYY-MM-DD.md`, each carrying a
fenced ` ```fit-workout ` block in the DSL described below. After
writing, you run `fit-agent sync-workouts` to two-way sync with
intervals.icu.

## Two kinds of files in `planned-workouts/`

The directory contains two kinds of markdown files. The suffix tells
them apart:

- `YYYY-MM-DD.md` — **agent-authored, writeable.** You create and edit
  these. They carry the structured `fit-workout` DSL and are the
  source pushed to intervals.icu.
- `YYYY-MM-DD.<icu-id>.icu.md` — **read-only, machine-owned mirror.**
  These are workouts that already exist on intervals.icu (authored on
  the icu web/app, by another device, or by an earlier sync). Treat
  them as inputs, not outputs. `sync-workouts` regenerates them on
  every run; any edits you make to them are lost.

If you want to take ownership of a workout that currently exists only
as an `.icu.md` mirror, copy its contents into a new
`YYYY-MM-DD.md`, set `icu_event_id` in the frontmatter to the same id
the mirror used, and delete the `.icu.md` file. The next
`sync-workouts` will see the local file as the source of truth and
PUT updates to the same icu event.

## Inputs you read

- `TRAINING-PLAN.md` — the macro plan (created by training-plan-coach).
  This is your authoritative source for what each day should look
  like. If it is missing, ask the user to run the training-plan-coach
  skill first.
- `ATHLETE-PROFILE.md` — for zone definitions, FTP, threshold pace.
- Existing `fit-agent/planned-workouts/*.md` — your prior authored
  files, to know what is already on the calendar.
- Existing `fit-agent/planned-workouts/*.icu.md` — read-only mirrors
  of icu-side workouts. Use these to avoid double-booking a day.

## Outputs you produce

One agent-authored markdown file per day. Format:

```markdown
---
fit-agent:
  kind: planned-workout-day
  date: 2026-05-04
workouts:
  - name: "Z2 Endurance"
    type: Ride
    moving_time_s: 4500
    icu_event_id: null     # filled in after first sync
---

## Z2 Endurance

Easy aerobic ride. Stay strictly in Z2; if HR drifts high in the
second half, ease off rather than push through.

```fit-workout
- 10m Z1
- 60m Z2
- 5m Z1
```
```

Multiple workouts on one day are allowed: add more entries to the
`workouts:` list and one `## name` section per workout.

## The `fit-workout` DSL

The DSL is validated by `fit-agent workout parse|render|lint`. The
canonical reference is `internal/workoutdsl` in the `fit-agent` repo.

### Lines

Each non-blank, non-comment line is one step. Lines starting with `#`
are comments and ignored.

- **Simple step** — `- <amount> <intensity>`
  - `- 10m Z2`
  - `- 45s Z5`
  - `- 2h Z1`
  - `- 15m 55%`
- **Repeat block** — `- <reps>x (<work> / <rest>)`
  - `- 5x (4m Z5 / 3m Z2)`
  - `- 8x (400m Z5 / 90s Z1)`
  - `- 3x (1m 150% / 1m 50%)`
- **Ramp** — `- <duration> ramp <fromZone>-<toZone>`
  - `- 20m ramp Z1-Z3`

### Amounts

- **Duration**: composed of `Nh`, `Nm`, `Ns` parts. Examples: `30s`,
  `5m`, `1h`, `1h30m`, `2h15m30s`.
- **Distance**: `Nkm`, `Ny`, or `Nm` where `N >= 50`. Bare `Nm` with
  `N < 50` is interpreted as minutes (so `5m` is five minutes, `400m`
  is four hundred metres). Examples: `400m`, `1km`, `100y`.

### Intensities

- **Zones**: `Z1` … `Z6`.
- **Named**: `recovery`, `easy`, `tempo`, `threshold`, `vo2`,
  `anaerobic`, `sprint`. The CLI passes these through verbatim;
  intervals.icu maps them to the athlete's zones.
- **Percent of FTP / threshold pace**: `55%`, `120%` (range 0–200).

### Notes

Free-text after `--` on any line (or inside a repeat's work/rest body)
becomes a step note that intervals.icu shows in the workout viewer:

- `- 5m Z2 -- easy spin between sets`
- `- 5x (4m Z5 -- hold steady / 3m Z2 -- recover)`

## Workflow

When the athlete asks "schedule the next two weeks":

1. Read `TRAINING-PLAN.md`. Identify the current week's structure.
2. Read existing `fit-agent/planned-workouts/*.md` and `*.icu.md`
   for the date range. Skip dates that already have a workout
   (either authored or pulled from icu) unless the user asks to
   overwrite.
3. For each new date, write the agent-authored markdown file
   (`YYYY-MM-DD.md`).
4. Run `fit-agent sync-workouts --dry-run` and show the diff.
5. Ask for confirmation, then run `fit-agent sync-workouts`.

When the athlete asks to swap a session:

1. If the day currently exists as an `.icu.md` mirror only, first
   create an agent-authored `YYYY-MM-DD.md` that takes ownership of
   the same `icu_event_id` (then delete the mirror).
2. Edit the agent-authored markdown file. Keep the same
   `icu_event_id` so sync will `PUT` an update rather than create a
   new event.
3. Run `fit-agent sync-workouts --dry-run` to confirm.
4. Run `fit-agent sync-workouts`.

To remove a planned workout entirely, delete the agent-authored file
and run `fit-agent sync-workouts --prune`. Without `--prune`, sync
refuses to delete events that exist on icu but not in markdown.

## Don'ts

- Do not edit `*.icu.md` files. They are regenerated on every sync.
- Do not invent zones the athlete has not provided. If FTP is
  missing, fall back to RPE/heart-rate language and say so.
- Do not write to `fit-agent/activities/*.yaml` or
  `fit-agent/wellness/*.yaml` — those are machine-owned.
- Do not run `fit-agent sync-workouts` without showing the dry-run
  first.
