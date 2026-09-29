# kaynakkestirir

Estimate the next code measurement — how big a file, or the whole repository,
is about to get — and say how confident the estimate is.

`kaynakkestirir` reads a stream of timestamped size measurements taken from a
repository's history (for example one record per commit per file), learns the
typical magnitude of each scope over a trailing window, and reports:

- a **point estimate** for the next measurement, blending a median with a
  geometric mean so that a single huge commit cannot drag the estimate with it;
- an **uncertainty band** derived from a robust spread, so a caller can see when
  the history is too regular to say anything useful;
- **moving average** figures alongside the point estimate, because the difference
  between a robust level and a plain average is itself informative;
- **anomaly alerts** for observations that fell outside the band implied by the
  observations before them.

Everything is computed per scope: one prediction per file, or one for the
repository as a whole. The default scope is per file, because "how big will
`src/app.py` be" is a question you can act on, and "how big will the repo be" is
usually a question with a much vaguer answer.

## Build

```sh
go build -o kaynakkestirir .
```

Go 1.21 or newer. The standard library is the only dependency: the median, MAD,
geometric mean, SMA and EMA are all implemented in this repository, so the
numbers do not shift under you when a third-party library changes a rounding
detail.

## Input

JSONL, one measurement per line:

```json
{"t": "2026-01-05T10:00:00Z", "file": "src/app.py", "bytes": 2048, "loc": 120}
{"t": 1767225600, "file": "src/util.py", "bytes": 1024}
```

- `t` — an ISO 8601 timestamp, or epoch seconds as a JSON number. Timestamps
  without a zone are read as UTC; everything is normalised to UTC.
- `file` — required, non-empty.
- `bytes` — required, a non-negative number. Anything else is a **record error**:
  the line is counted, reported and skipped, and the run exits 1.
- `loc` — optional. A record without a line count is not an error; it is simply
  skipped when the LOC series is selected with `--metric loc`.

Records may arrive in any order: they are sorted by timestamp, and records
sharing a timestamp keep their input order.

## Example

A history of a few files over roughly three months — `src/app.py` grows slowly,
then picks up a vendored block on 2026-02-09, and `CHANGELOG.md` barely moves:

```jsonl
{"t": "2025-11-03T09:12:00Z", "file": "src/app.py", "bytes": 18420, "loc": 512}
{"t": "2025-11-17T14:03:00Z", "file": "src/app.py", "bytes": 19105, "loc": 528}
{"t": "2025-12-01T11:41:00Z", "file": "src/app.py", "bytes": 18880, "loc": 519}
{"t": "2025-12-15T16:20:00Z", "file": "src/app.py", "bytes": 19940, "loc": 551}
{"t": "2026-01-05T10:00:00Z", "file": "src/app.py", "bytes": 19710, "loc": 544}
{"t": "2026-01-19T09:55:00Z", "file": "src/app.py", "bytes": 20480, "loc": 566}
{"t": "2026-02-02T13:30:00Z", "file": "src/app.py", "bytes": 20120, "loc": 555}
{"t": "2026-02-09T08:15:00Z", "file": "src/app.py", "bytes": 41960, "loc": 1102}
{"t": "2026-02-16T15:40:00Z", "file": "src/app.py", "bytes": 20630, "loc": 570}
{"t": "2025-11-03T09:12:00Z", "file": "src/parser.py", "bytes": 7320, "loc": 214}
{"t": "2025-12-01T11:41:00Z", "file": "src/parser.py", "bytes": 7890, "loc": 231}
{"t": "2026-01-05T10:00:00Z", "file": "src/parser.py", "bytes": 8010, "loc": 234}
{"t": "2026-02-16T15:40:00Z", "file": "src/parser.py", "bytes": 8140, "loc": 238}
{"t": "2026-01-19T09:55:00Z", "file": "CHANGELOG.md", "bytes": 2410, "loc": 96}
{"t": "2026-02-02T13:30:00Z", "file": "CHANGELOG.md", "bytes": 2680, "loc": 104}
```

```console
$ kaynakkestirir --window 6 --z 2 history.jsonl
kaynakkestirir 1.0.0  scope=file metric=bytes window=6 alpha=0.5 z=2 ema-span=5 horizon=1 drop-anomalies=false
records: 15 read, 15 accepted, 0 without bytes, 0 rejected

STATUS  SCOPE               N  MEDIAN  GEOMEAN    CENTER     BAND                    SMA        EMA
ok      file:CHANGELOG.md   2  2545    2541.417   2543.208   [2142.906, 2943.51]     2545       2500
ok      file:src/app.py     6  20300   22792.086  21546.043  [20523.049, 22569.037]  23806.667  25128.189
ok      file:src/parser.py  4  7950    7833.597   7891.799   [7521.149, 8262.449]    7840       7831.111

scopes: 3 (3 predicted, 0 insufficient)

anomalies: 2

  file:src/app.py: 2
    ANOMALY t=2025-12-15T16:20:00Z file=src/app.py v=19940 outside [18172.578, 19506.918]
    ANOMALY t=2026-02-09T08:15:00Z file=src/app.py v=41960 outside [18352.977, 21169.917]
```

