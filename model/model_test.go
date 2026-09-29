package model

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"kaynakkestirir/stats"
)

const tol = 1e-9

func closeTo(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > tol*math.Max(1, math.Abs(want)) {
		t.Fatalf("got %v, want %v (tolerance %g)", got, want, tol)
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad fixture time %q: %v", s, err)
	}
	return ts.UTC()
}

func parse(t *testing.T, input string) ParseResult {
	t.Helper()
	return ParseJSONL(strings.NewReader(input))
}

func TestParseValidRecords(t *testing.T) {
	input := strings.Join([]string{
		`{"t": "2026-01-05T10:00:00Z", "file": "src/app.py", "bytes": 2048, "loc": 120}`,
		`{"t": 1767225600, "file": "src/util.py", "bytes": 1024}`,
		`{"t": "2026-01-05T12:00:00+02:00", "file": "src/app.py", "bytes": 2048.0, "loc": 121}`,
		``,
		`   `,
	}, "\n")

	res := parse(t, input)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if res.Total != 3 {
		t.Fatalf("Total = %d, want 3 (blank lines are not records)", res.Total)
	}
	if len(res.Records) != 3 {
		t.Fatalf("got %d records, want 3", len(res.Records))
	}

	first := res.Records[0]
	if !first.T.Equal(mustTime(t, "2026-01-05T10:00:00Z")) {
		t.Fatalf("T = %v", first.T)
	}
	if first.File != "src/app.py" || first.Bytes != 2048 {
		t.Fatalf("record 0 = %+v", first)
	}
	if first.Loc == nil || *first.Loc != 120 {
		t.Fatalf("loc = %v, want 120", first.Loc)
	}

	// Epoch seconds are read as UTC.
	if got := res.Records[1].T; !got.Equal(time.Unix(1767225600, 0).UTC()) {
		t.Fatalf("epoch timestamp parsed as %v, want 2026-01-01T00:00:00Z", got)
	}
	// A missing line count is legal: the record is unusable for LOC series only.
	if res.Records[1].Loc != nil {
		t.Fatalf("expected nil loc, got %v", *res.Records[1].Loc)
	}
	if !res.Records[1].HasMetric(MetricBytes) || res.Records[1].HasMetric(MetricLoc) {
		t.Fatal("a record without loc must be usable for bytes but not for loc")
	}
	// An offset timestamp is normalised to UTC.
	if got, want := res.Records[2].T, mustTime(t, "2026-01-05T10:00:00Z"); !got.Equal(want) {
		t.Fatalf("offset timestamp parsed as %v, want %v", got, want)
	}
	if res.Records[2].Line != 3 {
		t.Fatalf("Line = %d, want 3: line numbers count every scanned line", res.Records[2].Line)
	}
}

func TestRecordErrors(t *testing.T) {
	t.Run("rejected lines are reported", func(t *testing.T) { recordErrorCases(t) })
	t.Run("a rejected line does not stop the stream", func(t *testing.T) { recordErrorStream(t) })
}

