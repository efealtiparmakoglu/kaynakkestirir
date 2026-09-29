package model

import (
	"math"

	"kaynakkestirir/stats"
)

// DetectAnomalies walks a series in time order and flags every observation
// that falls outside the band implied by the observations that came BEFORE it.
//
// The judgement uses the prior window only, never the current observation, so
// an outlier can never widen the band that excuses it. Without this rule a
// single huge commit would be silently absorbed by the very window meant to
// expose it.
//
// An observation is only judged once at least two earlier observations exist: a
// band needs two points before it has a spread at all, and a band inferred
// from a single value would flag every ordinary change. The first two
// observations of a series are therefore never flagged.
//
// A prior window with no dispersion (spread 0) collapses to a point, so any
// change from a perfectly constant run is reported. That is deliberate: a
// series that used to be constant and suddenly is not is exactly the kind of
// structural break worth surfacing.
func DetectAnomalies(s Series, set Settings) []Anomaly {
	out := []Anomaly{}
	values := s.Values
	for i := 2; i < len(values); i++ {
		prior := values[maxInt(0, i-set.Window):i]
		if len(prior) < 2 {
			continue
		}
		est := Center(prior, set.Alpha)
		spread := stats.Spread(prior)
		low, high := Band(est.Center, spread, set.Z)
		v := values[i]
		if !outsideBand(v, low, high) {
			continue
		}
		a := Anomaly{
			Step:   i + 1,
			Scope:  s.Key,
			File:   s.File,
			V:      v,
			Center: est.Center,
			Low:    low,
			High:   high,
		}
		if i < len(s.Times) {
			a.T = s.Times[i]
		}
		out = append(out, a)
	}
	return out
}

// bandEpsilon is the relative slack allowed before an observation counts as
// outside the band. The centre is a product of medians, geometric means and
// round trips through log/exp, so the last bit of a float is not a signal. The
// slack is far below any real change in a code measurement and keeps rounding
// noise from manufacturing anomalies on perfectly regular series.
const bandEpsilon = 1e-9

func outsideBand(v, low, high float64) bool {
	slack := bandEpsilon * math.Max(math.Abs(low), math.Abs(high))
	return v < low-slack || v > high+slack
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
