package stats

import (
	"math"
	"testing"
)

const tol = 1e-9

func closeTo(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > tol*math.Max(1, math.Abs(want)) {
		t.Fatalf("got %v, want %v (tolerance %g)", got, want, tol)
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		name string
		in   []float64
		want float64
	}{
		{"odd count takes the middle", []float64{5, 1, 3}, 3},
		{"odd count unsorted", []float64{42, 7, 19, 3, 88}, 19},
		{"even count averages the middle pair", []float64{1, 2, 3, 4}, 2.5},
		{"even count unsorted", []float64{100, 4, 3, 2, 1, 50}, 3.5},
		{"single value", []float64{9}, 9},
		{"empty is zero", nil, 0},
		{"byte magnitudes", []float64{2048, 2100, 1980, 2050}, 2049},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Median(tc.in)
			if math.Abs(got-tc.want) > tol {
				t.Fatalf("Median(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestMedianDoesNotMutateInput(t *testing.T) {
	in := []float64{3, 1, 2}
	Median(in)
	if in[0] != 3 || in[1] != 1 || in[2] != 2 {
		t.Fatalf("Median reordered its input: %v", in)
	}

	sorted := Sorted(in)
	if len(sorted) != 3 || sorted[0] != 1 || sorted[1] != 2 || sorted[2] != 3 {
		t.Fatalf("Sorted = %v, want [1 2 3]", sorted)
	}
	sorted[0] = 99
	if in[1] != 1 {
		t.Fatalf("Sorted shared storage with its input: %v", in)
	}
}

func TestMADAndSpread(t *testing.T) {
	t.Run("constant series has no dispersion", func(t *testing.T) {
		if got := MAD([]float64{7, 7, 7, 7, 7}); got != 0 {
			t.Fatalf("MAD of a constant series = %v, want 0", got)
		}
		if got := Spread([]float64{7, 7, 7, 7, 7}); got != 0 {
			t.Fatalf("Spread of a constant series = %v, want 0", got)
		}
	})

	t.Run("known deviations", func(t *testing.T) {
		// median 14, deviations 4,2,0,2,4, median deviation 2.
		if got := MAD([]float64{10, 12, 14, 16, 18}); got != 2 {
			t.Fatalf("MAD = %v, want 2", got)
		}
		closeTo(t, Spread([]float64{10, 12, 14, 16, 18}), 2*1.4826)
	})

	t.Run("outlier barely moves the spread", func(t *testing.T) {
		// [10,12,14,16,18,10000] has median 15, deviations 5,3,1,1,3,9985.
		// The median deviation stays 3, while the standard deviation explodes.
		closeTo(t, MAD([]float64{10, 12, 14, 16, 18, 10000}), 3)
	})

	t.Run("empty is zero", func(t *testing.T) {
		if MAD(nil) != 0 || Spread(nil) != 0 {
			t.Fatal("MAD/Spread of an empty slice must be 0")
		}
	})
}

func TestGeomean(t *testing.T) {
	t.Run("has the units of the input", func(t *testing.T) {
		// 1, 10, 100, 1000 bytes -> product 10^6, fourth root 10^1.5 bytes.
		// A geometric mean stays in the units of its input, so the answer
		// cannot be an order of magnitude away from the data.
		got := Geomean([]float64{1, 10, 100, 1000})
		if !got.OK || got.Included != 4 || got.Excluded != 0 {
			t.Fatalf("unexpected counts: %+v", got)
		}
		closeTo(t, got.Value, 10*math.Sqrt(10))
	})

	t.Run("ignores ordering", func(t *testing.T) {
		closeTo(t, Geomean([]float64{1000, 1, 100, 10}).Value, 10*math.Sqrt(10))
	})

	t.Run("excludes and counts non-positive values", func(t *testing.T) {
		// 0 cannot enter a product; 4 and 16 give a geometric mean of 8.
		got := Geomean([]float64{0, 4, 16, -2})
		if !got.OK {
			t.Fatal("expected a geometric mean from the positive values")
		}
		if got.Included != 2 || got.Excluded != 2 {
			t.Fatalf("Included=%d Excluded=%d, want 2 and 2", got.Included, got.Excluded)
		}
		closeTo(t, got.Value, 8)
	})

	t.Run("all zeros yields no geometric mean", func(t *testing.T) {
		got := Geomean([]float64{0, 0})
		if got.OK {
			t.Fatalf("expected OK=false, got %+v", got)
		}
		if got.Included != 0 || got.Excluded != 2 {
			t.Fatalf("Included=%d Excluded=%d, want 0 and 2", got.Included, got.Excluded)
		}
	})

	t.Run("a single positive value is returned exactly", func(t *testing.T) {
		got := Geomean([]float64{0, 2048, 0})
		if !got.OK || got.Value != 2048 {
			t.Fatalf("got %+v, want exactly 2048", got)
		}
	})

	t.Run("stays stable across magnitudes", func(t *testing.T) {
		// 1e150 squared would overflow a naive product; log space must not.
		got := Geomean([]float64{1e150, 1e150})
		if !got.OK || got.Value <= 0 {
			t.Fatalf("got %+v", got)
		}
		closeTo(t, got.Value/math.Pow(10, 150), 1)
	})
}

func TestSMAAndMean(t *testing.T) {
	tests := []struct {
		name string
		in   []float64
		want float64
	}{
		{"even count", []float64{1, 2, 3, 4}, 2.5},
		{"odd count", []float64{2048, 2100, 1980}, 2042.6666666666667},
		{"single value", []float64{512}, 512},
		{"empty is zero", nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			closeTo(t, SMA(tc.in), tc.want)
			closeTo(t, Mean(tc.in), tc.want)
		})
	}
}

func TestEMA(t *testing.T) {
	t.Run("alpha comes from the span", func(t *testing.T) {
		if got := EMAAlpha(5); got != 2.0/6.0 {
			t.Fatalf("EMAAlpha(5) = %v, want 1/3", got)
		}
		if got := EMAAlpha(1); got != 1 {
			t.Fatalf("EMAAlpha(1) = %v, want 1", got)
		}
		if got := EMAAlpha(0); got != 0 {
			t.Fatalf("EMAAlpha(0) = %v, want 0", got)
		}
	})

	t.Run("hand computed with span 5", func(t *testing.T) {
		// alpha = 1/3, seeded with 10:
		//   e1 = 10 + (20-10)/3        = 40/3
		//   e2 = 40/3 + (30-40/3)/3    = 170/9
		closeTo(t, EMASpan([]float64{10, 20, 30}, 5), 170.0/9.0)
	})

	t.Run("hand computed with span 3", func(t *testing.T) {
		// alpha = 1/2, seeded with 10: e1 = 15.
		if got := EMASpan([]float64{10, 20}, 3); got != 15 {
			t.Fatalf("EMA = %v, want 15", got)
		}
	})

	t.Run("is seeded with the first value", func(t *testing.T) {
		if got := EMASpan([]float64{2048}, 5); got != 2048 {
			t.Fatalf("EMA of a single value = %v, want 2048", got)
		}
		if got := EMASpan(nil, 5); got != 0 {
			t.Fatalf("EMA of an empty slice = %v, want 0", got)
		}
	})

	t.Run("span 1 tracks the latest value", func(t *testing.T) {
		if got := EMASpan([]float64{10, 20, 30}, 1); got != 30 {
			t.Fatalf("EMA with span 1 = %v, want 30", got)
		}
	})

	t.Run("alpha of zero freezes the seed", func(t *testing.T) {
		if got := EMA([]float64{10, 20, 30}, 0); got != 10 {
			t.Fatalf("EMA with alpha 0 = %v, want 10", got)
		}
	})
}