func recordErrorCases(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"malformed JSON", `{"t": "2026-01-05T10:00:00Z", "file": `, "malformed JSON"},
		{"not an object", `[1, 2, 3]`, "malformed JSON"},
		{"missing timestamp", `{"file": "a.py", "bytes": 1}`, `missing "t"`},
		{"unparseable timestamp", `{"t": "last tuesday", "file": "a.py", "bytes": 1}`, "ISO 8601"},
		{"t is not a timestamp", `{"t": true, "file": "a.py", "bytes": 1}`, "epoch seconds"},
		{"empty timestamp", `{"t": "  ", "file": "a.py", "bytes": 1}`, `empty "t"`},
		{"missing file", `{"t": "2026-01-05T10:00:00Z", "bytes": 1}`, `missing "file"`},
		{"empty file", `{"t": "2026-01-05T10:00:00Z", "file": "  ", "bytes": 1}`, `empty "file"`},
		{"missing bytes", `{"t": "2026-01-05T10:00:00Z", "file": "a.py"}`, `missing "bytes"`},
		{"null bytes", `{"t": "2026-01-05T10:00:00Z", "file": "a.py", "bytes": null}`, `missing "bytes"`},
		{"negative bytes", `{"t": "2026-01-05T10:00:00Z", "file": "a.py", "bytes": -1}`, `"bytes" is negative`},
		{"non numeric bytes", `{"t": "2026-01-05T10:00:00Z", "file": "a.py", "bytes": "2048"}`, `"bytes" is not a number`},
		{"non-numeric bytes type", `{"t": "2026-01-05T10:00:00Z", "file": "a.py", "bytes": true}`, `"bytes" is not a number`},
		{"negative loc", `{"t": "2026-01-05T10:00:00Z", "file": "a.py", "bytes": 1, "loc": -3}`, `"loc" is negative`},
		{"trailing content", `{"t": "2026-01-05T10:00:00Z", "file": "a.py", "bytes": 1} {}`, "trailing content"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := parse(t, tc.line+"\n")
			if len(res.Records) != 0 {
				t.Fatalf("a rejected line must not produce a record: %+v", res.Records)
			}
			if len(res.Errors) != 1 {
				t.Fatalf("got %d errors, want 1", len(res.Errors))
			}
			if !strings.Contains(res.Errors[0].Message, tc.want) {
				t.Fatalf("error %q does not mention %q", res.Errors[0].Message, tc.want)
			}
			if res.Errors[0].Line != 1 {
				t.Fatalf("error line = %d, want 1", res.Errors[0].Line)
			}
		})
	}
}

func recordErrorStream(t *testing.T) {
	res := parse(t, strings.Join([]string{
		`{"t": "2026-01-05T10:00:00Z", "file": "a.py", "bytes": 10}`,
		`this is not json`,
		`{"t": "2026-01-06T10:00:00Z", "file": "a.py", "bytes": 20}`,
		`{"t": "2026-01-07T10:00:00Z", "file": "a.py", "bytes": -3}`,
		`{"t": "2026-01-08T10:00:00Z", "file": "a.py", "bytes": 30}`,
	}, "\n"))

	if len(res.Records) != 3 {
		t.Fatalf("got %d accepted records, want 3", len(res.Records))
	}
	if len(res.Errors) != 2 {
		t.Fatalf("got %d errors, want 2", len(res.Errors))
	}
	if res.Total != 5 {
		t.Fatalf("Total = %d, want 5", res.Total)
	}
	if res.Errors[0].Line != 2 || res.Errors[1].Line != 4 {
		t.Fatalf("error lines = %d,%d want 2,4", res.Errors[0].Line, res.Errors[1].Line)
	}
	// Surviving records keep their input order, which is what ties rely on.
	if res.Records[0].Bytes != 10 || res.Records[1].Bytes != 20 || res.Records[2].Bytes != 30 {
		t.Fatalf("accepted records out of order: %+v", res.Records)
	}
}

func TestSortStableWithTies(t *testing.T) {
	res := parse(t, strings.Join([]string{
		`{"t": "2026-01-05T10:00:00Z", "file": "a.py", "bytes": 30}`,
		`{"t": "2026-01-03T10:00:00Z", "file": "a.py", "bytes": 10}`,
		`{"t": "2026-01-05T10:00:00Z", "file": "a.py", "bytes": 31}`,
		`{"t": "2026-01-04T10:00:00Z", "file": "a.py", "bytes": 20}`,
	}, "\n"))
	res.Sort()

	got := make([]float64, len(res.Records))
	for i, r := range res.Records {
		got[i] = r.Bytes
	}
	// Unsorted input becomes ascending; the two records sharing 2026-01-05
	// keep their input order (30 before 31).
	if want := []float64{10, 20, 30, 31}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted values = %v, want %v", got, want)
	}
}

