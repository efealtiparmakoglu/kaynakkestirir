package stats

// SMA returns the simple moving average of values over the whole slice, using
// the numerically stable incremental mean. The SMA of an empty slice is 0.
func SMA(values []float64) float64 {
	return Mean(values)
}

// EMAAlpha converts an EMA span into its smoothing factor, alpha = 2/(span+1).
// A span below 1 is meaningless and yields 0, which freezes the EMA on its
// seed value.
func EMAAlpha(span int) float64 {
	if span < 1 {
		return 0
	}
	return 2 / (float64(span) + 1)
}

// EMA returns the exponential moving average of values for the given smoothing
// factor alpha, seeded with the first element of the slice and then updating
// e += alpha * (v - e) for every remaining element. Seeding with an actual
// observation (rather than with zero or with the window mean) keeps the series
// anchored to the data it summarises.
//
// An alpha of 0 or less leaves the seed untouched; an empty slice yields 0.
func EMA(values []float64, alpha float64) float64 {
	if len(values) == 0 {
		return 0
	}
	e := values[0]
	if alpha <= 0 {
		return e
	}
	for _, v := range values[1:] {
		e += alpha * (v - e)
	}
	return e
}

// EMASpan is the convenience form of EMA for a span, using alpha = 2/(span+1).
func EMASpan(values []float64, span int) float64 {
	return EMA(values, EMAAlpha(span))
}
