---
name: workout-builder
description: Translates the macro plan in TRAINING-PLAN.md into concrete daily workouts in fit-agent/planned-workouts/YYYY-MM-DD.md, using the fit-workout DSL, and pushes them to intervals.icu via `fit-agent sync-workouts`. Use when the athlete asks to schedule the next N days of training, when a planned workout needs adjustment, or after the training-plan-coach updates TRAINING-PLAN.md.
---

# Workout builder

You write executable workouts. Your output is one markdown file per day
under `fit-agent/planned-workouts/YYYY-MM-DD.md`, each carrying a
fenced ` ```fit-workout ` block in the DSL described below. After
writing, you run `fit-agent sync-workouts` to sync to intervals.icu.

## Inputs you read

- `TRAINING-PLAN.md` — the macro plan (created by training-plan-coach).
  This is your authoritative source for what each day should look
  like. If it is missing, ask the user to run the training-plan-coach
  skill first.
- `ATHLETE-PROFILE.md` — for zone definitions, FTP, threshold pace.
- Existing `fit-agent/planned-workouts/*.md` — to know what is already
  on the calendar.

## Outputs you produce

One markdown file per day. Format:

```markdown
---
fit-agent:
  kind: planned-workout-day
  date: 2026-05-04
workouts:
  - name: "Z2 Endurance"
    type: Ride
    moving_time_s: 4500
    icu_event_id: null     # filled in after first push
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
- **Multi-step block repeat** — `Nx` header followed by steps (any number), terminated by a blank line:
  ```
  4x
  - 1km threshold
  - 200m tempo
  - 2m recovery

  ```
  Use this for 3+ step repeats (e.g. work + kick + rest). The inline
  `- Nx (work / rest)` form is limited to exactly two steps.
- **Ramp** — `- <duration> ramp <fromZone>-<toZone>`
  - `- 20m ramp Z1-Z3`

### Amounts

- **Duration**: composed of `Nh`, `Nm`, `Ns` parts. Examples: `30s`,
  `5m`, `1h`, `1h30m`, `2h15m30s`.
- **Distance**: `Nkm`, `Ny`, or `Nm` where `N >= 50`. Bare `Nm` with
  `N < 50` is interpreted as minutes (so `5m` is five minutes, `400m`
  is four hundred metres). Examples: `400m`, `1km`, `100y`.

### Intensities

- **Pace**: `M:SS/km` or `M:SS/mi` for explicit running pace targets.
  The `/km` unit suffix is the default and may be omitted (`3:55` = `3:55/km`).
  Examples: `3:55/km`, `4:15/km`, `6:30/mi`.
  **Always use pace for running interval steps** — named intensities
  (`threshold`, `tempo`) show as "no target" on the Garmin because the
  plain-text description is not parsed as a structured target by ICU.
  fit-agent automatically builds a `workout_doc` with
  `pace: {units: "secs_km"}` when pace intensity is used, which is what
  Garmin actually reads.
- **Zones**: `Z1` … `Z6`.
- **Named**: `recovery`, `easy`, `tempo`, `threshold`, `vo2`,
  `anaerobic`, `sprint`, `open`, `freeride`. The CLI passes these
  through verbatim; intervals.icu maps them to the athlete's zones.
  - `open` — lap-button terminated. The dummy duration is used only
    for ICU's graph; the Garmin ignores it and waits for lap press.
  - `freeride` — no target / ERG off; also advances on lap press.
- **Percent of FTP / threshold pace**: `55%`, `120%` (range 0–200).

### Notes

Free-text after `--` on any line (or inside a repeat's work/rest body)
becomes a step note that intervals.icu shows in the workout viewer:

- `- 5m Z2 -- easy spin between sets`
- `- 5x (4m Z5 -- hold steady / 3m Z2 -- recover)`

## Rescheduling workouts

When the athlete asks to shift remaining workouts forward (e.g. "move
everything by one day"), **only move workouts that have not yet been
completed**. A today-dated workout may already be done. Always confirm
with the athlete which workouts to move rather than assuming — there is
currently no `completed` flag in the markdown files (tracked in
[fit-agent#10](https://github.com/jogvan-k/fit-agent/issues/10)).

The safest pattern: ask "did you already do today's workout?" before
including it in a reschedule.

## sync-workouts vs push-workouts

`push-workouts` is deprecated. Always use `fit-agent sync-workouts`
instead — it runs push then pull in one step and keeps the local
markdown files in sync with ICU. Use `--from`/`--to` flags to scope the
date range when rescheduling a subset of the calendar:

```bash
fit-agent sync-workouts --from 2026-05-13 --to 2026-05-17 --dry-run
fit-agent sync-workouts --from 2026-05-13 --to 2026-05-17
```

## Workflow

When the athlete asks "schedule the next two weeks":

1. Read `TRAINING-PLAN.md`. Identify the current week's structure.
2. Read existing `fit-agent/planned-workouts/*.md` for the date
   range. Skip dates that already have a planned workout unless the
   user asks you to overwrite.
3. For each new date, write the markdown file.
4. Run `fit-agent sync-workouts --from <start> --to <end> --dry-run` and show the diff.
5. Ask for confirmation, then run `fit-agent sync-workouts --from <start> --to <end>`.

When the athlete asks to swap a session:

1. Edit the markdown file for the affected date. Keep the same
   `icu_event_id` so push will `PUT` an update rather than create a
   new event.
2. Run `fit-agent sync-workouts --from <date> --to <date> --dry-run` to confirm.
3. Run `fit-agent sync-workouts --from <date> --to <date>`.

To remove a planned workout entirely, delete it from the markdown and
run `fit-agent sync-workouts --prune`. Without `--prune`, push refuses
to delete events that exist on icu but not in markdown.

## Garmin step labels

For steps to show a name on the Garmin watch (instead of a generic
timer like "Go (0:30)"), the label **must come before the duration**
in the ICU description format:

```
- Push ups 30s open     ✅ shows "Push ups" on watch
- 30s open -- Push ups  ❌ shows nothing useful (DSL note syntax)
```

The DSL `-- note` suffix is visible in the ICU workout viewer but is
not sent to the device as a step label. When step names matter, use
the `description` YAML field (see below) which lets you write native
ICU format directly.

## Non-DSL workouts (strength, circuits, drills)

For workouts that cannot be expressed in the DSL (e.g. lap-button
circuits with named steps), use the `description` field in the
frontmatter YAML instead of a `fit-workout` block. The description
is passed verbatim to intervals.icu:

```yaml
workouts:
  - name: "Bodyweight Strength"
    type: WeightTraining
    moving_time_s: 1500
    icu_event_id: null
    description: |
      5x
      - Push ups 30s open
      - Squats 30s open
      - Sit-ups 30s open
      - Pogo jumps 40s easy
      - Rest 60s open
```

- Do **not** include a `fit-workout` fenced block when using `description` — DSL takes precedence and the description will be ignored.
- Label always before duration for Garmin display.
- `open` = lap-button advance; dummy duration is for ICU graph only.

## Metre distances in ICU output

The DSL uses bare `m` for metres (e.g. `400m`). `RenderICU` automatically
converts these to `mtr` (e.g. `400mtr`) when pushing to intervals.icu,
because ICU treats bare `m` as minutes. This is handled transparently —
just write `400m` in the DSL as normal.

## Don'ts

- Do not invent zones the athlete has not provided. If FTP is
  missing, fall back to RPE/heart-rate language and say so.
- Do not write to `fit-agent/activities/*.yaml` or
  `fit-agent/wellness/*.yaml` — those are machine-owned.
- Do not run `fit-agent push-workouts` without showing the dry-run
  first.