func TestFileScopeGrouping(t *testing.T) {
	res := parse(t, strings.Join([]string{
		`{"t": "2026-01-03T10:00:00Z", "file": "src/app.py", "bytes": 30, "loc": 3}`,
		`{"t": "2026-01-01T10:00:00Z", "file": "src/app.py", "bytes": 10, "loc": 1}`,
		`{"t": "2026-01-01T10:00:00Z", "file": "README.md", "bytes": 5}`,
		`{"t": "2026-01-02T10:00:00Z", "file": "src/app.py", "bytes": 20, "loc": 2}`,
	}, "\n"))
	res.Sort()

	series := BuildSeries(res.Records, ScopeFile, MetricBytes)
	if len(series) != 2 {
		t.Fatalf("got %d series, want 2", len(series))
	}
	// Series come back in sorted path order, not input order.
	if series[0].Key != "README.md" || series[1].Key != "src/app.py" {
		t.Fatalf("series keys = %q,%q", series[0].Key, series[1].Key)
	}
	if series[0].File != "README.md" {
		t.Fatalf("README series File = %q", series[0].File)
	}
	if want := []float64{10, 20, 30}; !reflect.DeepEqual(series[1].Values, want) {
		t.Fatalf("src/app.py values = %v, want %v (time order)", series[1].Values, want)
	}
	if !series[1].Times[0].Before(series[1].Times[1]) {
		t.Fatal("series times must be ascending")
	}
}

func TestRepoScopeSumsPerTimestamp(t *testing.T) {
	t.Run("byte series are summed per timestamp", func(t *testing.T) { repoByteSummation(t) })
	t.Run("the loc metric skips records without a line count", func(t *testing.T) { repoLocSummation(t) })
}

func repoByteSummation(t *testing.T) {
	res := parse(t, strings.Join([]string{
		`{"t": "2026-01-01T10:00:00Z", "file": "src/app.py", "bytes": 100}`,
		`{"t": "2026-01-01T10:00:00Z", "file": "README.md", "bytes": 50}`,
		`{"t": "2026-01-02T10:00:00Z", "file": "src/app.py", "bytes": 200}`,
		`{"t": "2026-01-02T10:00:00Z", "file": "docs/guide.md", "bytes": 25}`,
		`{"t": "2026-01-02T10:00:00Z", "file": "src/util.py", "bytes": 5}`,
	}, "\n"))
	res.Sort()

	series := BuildSeries(res.Records, ScopeRepo, MetricBytes)
	if len(series) != 1 {
		t.Fatalf("repo scope must produce exactly one series, got %d", len(series))
	}
	repo := series[0]
	if repo.Key != ScopeRepo || repo.File != "" {
		t.Fatalf("repo series key/file = %q/%q", repo.Key, repo.File)
	}
	// Two timestamps, each the sum of the files measured at that timestamp.
	if want := []float64{150, 230}; !reflect.DeepEqual(repo.Values, want) {
		t.Fatalf("repo values = %v, want %v", repo.Values, want)
	}
	if repo.Len() != 2 {
		t.Fatalf("repo observations = %d, want 2", repo.Len())
	}

	if empty := BuildSeries(nil, ScopeRepo, MetricBytes); len(empty) != 0 {
		t.Fatalf("an empty input must produce no repo series, got %d", len(empty))
	}
}

func repoLocSummation(t *testing.T) {
	res := parse(t, strings.Join([]string{
		`{"t": "2026-01-01T10:00:00Z", "file": "a.py", "bytes": 100, "loc": 7}`,
		`{"t": "2026-01-01T10:00:00Z", "file": "b.py", "bytes": 900, "loc": 3}`,
		`{"t": "2026-01-01T10:00:00Z", "file": "c.py", "bytes": 900}`,
	}, "\n"))
	res.Sort()

	usable, skipped := FilterSeries(res.Records, MetricLoc)
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1 (the record without loc)", skipped)
	}
	series := BuildSeries(usable, ScopeRepo, MetricLoc)
	if want := []float64{10}; !reflect.DeepEqual(series[0].Values, want) {
		t.Fatalf("repo loc values = %v, want %v", series[0].Values, want)
	}

	// The same records are all usable for the byte series.
	all, none := FilterSeries(res.Records, MetricBytes)
	if none != 0 || len(all) != 3 {
		t.Fatalf("bytes filter kept %d skipped %d, want 3 and 0", len(all), none)
	}
}

func TestCenterBlendWithKnownAlpha(t *testing.T) {
	t.Run("blend is alpha*median + (1-alpha)*geomean", func(t *testing.T) { centerBlend(t) })
	t.Run("no positive value falls back to the median alone", func(t *testing.T) { centerMedianFallback(t) })
}

