package model

import (
	"encoding/json"
	"time"

	"kaynakkestirir/stats"
)

// Status values reported for a scope.
const (
	// StatusOK means the scope had enough observations to predict.
	StatusOK = "ok"
	// StatusInsufficient means the scope held fewer than two usable
	// observations. This is a reported condition, never an error.
	StatusInsufficient = "insufficient"
)

// Settings holds every knob of the estimator. It is fully explicit so a report
// is reproducible from the values it prints.
type Settings struct {
	// Window is the number of trailing observations the model looks at.
	Window int
	// Alpha blends median and geomean: center = alpha*median + (1-alpha)*geomean.
	Alpha float64
	// Z is the multiplier applied to the robust spread to form the band.
	Z float64
	// EMASpan is the span of the exponential moving average, alpha = 2/(span+1).
	EMASpan int
	// Horizon is how many steps the flat prediction is repeated for.
	Horizon int
	// Scope is ScopeFile or ScopeRepo.
	Scope string
	// DropAnomalies recomputes the fit with flagged observations removed.
	DropAnomalies bool
	// Metric is MetricBytes or MetricLoc.
	Metric string
}

// DefaultSettings returns the documented defaults.
func DefaultSettings() Settings {
	return Settings{
		Window:        30,
		Alpha:         0.5,
		Z:             1.0,
		EMASpan:       5,
		Horizon:       1,
		Scope:         ScopeFile,
		DropAnomalies: false,
		Metric:        MetricBytes,
	}
}

// Window holds the fitted statistics of the trailing window.
type Window struct {
	// N is the number of observations actually used.
	N int `json:"n"`
	// Median is the median of the window values.
	Median float64 `json:"median"`
	// Geomean is the geometric mean of the positive window values, or null when
	// the window contained no positive value (median-only fallback).
	Geomean *float64 `json:"geomean"`
	// GeomeanExcluded counts the window values that were not strictly positive
	// and therefore could not enter the geometric mean.
	GeomeanExcluded int `json:"geomean_excluded"`
	// Center is the point prediction for the next observation.
	Center float64 `json:"center"`
	// Spread is 1.4826 * MAD over the window.
	Spread float64 `json:"spread"`
	// Degenerate is true when Spread is zero, i.e. the band collapses to a
	// single point at Center.
	Degenerate bool `json:"degenerate"`
	// Low and High are the bounds of the uncertainty band.
	Low  float64 `json:"low"`
	High float64 `json:"high"`
	// SMA is the simple moving average of the window.
	SMA float64 `json:"sma"`
	// EMA is the exponential moving average of the window.
	EMA float64 `json:"ema"`
	// Observed is the number of usable observations in the scope overall, after
	// anomaly dropping.
	Observed int `json:"observed"`
}

// Prediction is one step of the forecast horizon.
type Prediction struct {
	Step   int     `json:"step"`
	Center float64 `json:"center"`
	Low    float64 `json:"low"`
	High   float64 `json:"high"`
}

// Anomaly is an observation that fell outside the band implied by the prior
// window, judged at the time it was seen.
type Anomaly struct {
	// Step is the one-based position of the observation in its series.
	Step int `json:"step"`
	// T is the observation time.
	T time.Time `json:"-"`
	// Scope is the scope key the anomaly was found in.
	Scope string `json:"scope"`
	// File is empty for repository scope.
	File string `json:"file,omitempty"`
	// V is the observed value; Center, Low and High are the band it missed.
	V      float64 `json:"v"`
	Center float64 `json:"center"`
	Low    float64 `json:"low"`
	High   float64 `json:"high"`
	// Dropped is true when --drop-anomalies removed this observation from the
	// fitted window.
	Dropped bool `json:"dropped"`
}

// MarshalJSON renders the observation time as an ISO 8601 UTC timestamp.
func (a Anomaly) MarshalJSON() ([]byte, error) {
	type alias struct {
		Step    int     `json:"step"`
		T       string  `json:"t"`
		Scope   string  `json:"scope"`
		File    string  `json:"file,omitempty"`
		V       float64 `json:"v"`
		Center  float64 `json:"center"`
		Low     float64 `json:"low"`
		High    float64 `json:"high"`
		Dropped bool    `json:"dropped"`
	}
	return json.Marshal(alias{
		Step:    a.Step,
		T:       FormatTime(a.T),
		Scope:   a.Scope,
		File:    a.File,
		V:       a.V,
		Center:  a.Center,
		Low:     a.Low,
		High:    a.High,
		Dropped: a.Dropped,
	})
}