The vendored block on 2026-02-09 is the one that stands out, and the alert says
so with the band that the six preceding weeks implied. Note that the reported
`src/app.py` centre (21546) is nowhere near the 41960-byte spike: the robust
estimator is not being dragged by it. The SMA (23806) and the EMA (25128) are,
which is exactly why all three are printed.

## Usage

```
kaynakkestirir [flags] <history.jsonl>
```

The input may also be given with `-input PATH`, or read from stdin with `-input -`.
Flags may appear before or after the path.

| flag | default | meaning |
| --- | --- | --- |
| `-window N` | 30 | trailing window size, minimum 2 |
| `-alpha A` | 0.5 | blend of median vs geometric mean, 0..1 |
| `-z Z` | 1.0 | band multiplier applied to the robust spread |
| `-ema-span S` | 5 | EMA span, `alpha = 2/(span+1)` |
| `-horizon H` | 1 | how many steps the flat prediction is repeated for |
| `-scope S` | `file` | `file` (per file) or `repo` (per-timestamp sum) |
| `-metric M` | `bytes` | `bytes` or `loc` |
| `-drop-anomalies` | off | recompute the fit excluding flagged observations |
| `-json` | off | emit the machine-readable report |
| `-version` | | print the version and exit |

Exit codes: `0` success, `1` at least one input record was rejected (the report
is still produced in full), `2` usage error (missing input, unreadable input,
`--window` below 2, an unknown scope or metric, an out-of-range flag) — no
report is produced in that case.

A scope holding fewer than two usable observations is reported as
`insufficient`. That is a normal, expected outcome, not an error: the run still
exits 0 and the scope still appears in the table.

### JSON output

```console
$ kaynakkestirir --scope repo --window 4 --horizon 2 --json history.jsonl
```

```json
{
  "scopes": [
    {
      "scope": "repo",
      "key": "repo",
      "status": "ok",
      "window": {
        "n": 4,
        "median": 25830,
        "geomean": 28173.38274022896,
        "geomean_excluded": 0,
        "center": 27001.69137011448,
        "spread": 4425.561,
        "degenerate": false,
        "low": 22576.13037011448,
        "high": 31427.252370114482,
        "sma": 29105,
        "ema": 29074.444444444445,
        "observed": 9
      },
      "predictions": [
        {
          "step": 1,
          "center": 27001.69137011448,
          "low": 22576.13037011448,
          "high": 31427.252370114482
        },
        {
          "step": 2,
          "center": 27001.69137011448,
          "low": 22576.13037011448,
          "high": 31427.252370114482
        }
      ],
      "anomalies": [
        {
          "step": 4,
          "t": "2025-12-15T16:20:00Z",
          "scope": "repo",
          "v": 19940,
          "center": 24676.0510353381,
          "low": 23148.9730353381,
          "high": 26203.1290353381,
          "dropped": false
        },
        {
          "step": 5,
          "t": "2026-01-05T10:00:00Z",
          "scope": "repo",
          "v": 27720,
          "center": 22737.546349730957,
          "low": 17819.020849730958,
          "high": 27656.071849730957,
          "dropped": false
        },
        {
          "step": 8,
          "t": "2026-02-09T08:15:00Z",
          "scope": "repo",
          "v": 41960,
          "center": 23010.13372913287,
          "low": 20823.29872913287,
          "high": 25196.96872913287,
          "dropped": false
        }
      ]
    }
  ],
  "settings": {
    "window": 4,
    "alpha": 0.5,
    "z": 1,
    "ema_span": 5,
    "horizon": 2,
    "scope": "repo",
    "metric": "bytes",
    "drop_anomalies": false,
    "input": "history.jsonl"
  },
  "records": {
    "total": 15,
    "accepted": 15,
    "skipped_no_metric": 0,
    "errors": 0
  },
  "record_errors": [],
  "anomaly_total": 3
}
```

Every timestamp is ISO 8601 in UTC. `geomean` is `null` when the window
contained no positive value. An `insufficient` scope carries `"window": null` or
a one-observation window, an empty `predictions` list, and `"status":
"insufficient"`.

## Design notes

### Scope semantics

- `--scope file` (default) — one series per file path, holding that file's
  measurements in time order. The series are reported in sorted path order, so
  the output is stable no matter what order the input arrived in.
- `--scope repo` — **one** series, built by summing the metric across all files
  at each timestamp. Measurements sharing a timestamp are folded into a single
  observation, so a commit that touches five files contributes one repository
  observation, not five. The repository series therefore has one point per
  distinct timestamp, and its values are totals, which makes it the right input
  for "how big is a release" and the wrong input for "how big is this file".