func centerBlend(t *testing.T) {
	// Window {10, 20, 30, 40, 50}: median 30, geometric mean 120^(1/5)*10.
	values := []float64{10, 20, 30, 40, 50}
	gm := 10 * math.Pow(120, 1.0/5.0)
	closeTo(t, gm, 26.051710846970517)

	for _, alpha := range []float64{0, 0.25, 0.5, 1} {
		est := Center(values, alpha)
		if est.Geomean == nil {
			t.Fatal("expected a geometric mean")
		}
		closeTo(t, *est.Geomean, gm)
		closeTo(t, est.Median, 30)
		closeTo(t, est.Center, alpha*30+(1-alpha)*gm)
		if est.GeomeanExcluded != 0 {
			t.Fatalf("GeomeanExcluded = %d, want 0", est.GeomeanExcluded)
		}
	}

	// alpha 1 is the pure median, alpha 0 the pure geometric mean.
	if got := Center(values, 1).Center; got != 30 {
		t.Fatalf("alpha=1 center = %v, want 30", got)
	}
	closeTo(t, Center(values, 0).Center, gm)
}

func centerMedianFallback(t *testing.T) {
	// A window of zeros has no positive value, so no geometric mean exists.
	est := Center([]float64{0, 0, 0, 0}, 0.5)
	if est.Geomean != nil {
		t.Fatalf("expected a nil geometric mean, got %v", *est.Geomean)
	}
	if est.GeomeanExcluded != 4 {
		t.Fatalf("GeomeanExcluded = %d, want 4", est.GeomeanExcluded)
	}
	if est.Center != 0 {
		t.Fatalf("Center = %v, want the median fallback 0", est.Center)
	}

	// A mixed window drops the zeros but keeps the reported count: {0,4,16,16}
	// has median 10 and geometric mean cbrt(4*16*16) over the three positive
	// values.
	est = Center([]float64{0, 4, 16, 16}, 0.5)
	if est.Geomean == nil {
		t.Fatal("expected a geometric mean from the positive values")
	}
	closeTo(t, *est.Geomean, math.Cbrt(1024))
	if est.GeomeanExcluded != 1 {
		t.Fatalf("GeomeanExcluded = %d, want 1", est.GeomeanExcluded)
	}
	if est.Median != 10 {
		t.Fatalf("Median = %v, want 10", est.Median)
	}
	closeTo(t, est.Center, 0.5*10+0.5*math.Cbrt(1024))
}

func TestBandMathAndDegenerateCase(t *testing.T) {
	// Window {10, 12, 14, 16, 18}: median 14, deviations 4,2,0,2,4 -> MAD 2.
	est := Center([]float64{10, 12, 14, 16, 18}, 0.5)
	spread := 2 * stats.MADScale
	for _, z := range []float64{1, 1.5, 2} {
		low, high := Band(est.Center, spread, z)
		closeTo(t, low, est.Center-z*spread)
		closeTo(t, high, est.Center+z*spread)
	}
	// A wider multiplier must widen the band, never flip it.
	lo1, hi1 := Band(est.Center, spread, 1)
	lo2, hi2 := Band(est.Center, spread, 3)
	if !(lo2 < lo1 && hi2 > hi1) {
		t.Fatalf("z=3 band [%v,%v] is not wider than z=1 band [%v,%v]", lo2, hi2, lo1, hi1)
	}

	// A constant window has no dispersion: the band collapses to the centre.
	low, high := Band(123, 0, 2)
	if low != 123 || high != 123 {
		t.Fatalf("degenerate band = [%v,%v], want [123,123]", low, high)
	}
	if low, high = Band(123, 0, 0); low != 123 || high != 123 {
		t.Fatalf("degenerate band with z=0 = [%v,%v], want [123,123]", low, high)
	}
}

