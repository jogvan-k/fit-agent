---
name: fit-agent-coach
description: "Fitness coaching with FitAgent and Intervals.icu: initialise a workspace, inspect wellness and activity data, build safe training plans, schedule structured workouts, and sync them. Use when the athlete asks to coach, plan, adjust, or review training through FitAgent."
---

# FitAgent coach

Use the `fit-agent` CLI to keep a local coaching workspace synchronised with
[Intervals.icu](https://intervals.icu). The CLI is deterministic: it does not
call an LLM. This skill provides the coaching workflow around it.

## Prerequisites

1. Confirm that `fit-agent --version` succeeds. If it is missing, install it:

   ```sh
   brew tap jogvan-k/tap
   brew install fit-agent
   ```

   Or, with Go 1.25 or later:

   ```sh
   go install github.com/jogvan-k/fit-agent/cmd/fit-agent@latest
   ```

2. Work in the athlete's intended OpenClaw workspace. Initialise FitAgent once:

   ```sh
   fit-agent init
   ```

   Let the athlete enter their Intervals.icu API key through the interactive
   prompt. Never ask them to paste an API key into chat or a workspace file.

3. Refresh data before giving coaching advice:

   ```sh
   fit-agent fetch --since 90d
   ```

## Data ownership

Treat these as **machine-owned**; inspect but do not edit them:

- `fit-agent/activities/*.yaml`
- `fit-agent/wellness/*.yaml`
- `fit-agent/.cache/**`
- the `<!-- fit-agent:icu:begin -->` / `end` block in planned-workout files

The athlete and coach own:

- `fit-agent/ATHLETE-PROFILE.md`
- `fit-agent/TRAINING-PLAN.md`
- the authored frontmatter, prose, and `fit-workout` blocks in
  `fit-agent/planned-workouts/YYYY-MM-DD.md`

## Coaching workflow

### Build or revise a plan

1. Read `ATHLETE-PROFILE.md`, recent wellness, recent activities, and any
   existing `TRAINING-PLAN.md`.
2. Ask only for missing goals, event dates, constraints, injury context, or time
   budget; do not invent fitness markers.
3. Write or revise `TRAINING-PLAN.md` with an athlete summary, methodology,
   week-by-week structure, named workout types, and open questions.
4. Keep macro-plan work separate from daily workout prescription.

### Make a day-of recommendation

1. Read the most recent seven days of wellness and activity data, today's
   planned workout, and the macro plan.
2. Compare HRV, resting HR, sleep, recent RPE, and ramp rate with the athlete's
   normal baseline—not generic absolute numbers.
3. Recommend one of: **go ahead**, **hold**, **swap**, **deload**, or
   **cancel**. Give the short evidence for the choice.
4. Do not alter the macro plan for a one-day adjustment. Repeated daily changes
   mean the macro plan needs review.

### Schedule a structured workout

1. Read the plan, athlete profile, and existing planned-workout files.
2. Create or edit `fit-agent/planned-workouts/YYYY-MM-DD.md`. Use one `workouts:`
   frontmatter entry and one matching `##` section per workout.
3. Validate DSL before syncing:

   ```sh
   fit-agent workout parse path/to/workout.fit-workout
   ```

   Use the native `description:` frontmatter field when a required
   Intervals.icu feature cannot be represented by the DSL.
4. Always preview a calendar change first:

   ```sh
   fit-agent sync-workouts --from YYYY-MM-DD --to YYYY-MM-DD --dry-run
   ```

5. Explain the diff. After the athlete agrees—or if their standing preference
   explicitly permits push after a successful preview—sync it:

   ```sh
   fit-agent sync-workouts --from YYYY-MM-DD --to YYYY-MM-DD
   ```

6. Read the file again or inspect the generated machine-owned block to verify
   the event ID and returned workout state.

## Structured-workout conventions

- Use `mtr` or `km` for metres/distance; bare `m` means minutes.
- Use `Pace` after absolute or zone pace targets, e.g. `- 15m 4:30 Pace` or
  `- 10km Z3 Pace`.
- Use `intensity=recovery` for recovery steps when it must reach Garmin or
  Intervals.icu as recovery.
- Use the label before the duration for named steps, e.g.
  `- Warm-up 10m Z1-Z2 Pace`.
- Use a blank-line-delimited `3x` block for repeats with multiple steps.

## Safety and quality checks

- Use `--dry-run` before a sync that changes remote workouts.
- Never overwrite a planned workout that the athlete authored elsewhere without
  showing the conflict and asking which version should win.
- Do not make medical diagnoses or prescribe training through injury, acute
  illness, chest symptoms, fainting, or severe unexplained fatigue. Recommend
  clinical assessment where appropriate.
- Keep API keys and raw private activity data out of chat, commits, and skill
  output.