### Model

Over the trailing window of the `N` most recent observations of a scope:

```
median  = median(window)
geomean = (product of the strictly positive window values) ^ (1 / count)
center  = alpha * median + (1 - alpha) * geomean
spread  = 1.4826 * MAD(window)        where MAD = median(|x - median|)
band    = [center - z * spread, center + z * spread]
sma     = mean(window)
ema     = seeded with the first window value, then e += alpha_ema * (x - e)
          with alpha_ema = 2 / (ema-span + 1)
```

- The **geometric mean** is taken over values `> 0` only. Zeros and negatives
  cannot enter a product, so they are excluded and the count is reported
  (`geomean_excluded`) rather than silently clamped. If no positive value
  exists at all, the geometric mean is undefined: `geomean` is reported as
  `null` and the model falls back to the median alone.
- The **median** and the **geometric mean** answer different questions. The
  median describes a typical measurement; the geometric mean describes a
  typical *magnitude*, which is what "how big is this file" usually means, and
  which stays in the units of the input. `alpha` moves between them: `1` is a
  pure median, `0` a pure geometric mean.
- `1.4826 * MAD` is the standard conversion from a median absolute deviation to
  an estimate of the standard deviation. It is used instead of the ordinary
  standard deviation because one vendored file should not be able to widen the
  band that is supposed to catch it.
- A **degenerate** window — one whose MAD is exactly 0, typically because more
  than half the observations are identical — has no measurable dispersion. The
  band collapses to `[center, center]` rather than claiming a precision the
  data cannot support, and the report flags it with `degenerate: true`.

### Anomaly policy

An observation is an anomaly when it falls outside the band implied by the
observations that came **before** it. The current observation never takes part
in the band that judges it; otherwise an outlier would widen the very window
that is supposed to expose it.

- An observation is only judged once two earlier observations exist. A band
  needs two points before it has a spread at all.
- Flagged observations **stay in the window** by default. An outlier is a fact
  about the repository, not a parsing error, and silently deleting it would let
  a single bad measurement quietly reshape the model. `--drop-anomalies`
  recomputes the fit without them, and marks each anomaly `"dropped": true`.
  Note that the anomaly list itself is computed before any dropping, so the
  two flags do not feed back into each other.
- Because the judgement is one-sided in time, a single outlier can also make
  the observation *after* it look anomalous: the outlier skews the centre
  upwards, and the following ordinary value can fall below the new band. This
  shows up in the report as two adjacent alerts, and it is intentional.
- One honest limitation: a steadily trending file on a short window will trip
  the alert repeatedly, because each step grows by more than one robust spread.
  The remedy is a wider `--window` or a larger `--z`, not a different
  estimator.

### The horizon is flat, and that is a deliberate limit

`--horizon H` repeats the same prediction H times. This is a robust *level*
estimator, not a trend model: it answers "given how this file has behaved
recently, how big is the next measurement likely to be", and it makes no claim
about the direction of travel. A file that grew 5% a month will be predicted at
its recent level, not extrapolated upward — a forecast that compounds an
observed rate over a long horizon is a much stronger claim than a handful of
measurements can support, and this tool does not make it. The band widens with
`z` but not with the horizon, which is a second honest signal that the model has
nothing to say about step 7 that it does not say about step 1.

### Numbers and determinism

- Every statistic is hand-rolled in `stats/`; the tool depends on nothing
  outside the standard library.
- The mean and the geometric mean are computed with numerically stable forms
  (incremental mean; accumulation in log space) so that long windows of
  byte-sized values do not lose precision, and a constant sample returns its
  constant exactly rather than an ulp away from it.
- The anomaly test allows a relative slack of `1e-9` before it calls a value
  outside the band, so last-bit rounding never manufactures an alert.
- Sorting is stable, series and report order are deterministic, and no map
  iteration order reaches the output: the same input always produces
  byte-identical output.

### Layout

```
main.go            thin: parses nothing, chooses the exit code
cli/               flag parsing, input handling, human and JSON reports
model/             records, scopes, window fit, band, anomalies
stats/             median, MAD, geometric mean, SMA, EMA
```

## Tests

```sh
gofmt -l .
go vet ./...
go test -race -count=1 ./...
```

The suite is 24 tests covering both/even medians, MAD on constant and known
series, geometric mean units and its exclusion counting, the blend at known
`alpha`, band arithmetic and the degenerate case, hand-computed SMA and EMA
fixtures, trailing-window semantics, unsorted and tied input, epoch and ISO
timestamps, per-file grouping, repository summation, anomaly detection on
constructed outliers, `--drop-anomalies`, insufficient scopes, the JSON shape,
record-error collection, every usage exit path, stdin input, and byte-identical
output across repeated runs. Fixtures live in `t.TempDir()` and the expected
values are computed by hand.