func TestTrailingWindowAndAverages(t *testing.T) {
	set := DefaultSettings()
	set.Window = 3

	res := FitSeries(Series{Key: "a.py", File: "a.py", Values: []float64{100, 200, 300, 400}}, set)
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want ok", res.Status)
	}
	w := res.Window
	if w == nil {
		t.Fatal("expected a fitted window")
	}
	if w.N != 3 {
		t.Fatalf("N = %d, want 3: the window is the trailing observations, not the first ones", w.N)
	}
	closeTo(t, w.Median, 300)
	closeTo(t, *w.Geomean, 100*math.Cbrt(24))
	closeTo(t, w.Center, 0.5*300+0.5*100*math.Cbrt(24))
	closeTo(t, w.Spread, 100*stats.MADScale)
	closeTo(t, w.Low, w.Center-w.Spread)
	closeTo(t, w.High, w.Center+w.Spread)
	if w.Degenerate {
		t.Fatal("window {200,300,400} is not degenerate")
	}
	if w.Observed != 4 {
		t.Fatalf("Observed = %d, want 4", w.Observed)
	}

	// SMA of the trailing window {200,300,400} is 300.
	closeTo(t, w.SMA, 300)
	// EMA with span 5 (alpha 1/3), seeded with 200:
	//   e1 = 200 + (300-200)/3 = 233.33..., e2 = e1 + (400-e1)/3 = 288.88...
	closeTo(t, w.EMA, 2600.0/9.0)

	// A window larger than the series simply uses everything.
	set.Window = 30
	res = FitSeries(Series{Key: "a.py", File: "a.py", Values: []float64{10, 20}}, set)
	if res.Window.N != 2 {
		t.Fatalf("N = %d, want 2", res.Window.N)
	}
}

func TestAnomalyJudgedOnPriorWindow(t *testing.T) {
	set := DefaultSettings()

	// A constant run followed by one huge commit: exactly one flag, judged
	// against the constant run that preceded it.
	res := FitSeries(Series{Key: "a.py", File: "a.py", Values: []float64{100, 100, 100, 100, 100, 5000}}, set)
	if len(res.Anomalies) != 1 {
		t.Fatalf("got %d anomalies, want 1: %+v", len(res.Anomalies), res.Anomalies)
	}
	a := res.Anomalies[0]
	if a.Step != 6 || a.V != 5000 {
		t.Fatalf("anomaly = %+v, want step 6 with v=5000", a)
	}
	// The band is the one the prior five observations implied, not one widened
	// by the outlier itself: a constant window has no dispersion.
	if a.Center != 100 || a.Low != 100 || a.High != 100 {
		t.Fatalf("band = [%v,%v] around %v, want [100,100] around 100", a.Low, a.High, a.Center)
	}
	if a.Dropped {
		t.Fatal("an observation is kept by default")
	}

	// Growth series with a late outlier: the band is the one the five earlier
	// observations implied, never one stretched to cover the outlier.
	set.Window = 5
	grown := []float64{10, 12, 14, 16, 18, 200}
	res = FitSeries(Series{Key: "a.py", File: "a.py", Values: grown}, set)
	last := res.Anomalies[len(res.Anomalies)-1]
	if last.Step != 6 || last.V != 200 {
		t.Fatalf("last anomaly = %+v, want step 6 with v=200", last)
	}
	// Prior window {10,12,14,16,18}: median 14, geometric mean
	// 10*cbrt_root_5(1.2*1.4*1.6*1.8), MAD 2.
	priorCenter := 0.5*14 + 0.5*10*math.Pow(1.2*1.4*1.6*1.8, 1.0/5.0)
	closeTo(t, last.Center, priorCenter)
	closeTo(t, last.Low, priorCenter-2*stats.MADScale)
	closeTo(t, last.High, priorCenter+2*stats.MADScale)
	if last.V <= last.High {
		t.Fatalf("v=%v should lie above the band high %v", last.V, last.High)
	}

	// A steady ramp on a tight window is a genuine blind spot of a robust band:
	// every step grows by more than one MAD-scaled spread, so each one trips
	// the alert. That is the documented behaviour, and the cure is a wider
	// window or a larger z, not a smarter judgement call.
	ramp := []float64{10, 12, 14, 16, 18}
	if got := FitSeries(Series{Key: "a.py", File: "a.py", Values: ramp}, set); len(got.Anomalies) != 3 {
		t.Fatalf("got %d anomalies on a tight ramp, want 3: %+v", len(got.Anomalies), got.Anomalies)
	}
	// The same ramp under a wider band is quiet, and the 200 still stands out.
	set.Z = 4
	wide := append(append([]float64{}, ramp...), 200)
	res = FitSeries(Series{Key: "a.py", File: "a.py", Values: wide}, set)
	if len(res.Anomalies) != 1 || res.Anomalies[0].V != 200 {
		t.Fatalf("z=4 on a ramp: got %+v, want only the 200 flagged", res.Anomalies)
	}
	set.Z = DefaultSettings().Z

	// The first two observations are never judged: a band needs two points.
	quiet := FitSeries(Series{Key: "a.py", File: "a.py", Values: []float64{10, 90}}, set)
	if len(quiet.Anomalies) != 0 {
		t.Fatalf("got %d anomalies, want 0 for a two point series", len(quiet.Anomalies))
	}

	// A value inside the band is never flagged. Judging 100 against the prior
	// pair {100,200}: centre 145.71, MAD 50, spread 74.13, band [71.6, 219.8].
	inside := FitSeries(Series{Key: "a.py", File: "a.py", Values: []float64{100, 200, 100}}, set)
	if len(inside.Anomalies) != 0 {
		t.Fatalf("got %d anomalies, want 0 for a value inside the band", len(inside.Anomalies))
	}
}

