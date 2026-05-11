# fit-agent — Implementation Plan

`fit-agent` is a Go CLI that turns an AI agent into a personal fitness coach by
maintaining a markdown workspace synced with [intervals.icu](https://intervals.icu).
The agent reads/writes the workspace; the CLI is the bridge between the agent and
intervals.icu (and, later, devices).

The workspace is designed to be opened as an
[OpenClaw](https://docs.openclaw.ai) workspace, so the coaching skills shipped
with `fit-agent` are loaded automatically as
[workspace skills](https://docs.openclaw.ai/tools/skills) at
`<workspace>/skills/<skill-name>/SKILL.md`.

This plan covers v1 only, with hooks for the post-v1 webhook service.

---

## 1. Goals & non-goals

### v1 goals
- One self-contained binary (`fit-agent`) installable via `go install`.
- Three commands: `init`, `fetch`, `sync-workouts` (with `push-workouts`
  retained as a deprecated push-only alias).
- A workspace that is human-, agent-, and OpenClaw-friendly. Narrative
  files are markdown; regenerated-on-fetch data files (activities,
  wellness) are commented YAML — see §10 for the rationale.
- **FIT-file parsing for activities**, so per-lap and per-interval metrics
  (avg HR, avg power, pace, etc.) are available to the agent. intervals.icu's
  activity JSON only carries activity-level aggregates; lap/interval detail
  must come from the `.fit` file.
- A `.cache/` directory holding the original icu JSON + `.fit` payloads,
  alongside concise agent-facing markdown views.
- Safe credential storage following OS conventions.
- Workspace files written by the CLI are *machine-owned* (regenerated on
  every `fetch`); files initialized as templates (e.g. `ATHLETE-PROFILE.md`)
  are *agent-owned* and never overwritten.

### v1 non-goals
- No webhook server (post-v1).
- No multi-provider support (intervals.icu only).
- No AI/LLM calls inside the CLI — agents drive the CLI, not the other way around.
- No ingestion of `.fit` files from outside intervals.icu in v1
  (the FIT decoder is generic, but the trigger is always an icu activity).

---

## 2. Resolved decisions (recap)

1. **FIT SDK is in-scope for v1.** intervals.icu activity JSON has 174
   activity-level fields but no per-lap data. We use
   [`github.com/muktihari/fit`](https://github.com/muktihari/fit) to parse
   the activity `.fit` file and extract lap and interval summaries.
2. **Workout DSL conversion is owned by `fit-agent`.** The agent writes
   workouts in a documented markdown form; `WORKOUT-BUILDER.md` (a workspace
   skill) teaches the agent that form; `internal/workoutdsl` converts it to
   the intervals.icu workout-description string used by `POST /events`.
3. **Two-tier storage:** `.cache/` holds untouched icu JSON + raw `.fit`;
   the human/agent-facing markdown is a concise distillation rendered from
   the cache. The cache is the source of truth for re-rendering without
   re-fetching.
4. **Multi-activity days** are supported by stacking sections in the per-day
   activity file.
5. **Credentials** stored via OS keyring when available
   ([`zalando/go-keyring`](https://github.com/zalando/go-keyring)), falling
   back to `${XDG_CONFIG_HOME}/fit-agent/config.toml` with mode `0600`.
   The workspace stores no secrets — only a profile name.
6. **Skill location.** Workspace skills live at
   `<workspace>/skills/<skill-name>/SKILL.md` (per OpenClaw spec). Each is a
   directory so the skill can ship supporting files (templates, examples).
7. **`ATHLETE-PROFILE.md` is agent-owned.** `init` writes a template with
   placeholders for goals, history, constraints, equipment, etc. The coach
   skills know to read and update it. `fetch` never touches it.
8. **Top-level file naming** follows OpenClaw convention
   (`ATHLETE-PROFILE.md`, like `SOUL.md` / `HEARTBEAT.md`). Subdirectories
   are lowercase kebab-case (`activities/`, `wellness/`, `planned-workouts/`).
9. **File format split** — agent-owned narrative files
   (`ATHLETE-PROFILE.md`, `README.md`, `skills/**/SKILL.md`,
   `planned-workouts/*.md`) are markdown. Machine-owned regenerated data
   files (`activities/*.yaml`, `wellness/*.yaml`) are YAML with a header
   comment describing units and pointing at the cache. See §10 for why.

---

## 3. Repository layout

```
fit-agent/
├── README.md
├── LICENSE
├── go.mod
├── go.sum
├── agent-plan.md
├── cmd/
│   └── fit-agent/
│       └── main.go                # cobra root, wires subcommands
├── internal/
│   ├── config/                    # XDG config + keyring
│   │   ├── config.go
│   │   └── keyring.go
│   ├── icu/                       # intervals.icu HTTP client
│   │   ├── client.go              # auth, retry, rate-limit
│   │   ├── athlete.go
│   │   ├── activities.go          # list + detail + .fit download
│   │   ├── wellness.go
│   │   ├── events.go              # planned workouts (category=WORKOUT)
│   │   └── types.go
│   ├── fitparse/                  # muktihari/fit wrapper
│   │   ├── decode.go              # .fit -> normalized struct
│   │   ├── laps.go                # extract per-lap summaries
│   │   ├── intervals.go           # extract per-interval summaries
│   │   └── decode_test.go
│   ├── workspace/                 # markdown read/write + paths
│   │   ├── layout.go              # canonical paths
│   │   ├── frontmatter.go         # yaml frontmatter helpers
│   │   ├── jsonblock.go           # extract/insert ```json fences
│   │   ├── ownership.go           # machine-owned vs agent-owned guards
│   │   └── atomic.go              # atomic file writes
│   ├── render/                    # icu+fit -> markdown
│   │   ├── activity_md.go         # uses both icu JSON and parsed .fit
│   │   ├── wellness_md.go
│   │   └── planned_md.go
│   ├── workoutdsl/                # markdown <-> icu workout description
│   │   ├── parse.go
│   │   ├── render.go
│   │   └── parse_test.go
│   ├── templates/                 # go:embed templates
│   │   ├── ATHLETE-PROFILE.md.tmpl
│   │   ├── README.md.tmpl
│   │   └── skills/                # full SKILL.md folders to copy on init
│   │       ├── training-plan-coach/SKILL.md
│   │       ├── training-session-coach/SKILL.md
│   │       └── workout-builder/SKILL.md
│   └── cli/
│       ├── init.go
│       ├── fetch.go               # composition of cache + render
│       ├── push_workouts.go
│       ├── cache.go               # cache activities|activity|wellness|events|athlete|all
│       ├── render.go              # render activities|activity|wellness|planned|all
│       ├── fit.go                 # fit summary|laps|dump
│       └── workout.go             # workout parse|render|lint
├── testdata/
│   ├── icu/                       # recorded API responses
│   ├── fit/                       # sample .fit files
│   └── workspace/                 # golden markdown files
└── .github/workflows/ci.yml
```

---

## 4. Workspace layout (what `init` creates)

```
<workspace>/
├── ATHLETE-PROFILE.md             # agent-owned; goals, history, constraints
├── TRAINING-PLAN.md               # agent-owned; created by training-plan-coach
├── README.md                      # explains the workspace to humans/agents
├── .fit-agent.toml                # profile pointer (no secrets)
├── .gitignore                     # excludes fit-agent/.cache by default
├── skills/                        # OpenClaw workspace skills
│   ├── training-plan-coach/
│   │   └── SKILL.md
│   ├── training-session-coach/
│   │   └── SKILL.md
│   └── workout-builder/
│       └── SKILL.md
└── fit-agent/
    ├── wellness/
    │   └── YYYY-MM.yaml           # daily rows, upserted by date
    ├── activities/
    │   └── YYYY-MM-DD.yaml        # multi-doc YAML, one doc per activity
    ├── planned-workouts/
    │   └── YYYY-MM-DD.md          # markdown + ```fit-workout``` fence
    └── .cache/
        ├── activities/
        │   ├── <icu-id>.json      # raw icu activity JSON
        │   └── <icu-id>.fit       # raw FIT file
        ├── wellness/
        │   └── YYYY-MM.json       # raw monthly wellness JSON
        ├── events/
        │   └── <icu-id>.json      # raw planned workout JSON
        └── athlete.json           # raw athlete JSON (zones, FTP, etc.)
```

### Ownership rules

| Path | Owner | Touched by `fetch` | Touched by `sync-workouts` |
|------|-------|--------------------|----------------------------|
| `ATHLETE-PROFILE.md` | agent | never | never |
| `TRAINING-PLAN.md` | agent | never | never |
| `README.md` | agent | never | never |
| `skills/**/SKILL.md` | agent (after init) | never | never |
| `fit-agent/wellness/*.yaml` | machine | yes (upsert by date) | no |
| `fit-agent/activities/*.yaml` | machine | yes (full rewrite) | no |
| `fit-agent/planned-workouts/YYYY-MM-DD.md` | shared | yes (rewrites only the machine block) | yes (writes back ids + refreshes machine block) |
| `fit-agent/.cache/**` | machine | yes | yes |

Planned-workout files are **jointly owned**: the agent owns the
frontmatter (including the `workouts:` list with `name`, `type`,
`moving_time_s`, and the stamped `icu_event_id`), the prose, and the
```fit-workout``` fences. The CLI owns a single fenced YAML block
delimited by HTML-comment sentinels:

```markdown
<!-- fit-agent:icu:begin -->
​```yaml
# Machine-managed: rewritten on every `fit-agent render planned`.
# Do not edit between the begin/end sentinels.
generated_at: 2026-05-03T20:14:00Z
source: intervals.icu
events:
  - icu_event_id: 12345
    name: "Z2 Endurance"
    type: Ride
    category: WORKOUT
    moving_time_s: 4500
    start_date_local: 2026-05-04T07:00:00
    description: |
      - 10m Z1
      - 60m Z2
      - 5m Z1
​```
<!-- fit-agent:icu:end -->
```

`render planned` and `sync-workouts` rewrite the machine block in place
and never touch any byte outside the sentinels. If a date has events
on the icu side but no `<date>.md` file yet, the CLI creates one with
default frontmatter (`kind: planned-workout-day`, the date, an empty
`workouts:` list) and the machine block, so the agent can fill in
prose and fences later.

### File anatomy (machine-owned files)

Machine-owned data files use **YAML** with a header comment block that
documents units and points at the corresponding `.cache/` files. Multi-doc
YAML (`---` separators) is used where a file holds N items
(e.g. multiple activities in a day).

Example `fit-agent/activities/2026-05-03.yaml`:

```yaml
# fit-agent activity day. Regenerated on every `fit-agent fetch`.
# Units: time in seconds (HH:MM:SS where labeled), distance in meters,
# speed in m/s, pace in sec/km, power in watts, HR in bpm.
# Source of truth: ../.cache/activities/<icu_id>.{json,fit}
date: 2026-05-03
generated_at: 2026-05-03T20:14:00Z
source: intervals.icu

---
icu_id: i12345
name: Track Intervals — 8 × 400m
type: Run
start_local: 2026-05-03T07:30:00+02:00
duration_s: 3120          # 52:00
moving_time_s: 2820       # 47:00
distance_m: 9800
tss: 78
rpe: 7
avg_hr: 156
max_hr: 182
athlete_notes: |
  Legs felt heavy, last 2 reps off pace.
laps:
  - i: 1
    type: warmup
    duration_s: 720
    distance_m: 2100
    avg_hr: 132
    avg_pace_sec_per_km: 343
  - i: 2
    type: interval
    step_name: rep 1
    duration_s: 84
    distance_m: 400
    avg_hr: 168
    avg_pace_sec_per_km: 210
  - i: 3
    type: rest
    duration_s: 90
    distance_m: 200
    avg_hr: 142
  # ...
intervals:
  - type: warmup
    duration_s: 720
  - type: work_set
    reps: 8
    work: { type: interval, target_pace_sec_per_km: 210 }
    rest: { type: rest, duration_s: 90 }

---
icu_id: i12346
name: Evening Strength
type: WeightTraining
start_local: 2026-05-03T18:00:00+02:00
duration_s: 1800
athlete_notes: ""
```

Example `fit-agent/wellness/2026-05.yaml`:

```yaml
# Daily wellness for 2026-05. Upserted by date on every `fit-agent fetch`.
# Source of truth: ../.cache/wellness/2026-05.json
month: 2026-05
generated_at: 2026-05-03T20:14:00Z
days:
  "2026-05-01":
    resting_hr: 48
    hrv_rmssd: 72
    sleep_hours: 7.4
    sleep_score: 84
    steps: 8421
    weight_kg: 74.2
    stress_avg: 28
    notes: ""
  "2026-05-02":
    resting_hr: 50
    hrv_rmssd: 65
    sleep_hours: 6.8
```

Example `fit-agent/planned-workouts/2026-05-04.md` (jointly owned —
agent writes frontmatter, prose, and the ```fit-workout``` fence; the
CLI rewrites only the sentinel-delimited YAML block):

```markdown
---
fit-agent:
  kind: planned-workout-day
  date: 2026-05-04
workouts:
  - name: "Z2 Endurance"
    type: Ride
    moving_time_s: 4500
    icu_event_id: null
---

## Z2 Endurance

Easy aerobic ride. Stay strictly in Z2; if HR drifts high in the second
half, ease off rather than push through.

​```fit-workout
- 10m Z1
- 60m Z2
- 5m Z1
​```

<!-- fit-agent:icu:begin -->
​```yaml
# Machine-managed: rewritten on every `fit-agent render planned`.
# Do not edit between the begin/end sentinels.
generated_at: 2026-05-03T20:14:00Z
source: intervals.icu
events: []
​```
<!-- fit-agent:icu:end -->
```

Two-tier model:

- **YAML data files** are the agent's primary input for activity/wellness
  numbers. Compact, typed, commented for units, regenerable.
- **`.cache/`** holds untouched icu JSON + raw `.fit`. Source of truth; YAML
  files can be regenerated from cache without re-fetching.
- **Markdown** is reserved for files where prose matters: the athlete
  profile, the README, skill manifests, and planned-workout intent.

---

## 5. Configuration & credentials

- Config file: `${XDG_CONFIG_HOME:-~/.config}/fit-agent/config.toml`, mode `0600`.
- API key stored in OS keyring by default (service `fit-agent`,
  account `<profile>`); falls back to `icu_api_key` in the config file with
  an explicit warning during `init` if keyring is unavailable.
- Schema:
  ```toml
  [profile.default]
  workspace = "/home/me/coaching"
  icu_athlete_id = "i12345"
  # icu_api_key only present when keyring is unavailable
  ```
- Workspace `.fit-agent.toml`:
  ```toml
  profile = "default"
  ```
- Profile selection: `--profile` flag, `FIT_AGENT_PROFILE` env, then the
  workspace `.fit-agent.toml`, then `default`.

---

## 6. intervals.icu client (`internal/icu`)

- HTTP Basic Auth: username `API_KEY`, password = the user's key.
- Base URL: `https://intervals.icu/api/v1`.
- v1 endpoints:
  - `GET /athlete/{id}` — profile, zones, FTP, LTHR, timezone.
  - `GET /athlete/{id}/activities?oldest=YYYY-MM-DD&newest=YYYY-MM-DD` — list.
  - `GET /activity/{id}` — single activity detail (JSON).
  - `GET /activity/{id}/fit-file` — raw FIT bytes (for lap/interval data).
  - `GET /athlete/{id}/wellness?oldest=...&newest=...` — daily wellness.
  - `GET /athlete/{id}/events?oldest=...&newest=...&category=WORKOUT` — planned.
  - `POST /athlete/{id}/events` — create planned workout.
  - `PUT  /athlete/{id}/events/{id}` — update planned workout.
  - `DELETE /athlete/{id}/events/{id}` — remove planned workout.
- Implementation notes:
  - Single `Client` struct with injected `*http.Client` (testable).
  - Exponential backoff on `429`/`5xx`, honor `Retry-After`.
  - Token-bucket limiter to stay polite during backfill.
  - Dates passed in athlete-local timezone (cached from `/athlete/{id}`).
  - `GetActivityFIT(id)` streams to `<cache>/activities/<id>.fit`.

---

## 7. FIT parsing (`internal/fitparse`)

Wraps `github.com/muktihari/fit` and exposes a small, agent-shaped struct:

```go
type ParsedActivity struct {
    Sport          string
    StartLocal     time.Time
    TotalTime      time.Duration
    MovingTime     time.Duration
    Distance       float64 // meters
    Laps           []Lap
    Intervals      []Interval // grouped from lap_trigger or workout_step_index
}

type Lap struct {
    Index         int
    Trigger       string  // "manual", "time", "distance", "workout_step", ...
    StepName      string  // from workout step if present
    StartLocal    time.Time
    Duration      time.Duration
    Distance      float64
    AvgHR, MaxHR  uint8
    AvgPower      uint16
    AvgCadence    uint8
    AvgSpeed      float64 // m/s
    AvgPaceSecPerKm int   // derived
}
```

- **Lap source**: `mesg_num=lap` records in the FIT file.
- **Interval grouping**: when the `.fit` file contains `workout_step` data
  (Garmin structured workouts), group laps by step. Otherwise heuristic
  based on `intensity` field (`active` vs `rest` vs `warmup`/`cooldown`).
- Streams (per-second power/HR/cadence) are **not** rendered to markdown —
  too noisy. They stay in `.fit` in `.cache/` and can be added on demand.

---

## 8. Commands

### 8.1 `fit-agent init`

Interactive (with `--non-interactive` for scripting):

1. Prompt: workspace directory (default `$PWD`). Create if missing.
2. Prompt: intervals.icu API key, with link to
   <https://intervals.icu/settings> → "Developer" section. Validate by
   calling `GET /athlete/0` (returns own athlete) and detecting athlete id.
3. Prompt: profile name (default `default`).
4. Store key in OS keyring (or fall back to `config.toml` 0600 with warning).
5. Write `~/.config/fit-agent/config.toml` with workspace + athlete id.
6. Write `<workspace>/.fit-agent.toml` (profile pointer).
7. Scaffold the workspace using `go:embed` templates (§3):
   - `ATHLETE-PROFILE.md` — template with placeholders
     (goals, training history, constraints, equipment, weekly availability).
   - `README.md` — explains the workspace and how the agent should use it.
   - `skills/training-plan-coach/SKILL.md`
   - `skills/training-session-coach/SKILL.md`
   - `skills/workout-builder/SKILL.md`
   - empty `fit-agent/{wellness,activities,planned-workouts,.cache/...}` dirs.
   - `.gitignore` excluding `fit-agent/.cache/` by default.
8. **Never overwrites agent-owned files** on re-run; prompts (or `--force`)
   per-file. Always safe to re-init for missing files.
9. Print next-step suggestion: `fit-agent fetch --since 30d`.

### 8.2 `fit-agent fetch`

```
fit-agent fetch [--from YYYY-MM-DD] [--to YYYY-MM-DD] [--since 30d]
                [--only activities|wellness|planned] [--force-refit]
                [--profile NAME] [--dry-run]
```

- Default range: last 14 days through today.
- `--since 30d` ≡ `--from today-30d --to today`.
- Pipeline per activity:
  1. Fetch activity JSON → `.cache/activities/<id>.json`.
  2. Download `.fit` file → `.cache/activities/<id>.fit` (skip if present
     and `--force-refit` not set).
  3. Parse `.fit` via `fitparse`.
  4. Render to markdown section, group by date.
- For each day in range, write `activities/YYYY-MM-DD.md` with all
  activities for that date as sections.
- Wellness: fetch the months touching the range, write
  `wellness/YYYY-MM.md`. Upsert by date row (read existing JSON block, merge
  by date, re-render). Cache raw JSON per month.
- Planned workouts: fetch `events?category=WORKOUT` in range; render to
  `planned-workouts/YYYY-MM-DD.md`. Cache raw event JSON per id.
- All writes are atomic (temp file + `os.Rename`).
- `--dry-run` prints diffs without writing.
- Summary output: `wellness +12, activities +5 (~2 changed), planned +3`.

### 8.3 `fit-agent sync-workouts`

```
fit-agent sync-workouts [--from YYYY-MM-DD] [--to YYYY-MM-DD]
                        [--prune] [--dry-run] [--profile NAME]
```

`sync-workouts` is the agent's primary workout-calendar command. It is
two-way: push first, then pull, so workouts the agent just authored come
back from the pull with their server-assigned id stamped into the
locally-authored file.

#### Push half (identical to the legacy `push-workouts`)

- Reads `planned-workouts/YYYY-MM-DD.md` files in range (the
  agent-authored frontmatter and ```fit-workout``` fences; the
  machine-owned sentinel block is ignored on push).
- Each file may declare 0..N workouts. Schema:
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

  ```fit-workout
  - 10m Z1
  - 60m Z2
  - 5m Z1
  ```
  ```
- For each workout:
  - `icu_event_id == null` → `POST` create, write returned id back.
  - `icu_event_id` set + body changed → `PUT` update.
  - Workout previously pushed but missing from markdown → require `--prune`
    before `DELETE`.
- `internal/workoutdsl` converts the ```fit-workout``` body to the
  intervals.icu workout-description string. The `workout-builder` skill
  teaches the agent the supported syntax.
- `--dry-run` prints the diff that *would* be sent, with the converted
  intervals.icu description.

#### Pull half

- After push, every WORKOUT-category event in range is fetched fresh from
  intervals.icu and written to `.cache/events/<id>.json`.
- The pull step then delegates to the same logic that drives
  `fit-agent render planned`: it groups cached events by local calendar
  date and rewrites the machine-owned sentinel block inside each
  `fit-agent/planned-workouts/YYYY-MM-DD.md` (creating the file with a
  default agent-owned skeleton when none exists).
- The machine block contains an `events:` list with every icu event for
  that date (id, name, type, category, moving_time_s, start_date_local,
  description). The agent's frontmatter, prose, and ```fit-workout```
  fences outside the sentinels are preserved byte-for-byte.
- There are no separate `.icu.md` mirror files; the combined file is
  the single representation of a planned day.

#### Backwards compatibility

- `fit-agent push-workouts` is retained as a hidden / deprecated alias
  that runs the push half only, so existing scripts and skill prompts
  keep working. New skills and docs target `sync-workouts`.

### 8.4 Atomic subcommands (debug & inspection)

The high-level `fetch` command is a composition of smaller, independently
useful steps. Each step is exposed as its own subcommand so they can be
debugged, scripted, or invoked by the agent when only part of the pipeline
is needed. They share flags (`--profile`, `--from`, `--to`, `--since`,
`--dry-run`) where applicable.

#### `fit-agent cache` — fetch raw payloads from intervals.icu

Writes only to `.cache/`; does not touch agent-facing YAML/markdown.

```
fit-agent cache activities  [--from ... --to ... --since ... --force-refit]
fit-agent cache activity    <icu-id>            # single activity (json + fit)
fit-agent cache wellness    [--from ... --to ...]
fit-agent cache events      [--from ... --to ...]   # planned workouts
fit-agent cache athlete                              # athlete profile json
fit-agent cache all         [--since 30d ...]        # everything in range
```

Output is a list of paths written, e.g.:
```
.cache/activities/i12345.json     (new)
.cache/activities/i12345.fit      (new)
.cache/activities/i12340.json     (unchanged)
```

#### `fit-agent render` — turn cached payloads into agent-facing files

Reads only from `.cache/`; does not call intervals.icu. Idempotent.

```
fit-agent render activities [--from ... --to ...]    # writes activities/*.yaml
fit-agent render activity   <icu-id>                  # one .yaml doc to stdout
fit-agent render wellness   [--month YYYY-MM | --from ... --to ...]
fit-agent render planned    [--from ... --to ...]    # planned-workouts/*.md
fit-agent render all        [--from ... --to ...]
```

Useful for:
- Iterating on the YAML schema without burning API calls.
- Re-rendering after a parser/template fix.
- Comparing rendered output against a golden file in tests.

`fit-agent render activity <id>` writing to stdout is the primary debug
loop: run `fit-agent cache activity i12345 && fit-agent render activity i12345`
and eyeball the YAML.

#### `fit-agent fit` — inspect a parsed FIT file

Pure FIT inspection; no network, no workspace I/O.

```
fit-agent fit summary <path-to.fit>     # overall session metrics
fit-agent fit laps    <path-to.fit>     # one row per lap, table or --json
fit-agent fit dump    <path-to.fit>     # all parsed messages, --json only
```

Lets you confirm `internal/fitparse` extracts the expected lap/interval
data before worrying about render output.

#### `fit-agent workout` — convert the DSL

```
fit-agent workout parse  <path-to.md|->        # ```fit-workout``` body -> json
fit-agent workout render <path-to.md|->        # -> intervals.icu description
fit-agent workout lint   <path-to.md>          # validate without converting
```

Used by tests, by the agent when iterating on a workout, and by humans
debugging unexpected push output.

#### `fit-agent fetch` — composition

`fit-agent fetch` is now defined as:

```
fetch = cache all   --from F --to T   [--force-refit]
      ; render all  --from F --to T
```

with one combined progress summary at the end. Equivalent to running both
manually; the convenience wrapper exists because it is the 95% path.

#### Common flags across subcommands

| Flag | Meaning |
|------|---------|
| `--profile NAME` | select profile from `config.toml` |
| `--from YYYY-MM-DD` | inclusive start date (athlete-local TZ) |
| `--to YYYY-MM-DD` | inclusive end date |
| `--since 30d` | sugar for `--from today-30d --to today` |
| `--dry-run` | print actions/diffs without writing |
| `--json` | machine-readable output (where it makes sense) |
| `--force-refit` | re-download `.fit` files already in cache |

---

## 9. Skill content (templates shipped with the CLI)

These are workspace-skill templates copied into `<workspace>/skills/<name>/`
on `init`. Each `SKILL.md` has the OpenClaw-required frontmatter
(`name`, `description`) and is plain markdown content otherwise.

The three skills form a coaching pipeline:

```
training-plan-coach  →  TRAINING-PLAN.md  →  workout-builder  →  planned-workouts/*.md
                                                                  ↑
                                              training-session-coach (daily adjustments)
```

- **`training-plan-coach/SKILL.md`** — Teaches the agent to interview the
  user about goals, target events, and time budget; pick a methodology
  (Jack Daniels, 80/20 polarized, Norwegian threshold/sub-threshold,
  base/build/peak periodization, etc.); and **produce
  `<workspace>/TRAINING-PLAN.md`**. The plan file contains:
  - athlete summary (goals, target event date, methodology chosen, why)
  - week-by-week structure (volume, intensity distribution, key sessions)
  - high-level description of each named workout type used in the plan
    (e.g. "Z2 Long Ride", "VO2max 5x4", "Threshold 2x20") *without* the
    detailed DSL — that is the workout-builder's job.

  Reads `ATHLETE-PROFILE.md` and recent weeks of
  `fit-agent/activities/*.yaml` + `fit-agent/wellness/*.yaml`. May update
  `ATHLETE-PROFILE.md` as goals/constraints change. Re-plans by editing
  `TRAINING-PLAN.md` in place.

- **`workout-builder/SKILL.md`** — Translates `TRAINING-PLAN.md` into
  concrete daily files in `fit-agent/planned-workouts/YYYY-MM-DD.md`,
  using the ```fit-workout``` DSL. Authoritative reference for the DSL:
  zones, durations, repetitions, ramps, free-text steps, examples and
  forbidden constructs. Teaches the agent to:
  - read `TRAINING-PLAN.md` and the existing
    `fit-agent/planned-workouts/*.md` to know what is already scheduled,
  - generate / regenerate the next N days of planned workouts,
  - adapt individual sessions on user request (skip, swap, shorten),
  - run **`fit-agent sync-workouts`** to push new or changed workouts to
    intervals.icu. The skill documents `--dry-run` and `--prune`.

- **`training-session-coach/SKILL.md`** — Day-of guidance. Reads recent
  wellness (HRV, sleep, RHR), today's planned-workout file, recent
  `fit-agent/activities/*.yaml`, and `TRAINING-PLAN.md` for context.
  Recommends adjustments (push, hold, deload, swap) and, when the user
  agrees, asks the workout-builder to rewrite the day's planned workout
  and push it.

`TRAINING-PLAN.md` is **not scaffolded by `fit-agent init`** — it does
not exist until the user runs the training-plan-coach skill. The
workspace `README.md` documents this so a fresh agent knows the file is
expected but optional.

Authoring the actual coaching prose for these three skills is
out-of-scope for the engineering plan; v1 ships skeletons with TODOs and
a working `fit-workout` DSL reference.

---

## 10. Format choice — why YAML for data, markdown for narrative

The first draft of this plan used markdown for everything. That was wrong
for the regenerated data files. Reasoning:

- **Activities and wellness are structured data**, not narrative. Their
  value is fields like `avg_hr`, `tss`, `laps[]`. Wrapping that in prose +
  markdown tables is a JSON object pretending to be a document.
- **Markdown tables are token-heavy** for 30-lap interval sessions and
  produce noisy diffs when regenerated.
- **Hand-editing is not a goal** for these files — `fetch` overwrites them.
- **LLMs read YAML at least as well as JSON or markdown**, and YAML stays
  human-readable in a plain editor.

Why YAML over alternatives:

| Format | Why not |
|--------|---------|
| Raw JSON | No comments, no room to inline units, painful to scan visually. |
| TOML | Awful for nested arrays of objects (lap lists). |
| JSON5 / JSONC | Less standard; tooling support is uneven. |
| NDJSON | Bad for nested structures like laps + intervals. |
| Markdown w/ tables | Token-heavy, lossy, noisy diffs when regenerated. |

YAML wins on:

- Comments → we document units once at the top of every file.
- Self-documenting keys → `avg_pace_sec_per_km` beats `"pace": 210`.
- Multi-document files (`---`) → natural fit for multi-activity days.
- Block scalars (`|`) → clean place for athlete notes.
- Cleaner regenerated diffs than markdown tables.

Markdown is kept for files where prose matters and the agent edits them:
`ATHLETE-PROFILE.md`, `README.md`, `skills/**/SKILL.md`, and
`planned-workouts/*.md` (where the agent expresses intent and the
```fit-workout``` fence carries the structured plan).

### Invariants

- YAML data files are fully owned by `fetch`; any prose the agent wants to
  add about a session goes in `ATHLETE-PROFILE.md` or in a freehand
  journal file the agent creates separately.
- The `athlete_notes` field in activity YAML is the icu-side `description`
  (athlete-authored on the icu web UI/app); it round-trips from the cache
  and is preserved across regeneration.
- `.cache/` is the ultimate source of truth — YAML can be rebuilt from
  cache without re-fetching (`fit-agent render` is a candidate post-v1
  helper).
- `sync-workouts` is the only command that pushes to icu. Its push half
  touches `fit-agent/planned-workouts/*.md` (to back-fill `icu_event_id`)
  and `.cache/events/*.json`; its pull half refreshes
  `.cache/events/*.json` and rewrites only the machine-owned sentinel
  block inside each `fit-agent/planned-workouts/*.md`. The deprecated
  `push-workouts` alias only runs the push half.

---

## 11. Testing strategy

- **Unit:** every package, table-driven.
- **Golden files:** `testdata/icu/*.json` + `testdata/fit/*.fit` →
  `testdata/workspace/*.{yaml,md}`. Tests render and `diff` against
  goldens. Regenerate with `go test -update`.
- **HTTP client:** `httptest.Server` for happy path, `429` backoff, `5xx`
  retry, malformed payloads, FIT-file streaming.
- **FIT parser:** smoke test against the sample `.fit` already at
  `~/icu/activity.fit` (copied into `testdata/fit/`). Verify lap count,
  interval grouping, and a few known averages.
- **Workout DSL:** round-trip property test for the supported subset;
  one-way fixture tests for markdown → icu description.
- **CLI:** `cmd/fit-agent` smoke-tested with `testscript`-style fixtures
  against a fake icu server.
- Coverage target: 80% on `internal/icu`, `internal/fitparse`,
  `internal/render`, `internal/workoutdsl`.

---

## 12. Implementation milestones

| # | Milestone | Outcome |
|---|-----------|---------|
| M0 | Repo scaffolding, CI, cobra skeleton | `fit-agent --help` works |
| M1 | `internal/config` (XDG + keyring) | round-trip config tests green |
| M2 | `internal/icu` (athlete, activities list/detail, wellness, events GETs) | curl-equivalent client |
| M3 | `internal/fitparse` (laps + interval grouping) | parses sample `.fit` to expected struct |
| M4 | `internal/workspace` + `internal/render` for activities + wellness | golden tests green |
| M5 | `init` command end-to-end | scaffolds a real workspace incl. skill templates |
| M6 | `fetch` command (activities + wellness + planned, with cache) | idempotent across reruns |
| M7 | `internal/workoutdsl` + `WORKOUT-BUILDER.md` reference | round-trip tests green |
| M8 | `sync-workouts` command (push then pull) | create/update/delete against icu + machine-owned icu block in each `planned-workouts/*.md` |
| M9 | Polish: rate-limits, `--dry-run` everywhere, error messages, docs | v1.0.0 release |

Post-v1:
- M10 polling daemon (`fit-agent serve` + `setup-service` / `remove-service`
  systemd-user wrappers). Webhooks were dropped: intervals.icu webhooks
  require an OAuth app and a public URL, neither of which fits the
  single-user, on-laptop deployment model. A polite 15-minute poll over
  `--since 2d` issues ~1-2 requests per minute against icu's 30 req/s
  ceiling and refreshes well before icu's own 60-second `ACTIVITY_ANALYZED`
  delay matters. **Status: shipped** (`internal/serveorch`,
  `internal/systemdunit`, `internal/cli/{serve,setup_service,remove_service}.go`).
- M11 OpenClaw webhook integration to notify the agent of new data.
- M12 Authored coaching prompts (replaces the TODO skeletons).

---

## 13. Dependencies

- `github.com/spf13/cobra` — CLI framework.
- `github.com/BurntSushi/toml` — config.
- `github.com/charmbracelet/huh` — interactive prompts.
- `gopkg.in/yaml.v3` — frontmatter.
- `github.com/muktihari/fit` — FIT decoding.
- `github.com/zalando/go-keyring` — keyring backend.
- Standard library for HTTP, time, fs.

No LLM SDKs. The CLI is a tool the agent calls; it does not call the agent.

---

## 14. Risks & mitigations

| Risk | Mitigation |
|------|------------|
| intervals.icu API drift / undocumented fields | Pin to documented endpoints; keep raw JSON cache so we can re-render without re-fetching. |
| FIT files with non-standard messages or developer fields | `muktihari/fit` supports unknown messages; we only read `lap`, `session`, `record`, `workout_step`. Unknown trigger → "unknown" string in markdown. |
| Workout DSL drift | Keep DSL behind a versioned `workoutdsl` package; round-trip tests; fail loudly on unknown tokens. |
| Agent edits a machine-owned YAML file by mistake | Files have `generated_at` + a header comment stating they are regenerated. Next `fetch` overwrites. Document this in the workspace `README.md`. |
| Rate limits on backfill | Token-bucket limiter; chunk by month. |
| Timezone bugs around midnight | Always use athlete-local TZ from `/athlete/{id}`; store ISO-8601 with offset in JSON blocks. |
| Secrets leaking into git | Keyring by default; workspace stores no secrets; `.gitignore` shipped at init. |
| `.cache/` size growth | `.fit` files ~100 KB each; manageable for years of data. Document a manual `find ... -mtime +N -delete` recipe; consider `fit-agent prune` post-v1. |

---

## 15. v1 task checklist

Tasks are grouped by milestone (§12). Each box is intended to be small
enough to ship behind one commit/PR. Order within a milestone is a
suggestion; cross-milestone parallelism is fine where there are no
dependencies.

### M0 — Repo scaffolding
- [x] `go mod init github.com/<owner>/fit-agent`, set Go version
- [x] `cmd/fit-agent/main.go` with cobra root command + `--version`
- [x] `Makefile` (or `taskfile`) with `build`, `test`, `lint`, `tidy`
- [x] `.golangci.yml` with reasonable defaults
- [x] `.github/workflows/ci.yml` running build, test, lint on push/PR
- [x] `LICENSE` (MIT) and minimal `README.md`
- [x] Pre-commit hook example (`gofmt`, `go vet`, `golangci-lint run`)

### M1 — `internal/config`
- [x] TOML schema + load/save with explicit `0600` permissions
- [x] XDG path resolution (`$XDG_CONFIG_HOME` fallback to `~/.config`)
- [x] Profile selection: flag → env (`FIT_AGENT_PROFILE`) → workspace `.fit-agent.toml` → `default`
- [x] Keyring backend via `zalando/go-keyring` (service `fit-agent`, account `<profile>`)
- [x] Fallback path: store key in TOML when keyring is unavailable, with explicit warning
- [x] Round-trip tests (write → read → assert equal)
- [x] Migration-safe loader (unknown keys ignored, missing keys defaulted)

### M2 — `internal/icu` HTTP client
- [x] `Client` struct with injected `*http.Client` and base URL
- [x] HTTP Basic Auth (`API_KEY` / user key)
- [x] `GET /athlete/{id}` (and `/athlete/0` self-resolve)
- [x] `GET /athlete/{id}/activities?oldest=&newest=`
- [x] `GET /activity/{id}` (single)
- [x] `GET /activity/{id}/fit-file` (streamed to caller-provided `io.Writer`)
- [x] `GET /athlete/{id}/wellness?oldest=&newest=`
- [x] `GET /athlete/{id}/events?oldest=&newest=&category=WORKOUT`
- [x] `POST /athlete/{id}/events`
- [x] `PUT /athlete/{id}/events/{id}`
- [x] `DELETE /athlete/{id}/events/{id}`
- [x] Exponential backoff on `429`/`5xx` honoring `Retry-After`
- [x] Token-bucket rate limiter
- [x] Typed error wrapping (`ErrUnauthorized`, `ErrNotFound`, `ErrRateLimited`)
- [x] Tests against `httptest.Server`: happy path, 401, 404, 429+retry, 5xx+retry, malformed JSON, FIT streaming

### M3 — `internal/fitparse`
- [x] Decode `.fit` via `muktihari/fit` into a `ParsedActivity` struct
- [x] Extract per-`lap` summaries (HR, power, cadence, pace, distance, trigger)
- [x] Group laps into `intervals[]` using `workout_step_index` when present
- [x] Heuristic interval grouping fallback using `intensity` + lap names
- [x] Derived fields: pace (sec/km) from `avg_speed`, moving time
- [x] Handle missing fields gracefully (no power on a run, no HR on a swim)
- [x] CLI helper `fit-agent fit summary|laps|dump <path>`
- [x] Test against `~/icu/activity.fit` copied into `testdata/fit/`

### M4 — `internal/workspace` + `internal/render`
- [x] Canonical path helpers (`Layout` struct: workspace root + all subpaths)
- [x] Atomic write helper (`temp + rename`, preserve mode)
- [x] YAML emitter for activity day (multi-doc) with header comment
- [x] YAML emitter for wellness month with header comment
- [x] Markdown emitter for planned-workout day (frontmatter + DSL fence)
- [x] Wellness upsert-by-date (read existing → merge → write)
- [x] Ownership guard: refuse to overwrite agent-owned files
- [x] Golden tests: `testdata/icu/*.json + testdata/fit/*.fit → testdata/workspace/*.{yaml,md}`
- [x] `go test -update` flag to regenerate goldens

### M5 — `init` command
- [x] Interactive prompts via `huh` (workspace, API key, profile name)
- [x] `--non-interactive` mode reading flags / env
- [x] API key validation via `GET /athlete/0`
- [x] Embedded templates via `go:embed` for `ATHLETE-PROFILE.md`, `README.md`, three `skills/<name>/SKILL.md` files
- [x] Write `<workspace>/.fit-agent.toml` profile pointer
- [x] Write `<workspace>/.gitignore` excluding `fit-agent/.cache/`
- [x] Per-file overwrite guard (prompt unless `--force`)
- [x] Re-init safe: missing files are recreated, present files are left alone
- [x] Smoke test: run `init --non-interactive` against a tmp dir, assert tree

### M6 — `cache` + `render` + `fetch` commands
- [x] `fit-agent cache athlete`
- [x] `fit-agent cache activities [range]` (list + per-activity json + fit)
- [x] `fit-agent cache activity <id>`
- [x] `fit-agent cache wellness [range]`
- [x] `fit-agent cache events [range]`
- [x] `fit-agent cache all [range]`
- [x] `fit-agent render activities [range]`
- [x] `fit-agent render activity <id>` (stdout)
- [x] `fit-agent render wellness [--month | range]`
- [x] `fit-agent render planned [range]`
- [x] `fit-agent render all [range]`
- [x] `fit-agent fetch [range]` = `cache all` + `render all`
- [x] `--dry-run` on every command above
- [ ] `--json` machine-readable output where applicable
- [x] `--force-refit` flag for cache activities
- [x] Combined progress summary (added/updated/unchanged counts)
- [x] Idempotency tests: run twice → second run reports zero changes

### M7 — `internal/workoutdsl`
- [x] Tokenizer + parser for the `fit-workout` DSL (durations, zones, reps, ramps, free-text)
- [x] AST → intervals.icu workout-description string
- [x] Round-trip property test for the supported subset
- [x] Fixture tests: known DSL → expected icu description
- [x] Validation errors with line/column pointers
- [x] `fit-agent workout parse|render|lint <path|->` CLI helpers
- [x] Document supported syntax in `workout-builder/SKILL.md` template

### M8 — `sync-workouts` command
- [x] Read all `planned-workouts/*.md` in range, parse frontmatter + DSL fences
- [x] Diff against `.cache/events/*.json`
- [x] `POST` for new (`icu_event_id == null`)
- [x] `PUT` for changed
- [x] `DELETE` for removed (only when `--prune`)
- [x] Write returned `icu_event_id` back into the markdown frontmatter
- [x] `--dry-run` prints the icu description that would be sent
- [x] End-to-end test against fake icu server (create → modify → delete)
- [x] Pull half: list events from icu, refresh `.cache/events/<id>.json`, and rewrite the machine-owned sentinel block inside each `planned-workouts/YYYY-MM-DD.md` (creating the file with a default skeleton when missing)
- [x] `push-workouts` retained as deprecated alias for the push half only

### M9 — Polish & release
- [ ] Coherent error messages (file path + actionable suggestion)
- [ ] `fit-agent doctor` (post-v1 candidate; minimal version: print resolved config + workspace + icu reachability)
- [ ] `--verbose` / `--quiet` flags wired through
- [ ] Version stamping via `-ldflags` on release build
- [ ] `goreleaser` config for cross-compiled binaries
- [ ] `README.md`: install, `init`, `fetch`, `sync-workouts`, examples
- [ ] `docs/workspace.md`: ownership rules, file formats, units
- [ ] `docs/workout-dsl.md`: DSL reference (mirrors the skill template)
- [ ] CI: tag-driven release workflow
- [ ] Tag `v0.1.0`

### Skill templates (parallel to milestones; ship by M9)
- [ ] `internal/templates/skills/training-plan-coach/SKILL.md` skeleton
- [ ] `internal/templates/skills/workout-builder/SKILL.md` with full DSL reference
- [ ] `internal/templates/skills/training-session-coach/SKILL.md` skeleton
- [ ] `internal/templates/ATHLETE-PROFILE.md.tmpl` with placeholders
- [ ] `internal/templates/README.md.tmpl` documenting ownership + commands

## Additional features to add
- [ ] config specifying if imperial or metric units are preferred. Initialized with init command
- [ ] workspace path specify as part of init command. Default shown is ~/.openclaw/workspace
- [ ] cross platform compatibility. Suppory MacOS and Windows.