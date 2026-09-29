package stats

import "math"

// GeomeanResult reports the outcome of a geometric mean computation.
type GeomeanResult struct {
	// Value is the geometric mean of the positive values.
	Value float64
	// Included is the number of values that were strictly greater than zero and
	// therefore took part in the product.
	Included int
	// Excluded is the number of values that were not strictly positive (zero or
	// negative) and had to be dropped from the product.
	Excluded int
	// OK is false when no strictly positive value existed; the caller must then
	// fall back to a median-only model.
	OK bool
}

// Geomean returns the geometric mean of the strictly positive values in
// values, together with the number of values it included and the number it had
// to exclude. The logarithm is accumulated in log space and exponentiated once,
// which keeps the result stable across a wide range of magnitudes and avoids
// intermediate overflow for very large products.
//
// Non-positive values cannot enter a product, so they are excluded and counted
// rather than silently clamped; the count is surfaced in the report because a
// window full of zeros changes the meaning of the blend.
func Geomean(values []float64) GeomeanResult {
	res := GeomeanResult{}
	sum := 0.0
	first := 0.0
	constant := true
	for _, v := range values {
		if v > 0 && !math.IsInf(v, 0) {
			if res.Included == 0 {
				first = v
			}
			if v != first {
				constant = false
			}
			sum += math.Log(v)
			res.Included++
		} else {
			res.Excluded++
		}
	}
	if res.Included == 0 {
		return res
	}
	res.OK = true
	if res.Included == 1 || constant {
		// A constant sample's geometric mean is that constant, returned
		// exactly. Round-tripping it through log/exp would land an ulp away
		// from it, which is enough to make a degenerate band miss the very
		// values it was built from.
		res.Value = first
		return res
	}
	res.Value = math.Exp(sum / float64(res.Included))
	return res
}