func TestDropAnomaliesRecomputes(t *testing.T) {
	values := []float64{100, 100, 100, 100, 100, 5000, 100}

	keep := DefaultSettings()
	kept := FitSeries(Series{Key: "a.py", File: "a.py", Values: values}, keep)
	// Two flags, and both are explainable: the 5000 itself, and the 100 that
	// follows it, which sits below a band the 5000 had already skewed upwards.
	if len(kept.Anomalies) != 2 {
		t.Fatalf("got %d anomalies, want 2: %+v", len(kept.Anomalies), kept.Anomalies)
	}
	if kept.Anomalies[0].Step != 6 || kept.Anomalies[0].V != 5000 {
		t.Fatalf("first anomaly = %+v, want step 6 with v=5000", kept.Anomalies[0])
	}
	if kept.Anomalies[1].Step != 7 || kept.Anomalies[1].V != 100 || kept.Anomalies[1].V >= kept.Anomalies[1].Low {
		t.Fatalf("second anomaly = %+v, want step 7 with v=100 below the band", kept.Anomalies[1])
	}
	// With the outlier still in the window: median 100, geometric mean
	// 50^(1/7)*100, so the centre is pulled up above the flat level.
	if kept.Window.GeomeanExcluded != 0 {
		t.Fatalf("GeomeanExcluded = %d, want 0", kept.Window.GeomeanExcluded)
	}
	if kept.Window.Center <= 100 {
		t.Fatalf("center = %v, want it pulled above 100 by the outlier", kept.Window.Center)
	}
	for _, a := range kept.Anomalies {
		if a.Dropped {
			t.Fatal("Dropped must be false without --drop-anomalies")
		}
	}

	drop := DefaultSettings()
	drop.DropAnomalies = true
	dropped := FitSeries(Series{Key: "a.py", File: "a.py", Values: values}, drop)
	if len(dropped.Anomalies) != 2 {
		t.Fatalf("the anomaly list itself must not change: %+v", dropped.Anomalies)
	}
	for _, a := range dropped.Anomalies {
		if !a.Dropped {
			t.Fatal("Dropped must be true with --drop-anomalies")
		}
	}
	if dropped.Window.N != 5 || dropped.Window.Observed != 5 {
		t.Fatalf("N/Observed = %d/%d, want 5/5 (both flags are out)", dropped.Window.N, dropped.Window.Observed)
	}
	// What is left is flat, so the median-only level 100 is recovered and the
	// band degenerates to a point.
	if dropped.Window.Center != 100 || dropped.Window.Spread != 0 || !dropped.Window.Degenerate {
		t.Fatalf("dropped fit = %+v, want a flat centre 100 with a zero spread", dropped.Window)
	}
	if dropped.Window.Low != 100 || dropped.Window.High != 100 {
		t.Fatalf("degenerate band = [%v,%v], want [100,100]", dropped.Window.Low, dropped.Window.High)
	}

	// Dropping is judged on the original series, so a flag list computed with
	// a two observation window is the same one; only the fit changes.
	drop.Window = 2
	short := FitSeries(Series{Key: "a.py", File: "a.py", Values: []float64{100, 100, 1000, 100}}, drop)
	if len(short.Anomalies) != 1 || short.Anomalies[0].Step != 3 {
		t.Fatalf("anomalies = %+v, want only step 3", short.Anomalies)
	}
	if short.Window.N != 2 {
		t.Fatalf("N = %d, want 2: the 1000 is dropped and a window of 2 keeps two", short.Window.N)
	}
	if short.Window.Observed != 3 {
		t.Fatalf("Observed = %d, want 3 (4 observations, 1 dropped)", short.Window.Observed)
	}
}

