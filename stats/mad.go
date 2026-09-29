package stats

import "math"

// MADScale converts a median absolute deviation into an estimator of the
// standard deviation of normally distributed data. 1.4826 is the usual
// constant (the reciprocal of the standard normal median).
const MADScale = 1.4826

// MAD returns the median absolute deviation of values: the median of the
// absolute distances from the median. It is a robust spread estimate that is
// not dragged around by a single huge outlier, which is exactly what a code
// growth series needs. The median of an empty slice is 0.
func MAD(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := Median(values)
	dev := make([]float64, len(values))
	for i, v := range values {
		dev[i] = math.Abs(v - m)
	}
	return Median(dev)
}

// Spread returns the scaled robust spread of values, i.e. 1.4826 * MAD. A
// spread of exactly 0 means the window is degenerate: every observation in it
// shares the same median and no absolute deviation at all, so no meaningful
// band can be derived. Callers must treat 0 as a collapse to a point rather
// than dividing by it.
func Spread(values []float64) float64 {
	return MADScale * MAD(values)
}
