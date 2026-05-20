# Elevation Gain / Loss Algorithm

This document describes how fit-agent computes per-segment elevation gain and
loss for auto-split segments. The same approach applies regardless of whether
the altitude data comes from a barometric sensor (Garmin, Polar, Suunto) or
from GPS-derived altitude (phone-based recording, GPS-only devices).

---

## The Problem

Raw altitude data from GPS and barometric sensors is noisy. A device recording
at 1 Hz on a completely flat surface will still show ±1–3 m of altitude
variation due to:

- **GPS vertical noise** — GPS satellites are distributed horizontally, so
  vertical accuracy is inherently worse than horizontal (~3–5× worse).
- **Barometric drift** — atmospheric pressure changes with weather and
  temperature; a barometer will drift even without any real elevation change.
- **Sensor lock-on** — barometric sensors need a warm-up period. The first
  30–60 seconds of a FIT record stream often show an apparent descent as the
  sensor converges to the true ground-level pressure.

If you naïvely sum every positive delta between consecutive records, a flat
10 km run will appear to have 100–200 m of elevation gain. This is why
Garmin, Strava, and other platforms all apply filtering before reporting
totals.

---

## Algorithm

fit-agent uses a two-step approach applied to the per-second FIT record stream.

### Step 1 — EWMA Smoothing (whole lap)

An **Exponentially Weighted Moving Average** (EWMA) is applied across the
entire lap's altitude stream before any bucketing into segments:

```
smoothed[0] = altitude[0]
smoothed[i] = α × altitude[i] + (1 − α) × smoothed[i−1]
```

`α = 0.1` (smoothing factor). Each new sample contributes 10 % of the
updated value; the previous smoothed estimate contributes 90 %. This
suppresses high-frequency noise while preserving real terrain shape.

**Why EWMA over a box (moving-average) filter?**

A centred box filter spreads artefacts symmetrically around the point of
interest. The sensor lock-on drift at run start (apparent descent of several
metres over the first 500 m) would be smeared forward into the first few
segments. EWMA is causal — it decays the initial error exponentially, so by
the time the sensor has stabilised (~50–100 records into the lap) the smoothed
value has converged to the true altitude.

**Why run it across the whole lap, not per segment?**

If the EWMA were reset at every segment boundary, each segment would start
with a stale initial value equal to the last sample of the previous segment's
raw altitude. Running a single pass over the whole lap means the smoothing
state is continuous and segment boundaries have no effect on accuracy.

### Step 2 — Per-segment Hysteresis

After smoothing, each segment receives the slice of smoothed points whose
cumulative distance falls within `[segStart, segEnd]`. A hysteresis filter
is applied to that slice:

```
committed ← first smoothed point in segment
for each subsequent point p:
    delta ← p.alt − committed
    if delta ≥ threshold:
        gain += delta
        committed ← p.alt
    else if delta ≤ −threshold:
        loss += |delta|
        committed ← p.alt
```

The **threshold** is chosen based on the altitude data source:

| Source | Threshold | Rationale |
|--------|-----------|-----------|
| Barometric (`EnhancedAltitude` field present) | **2 m** | Matches Strava's stated threshold for barometric data; barometric sensors are accurate to ~0.5 m relative, so 2 m is conservative enough to reject noise while catching real terrain |
| GPS-only (`Altitude` field, no `EnhancedAltitude`) | **8 m** | GPS vertical accuracy is typically ±5–15 m; 8 m rejects noise while still detecting meaningful climbs on hilly terrain |

The source is detected automatically from the FIT record fields:
- `EnhancedAltitude` present → barometric
- Only `Altitude` present → GPS-derived

---

## Behaviour on Edge Cases

### No altitude data
If no records in the lap have valid altitude, `elevation_gain_m` and
`elevation_loss_m` are omitted from the segment YAML entirely.

### GPS-only on flat terrain
With an 8 m threshold on a flat urban run, most or all segments will report
no gain/loss. This is the correct and honest result — GPS-only altitude cannot
reliably distinguish 2–3 m undulations from noise.

### Sensor lock-on at run start
The EWMA smoothing handles this naturally. The first segment may still show
a small apparent loss as the barometric sensor settles, but the magnitude is
significantly reduced compared to raw summing. This is a known limitation of
any purely sensor-based approach; DEM correction (not implemented) would
eliminate it entirely.

### Short segments (remainder tails)
The algorithm treats remainder segments identically to full-length segments.
If there are fewer smoothed points than the hysteresis threshold requires,
zero gain/loss is reported — which is correct for very short tails.

---

## Comparison with Platform Approaches

| Platform | Method |
|----------|--------|
| **Garmin (device)** | Proprietary on-device filter; result stored in FIT `TotalAscent`/`TotalDescent` lap fields. Accurate but opaque and not always present for all lap types. |
| **Strava** | Smoothing + threshold: 10 m for GPS-only, 2 m for barometric. Applied to the whole activity, not per segment. |
| **RideWithGPS** | "Mathematical smoothing then point-to-point delta summation" (their words). Threshold undisclosed. |
| **fit-agent** | EWMA (α=0.1) over whole lap → per-segment hysteresis (2 m barometric, 8 m GPS). Does not rely on pre-computed device totals. |

---

## Configuration

`auto_split_distance` controls segment size. The elevation algorithm is
applied automatically whenever auto-splits are generated. See
[configuration reference](../README.md) for `auto_split_distance` syntax.
