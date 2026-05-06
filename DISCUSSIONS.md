# CONSIDERATIONS.md

Open questions and design decisions made under uncertainty. Each entry
notes the call I made so work could continue, and what evidence we'd
want before locking it in.

---

## C1 — `WellnessDay` field names vs intervals.icu reality

**Status:** unverified against the live API.

`internal/icu/types.go` declares the wellness JSON shape with these
field names: `restingHR`, `hrv`, `hrvSDNN`, `sleepSecs`, `sleepScore`,
`steps`, `weight`, `avgStress`, `comments`. These were chosen by
guessing at intervals.icu's camelCase convention.

The intervals.icu wellness endpoint may use different keys. The cookbook
at <https://forum.intervals.icu/t/intervals-icu-api-integration-cookbook/80090>
should be consulted, and ideally a single live `GET /athlete/{id}/wellness`
response should be inspected to confirm.

**Risk if wrong:** numeric fields silently decode to zero and the
rendered YAML would show empty days.

**Mitigation in place:** the cache stores the raw JSON, so we can
re-render at any time without losing data once the field names are
fixed.

---

## C2 — `ActivitySummary.Description` mapping to "athlete notes"

**Status:** assumed.

The plan §10 ("File anatomy") names `athlete_notes` as "the icu-side
`description` (athlete-authored on the icu web UI/app)". I mapped
`Description` from the ActivitySummary JSON to `athlete_notes` in the
YAML, but it is not 100% confirmed which icu field carries the user's
prose. Possible alternatives: `notes`, `commute_description`,
`workout_doc`.

**Action:** confirm against a real activity that has prose in the icu
web UI's description field.

---

## C3 — Lap-level workout step name

`fitparse.Lap` does not currently expose the workout step name (e.g.
"rep 1") because the FIT decoder I'm using exposes laps via
`mesgdef.Lap` but workout steps live in separate `workout_step`
messages joined by `wkt_step_index`. The plan §10 example shows
`step_name: rep 1` on lap rows.

**Decision:** ship M4 without step_name; add it in M3-followup or M6 if
a sample FIT actually carries `workout_step` messages with names. The
`testdata/fit/sample-intervals.fit` is an unstructured run so step
names are not exercised yet.

---

## C4 — Activity summary fields are a small subset of icu's 174

`internal/icu/types.go` only types ~17 fields of the activity JSON. The
agent gets the rest via the raw cache `.cache/activities/<id>.json`,
which is the design (§14, "intervals.icu API drift" mitigation). If the
agent needs more typed fields later, add them to `ActivitySummary` and
plumb to render.

---

## C5 — `formatFloat` keeps a trailing `.0`

`formatFloat(9800, 1)` returns `"9800.0"` rather than `"9800"`. The
intent was to keep distance and elevation fields visually distinct from
integers; YAML decodes both as numbers either way, so this is a style
choice. If the agent prefers compact ints in YAML, drop the trailing-
zero policy.

---

## C6 — Init flow needs the icu athlete id

The plan §8.1 says `init` validates the API key by calling
`GET /athlete/0` (which returns the caller's own athlete) and detects
the id. As of M4 the `icu` client has no `/athlete/0` shortcut wired
into `init`; that work belongs in M5 and the cookbook recipe should be
re-checked there.

---

## C8 — Interval grouping heuristic includes warmup in work_set

The fitparse heuristic for FIT files without `workout_step` messages
collapses adjacent active/recovery laps into one `work_set`. On a real
intervals.icu activity (id i146012960, 5×1.6km repeats), the warmup
lap (active, ~10min, no recovery before it) got folded into the
work_set rather than emitted as a separate `warmup` interval, because
its `intensity` is `active` not `warmup`. The lap’s long duration is
the only signal distinguishing it.

**Decision:** acceptable for v1; the per-lap data is correct and the
agent can re-derive grouping from it. A smarter heuristic
(distance/duration outliers, first-lap is usually warmup) is a follow-
up. Track here so we revisit during M9 polish.

---

## C9 — `ActivitySummary.Description` is null on every probed activity

intervals.icu returns `description: null` for all 6 activities in the
last 7 days of the test account. We cannot confirm yet whether
prose-on-activity uses `description` or some other field; the
renderer's `athlete_notes` mapping is unverified. Will re-check during
M6 with an activity the user has actually annotated.

---

## C10 — `file_type` instead of `has_fit_file`

intervals.icu does not return `has_fit_file`. The presence of a FIT
file is signalled by `file_type == "fit"`. Updated `ActivitySummary`
accordingly. Other plausible values: `"tcx"`, `"gpx"`, empty string
for manually-entered activities.

---

## C11 — Wellness `sleepScore` returns as float

intervals.icu returns `sleepScore: 90.0` (float), not `90` (int).
Wellness numeric fields are now typed as `float64` to accept both;
renderer formats them via `formatFloat` (so we get `sleep_score: 90.0`
in YAML).

---

## C12 — Many wellness fields are typically null

In the probed 7-day window, only ~10 of the ~50 wellness fields are
populated per row. The renderer correctly omits zero-valued fields.
Adding more typed fields (`vo2max`, `bodyFat`, `ctl`, `atl`,
`rampRate`, `sleepQuality`, `avgSleepingHR`) was straightforward and
they appear when the data carries them.

---

## C7 — Empty wellness day rendering

`render.WellnessMonthYAML` emits an empty mapping under the date key
when no fields are populated:

```yaml
"2026-05-04":
```

That is valid YAML (decodes to `nil`), but some YAML linters complain.
If it bothers the agent we can skip empty rows entirely or emit an
explicit `notes: ""`. Current call sites would not normally produce a
fully empty row because the icu wellness endpoint always returns at
least one field per emitted day.
