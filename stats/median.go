// Package stats implements the handful of robust summary statistics the
// estimator needs. Everything here is hand-rolled on top of the standard
// library: no external packages, by design, so the numeric behaviour of the
// tool is fully auditable and stable across Go releases.
package stats

import "sort"

// Sorted returns a sorted copy of values in ascending order. The input slice is
// never modified.
func Sorted(values []float64) []float64 {
	out := make([]float64, len(values))
	copy(out, values)
	sort.Float64s(out)
	return out
}

// Mean returns the arithmetic mean of values. It uses the incremental
// formulation m += (x-m)/n so that long windows of large magnitudes (byte
// counts) do not lose precision to intermediate rounding. The mean of an empty
// slice is 0.
func Mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := 0.0
	for i, v := range values {
		m += (v - m) / float64(i+1)
	}
	return m
}

// Median returns the median of values: the middle element for an odd count, the
// mean of the two middle elements for an even count. The input is not modified.
// The median of an empty slice is 0.
func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := Sorted(values)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
