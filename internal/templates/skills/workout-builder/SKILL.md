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

## File layout in `planned-workouts/`

Each date has at most one file, `YYYY-MM-DD.md`. The file is
**jointly owned**:

- **You own** the YAML frontmatter (including the `workouts:` list with
  `name`, `type`, `moving_time_s`, and `icu_event_id`), the prose, and
  every ` ```fit-workout ` fenced block.
- **`fit-agent` owns** a single fenced YAML block delimited by HTML
  comments:

  ```
  <!-- fit-agent:icu:begin -->
  ```yaml
  # Machine-managed: rewritten on every `fit-agent render planned`.
  # Do not edit between the begin/end sentinels.
  ...
  ```
  <!-- fit-agent:icu:end -->
  ```

  This block lists every icu event currently on the calendar for that
  date (id, name, type, category, moving_time_s, start_date_local,
  description). It is regenerated on every `fit-agent fetch` and every
  `fit-agent sync-workouts`. **Do not edit anything between the
  `begin` and `end` sentinels — your changes will be overwritten.**

When `fit-agent fetch` runs and finds an icu event on a date with no
existing `.md` file, it creates one with an empty `workouts: []` list
and the machine block populated. You then fill in `workouts:`,
`## name` sections, and ` ```fit-workout ` fences as you would for any
other day.

## Taking ownership of an icu-side workout

If an icu event was authored elsewhere (the icu web UI, a Garmin
device, another sync), it appears only inside the machine block. To
take ownership:

1. Read the event's `name`, `type`, and `description` from the machine
   block.
2. Add a corresponding entry to the `workouts:` list at the top of the
   same file, copying the `icu_event_id` so that `sync-workouts` will
   `PUT` updates rather than create a new event.
3. Add a `## <name>` section with prose (optional) and a
   ` ```fit-workout ` fence rewriting the description in the DSL.
4. Run `fit-agent sync-workouts --dry-run`. The diff should show an
   `update` for the existing id, not a `create`.

## Inputs you read

- `TRAINING-PLAN.md` — the macro plan (created by training-plan-coach).
  This is your authoritative source for what each day should look
  like. If it is missing, ask the user to run the training-plan-coach
  skill first.
- `ATHLETE-PROFILE.md` — for zone definitions, FTP, threshold pace.
- Existing `fit-agent/planned-workouts/*.md` — both the agent-authored
  parts you wrote earlier and the machine block inside each file
  listing icu-side events. Use both to avoid double-booking a day.

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
- **Repeat block (inline)** — `- <reps>x (<work> / <rest>)` for the
  common two-step case (effort + recovery):
  - `- 5x (4m Z5 / 3m Z2)`
  - `- 8x (400m Z5 / 90s Z1)`
  - `- 3x (1m 150% / 1m 50%)`
- **Repeat block (multi-step)** — for three or more sub-steps per rep,
  use a header line `<reps>x` followed by one `- <step>` line per
  sub-step. The block ends at a blank line, EOF, or any non-`-` line.
  Useful for workouts with a finishing kick, walk-back recovery, etc.:

  ```
  4x
  - 1km threshold
  - 200m Z5
  - 120s recovery
  ```

  An optional outer note attaches to the header: `4x -- main set`.
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
2. Read existing `fit-agent/planned-workouts/*.md` for the date range
   (both your own content and the machine block inside each file).
   Skip dates that already have a workout (either authored or
   reflected in the machine block) unless the user asks to overwrite.
3. For each new date, write the agent-authored markdown file
   (`YYYY-MM-DD.md`).
4. Run `fit-agent sync-workouts --dry-run` and show the diff.
5. Ask for confirmation, then run `fit-agent sync-workouts`.

When the athlete asks to swap a session:

1. If the day's `<date>.md` exists but has no entry in the `workouts:`
   list for this workout (i.e. the icu event was authored elsewhere
   and only appears inside the machine block), first add a `workouts:`
   entry that stamps the existing `icu_event_id`.
2. Edit the `workouts:` entry and its `## name` section in place. Keep
   the same `icu_event_id` so sync will `PUT` an update rather than
   create a new event.
3. Run `fit-agent sync-workouts --dry-run` to confirm.
4. Run `fit-agent sync-workouts`.

To remove a planned workout entirely, delete the agent-authored file
and run `fit-agent sync-workouts --prune`. Without `--prune`, sync
refuses to delete events that exist on icu but not in markdown.

## Don'ts

- Do not edit anything between the `<!-- fit-agent:icu:begin -->` and
  `<!-- fit-agent:icu:end -->` sentinels in a planned-workout file.
  That block is regenerated on every sync.
- Do not invent zones the athlete has not provided. If FTP is
  missing, fall back to RPE/heart-rate language and say so.
- Do not write to `fit-agent/activities/*.yaml` or
  `fit-agent/wellness/*.yaml` — those are machine-owned.
- Do not run `fit-agent sync-workouts` without showing the dry-run
  first.