// ScopeResult is the full report for one scope.
type ScopeResult struct {
	Scope       string       `json:"scope"`
	Key         string       `json:"key"`
	File        string       `json:"file,omitempty"`
	Status      string       `json:"status"`
	Window      *Window      `json:"window"`
	Predictions []Prediction `json:"predictions"`
	Anomalies   []Anomaly    `json:"anomalies"`
}

// Estimate is the point estimate produced for a set of values.
type Estimate struct {
	Median          float64
	Geomean         *float64
	GeomeanExcluded int
	Center          float64
}

// Center blends the median and the geometric mean of values:
//
//	center = alpha*median + (1-alpha)*geomean
//
// The geometric mean uses only strictly positive values; every other value is
// excluded and counted. When no positive value exists the geometric mean is
// undefined and the model falls back to the median alone, which is reported as
// a nil geometric mean rather than as a fabricated number.
func Center(values []float64, alpha float64) Estimate {
	med := stats.Median(values)
	g := stats.Geomean(values)
	est := Estimate{Median: med, GeomeanExcluded: g.Excluded}
	if !g.OK {
		est.Center = med
		return est
	}
	gm := g.Value
	est.Geomean = &gm
	est.Center = alpha*med + (1-alpha)*gm
	return est
}

// Band returns the uncertainty band around center for a robust spread and a
// z multiplier. A spread of exactly 0 is degenerate: the window carries no
// measurable dispersion, so the band collapses to [center, center] rather than
// pretending to a precision the data cannot support.
func Band(center, spread, z float64) (low, high float64) {
	if spread == 0 {
		return center, center
	}
	return center - z*spread, center + z*spread
}

// trailing returns the last n values of a series.
func trailing(values []float64, n int) []float64 {
	if n >= len(values) {
		return values
	}
	return values[len(values)-n:]
}

// FitWindow computes the full window model from the values of one scope.
func FitWindow(values []float64, n, emaSpan int, alpha, z float64) *Window {
	w := trailing(values, n)
	est := Center(w, alpha)
	spread := stats.Spread(w)
	low, high := Band(est.Center, spread, z)
	return &Window{
		N:               len(w),
		Median:          est.Median,
		Geomean:         est.Geomean,
		GeomeanExcluded: est.GeomeanExcluded,
		Center:          est.Center,
		Spread:          spread,
		Degenerate:      spread == 0,
		Low:             low,
		High:            high,
		SMA:             stats.SMA(w),
		EMA:             stats.EMASpan(w, emaSpan),
	}
}

// FitSeries fits the model for a single series: it detects anomalies on the
// original series, optionally recomputes without them, and then predicts the
// next horizon steps as a flat repetition of the point estimate.
func FitSeries(s Series, set Settings) ScopeResult {
	res := ScopeResult{
		Scope:       set.Scope,
		Key:         s.Key,
		File:        s.File,
		Predictions: []Prediction{},
		Anomalies:   []Anomaly{},
	}

	anomalies := DetectAnomalies(s, set)
	dropped := make([]bool, len(s.Values))
	for _, a := range anomalies {
		dropped[a.Step-1] = true
		a.Dropped = set.DropAnomalies
		res.Anomalies = append(res.Anomalies, a)
	}

	values := s.Values
	if set.DropAnomalies {
		kept := make([]float64, 0, len(values))
		for i, v := range values {
			if !dropped[i] {
				kept = append(kept, v)
			}
		}
		values = kept
	}

	if len(values) < 2 {
		// Fewer than two usable observations: reported, never an error. The
		// single-value statistics are still shown because they cost nothing and
		// explain why the scope is too thin to predict.
		if len(values) == 1 {
			res.Window = FitWindow(values, set.Window, set.EMASpan, set.Alpha, set.Z)
		}
		res.Status = StatusInsufficient
		return res
	}

	res.Status = StatusOK
	w := FitWindow(values, set.Window, set.EMASpan, set.Alpha, set.Z)
	w.Observed = len(values)
	res.Window = w
	for step := 1; step <= set.Horizon; step++ {
		res.Predictions = append(res.Predictions, Prediction{
			Step:   step,
			Center: w.Center,
			Low:    w.Low,
			High:   w.High,
		})
	}
	return res
}

// FitAll fits every series, preserving the order of the input series.
func FitAll(series []Series, set Settings) []ScopeResult {
	out := make([]ScopeResult, 0, len(series))
	for _, s := range series {
		out = append(out, FitSeries(s, set))
	}
	return out
}