func TestInsufficientScope(t *testing.T) {
	set := DefaultSettings()

	one := FitSeries(Series{Key: "a.py", File: "a.py", Values: []float64{512}}, set)
	if one.Status != StatusInsufficient {
		t.Fatalf("status = %q, want insufficient", one.Status)
	}
	if len(one.Predictions) != 0 {
		t.Fatalf("got %d predictions, want 0", len(one.Predictions))
	}
	if one.Window == nil || one.Window.N != 1 {
		t.Fatalf("window = %+v, want a one observation window", one.Window)
	}
	if one.Key != "a.py" {
		t.Fatalf("key = %q", one.Key)
	}

	none := FitSeries(Series{Key: "empty.py"}, set)
	if none.Status != StatusInsufficient {
		t.Fatalf("status = %q, want insufficient", none.Status)
	}
	if none.Window != nil {
		t.Fatalf("window = %+v, want nil for an empty scope", none.Window)
	}
	if len(none.Predictions) != 0 {
		t.Fatal("an empty scope must not predict")
	}

	// Two observations are enough.
	two := FitSeries(Series{Key: "a.py", File: "a.py", Values: []float64{10, 12}}, set)
	if two.Status != StatusOK || len(two.Predictions) != 1 {
		t.Fatalf("two observations should predict, got %+v", two)
	}
}

func TestHorizonIsFlatAndFitIsDeterministic(t *testing.T) {
	set := DefaultSettings()
	set.Horizon = 3
	series := []Series{
		{Key: "b.py", File: "b.py", Values: []float64{200, 190, 205, 195}},
		{Key: "a.py", File: "a.py", Values: []float64{10, 12, 11, 13}},
	}
	first := FitAll(series, set)
	second := FitAll(series, set)

	if !reflect.DeepEqual(first, second) {
		t.Fatal("two identical runs produced different results")
	}
	// Series order follows the input, it is not re-sorted by the model.
	if first[0].Key != "b.py" || first[1].Key != "a.py" {
		t.Fatalf("FitAll reordered the series: %q,%q", first[0].Key, first[1].Key)
	}

	for _, res := range first {
		if len(res.Predictions) != 3 {
			t.Fatalf("%s: got %d predictions, want 3", res.Key, len(res.Predictions))
		}
		p0 := res.Predictions[0]
		for i, p := range res.Predictions {
			if p.Step != i+1 {
				t.Fatalf("%s: step %d at index %d", res.Key, p.Step, i)
			}
			// This is a robust level estimate, not a trend: the horizon is flat.
			if p.Center != p0.Center || p.Low != p0.Low || p.High != p0.High {
				t.Fatalf("%s: step %d drifted from step 1: %+v vs %+v", res.Key, p.Step, p, p0)
			}
			if p.Center != res.Window.Center || p.Low != res.Window.Low || p.High != res.Window.High {
				t.Fatalf("%s: prediction does not match the fitted window", res.Key)
			}
		}
	}
}

func TestDefaultsAndTimeFormatting(t *testing.T) {
	set := DefaultSettings()
	if set.Window != 30 || set.Alpha != 0.5 || set.Z != 1 || set.EMASpan != 5 || set.Horizon != 1 {
		t.Fatalf("documented defaults changed: %+v", set)
	}
	if set.Scope != ScopeFile || set.Metric != MetricBytes || set.DropAnomalies {
		t.Fatalf("documented defaults changed: %+v", set)
	}

	// Every printed timestamp is ISO 8601 in UTC, whatever zone it arrived in.
	ts := time.Date(2026, 1, 5, 12, 0, 0, 123, time.FixedZone("plus2", 2*3600))
	if got := FormatTime(ts); got != "2026-01-05T10:00:00Z" {
		t.Fatalf("FormatTime = %q, want 2026-01-05T10:00:00Z", got)
	}
}
