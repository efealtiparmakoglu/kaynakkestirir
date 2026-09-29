package model

import (
	"sort"
	"time"
)

// Scope names for the --scope flag.
const (
	ScopeFile = "file"
	ScopeRepo = "repo"
)

// Series is one chronological series of measurements belonging to a single
// scope: one file, or the repository as a whole.
type Series struct {
	// Key identifies the series in the report, e.g. "src/app.py" or "repo".
	Key string
	// File is the file the series belongs to; empty for repository scope.
	File string
	// Times are the observation times in ascending order.
	Times []time.Time
	// Values are the measurements in ascending time order.
	Values []float64
}

// Len is the number of observations in the series.
func (s Series) Len() int { return len(s.Values) }

// sortRecordsStable orders records by timestamp ascending, keeping input order
// among records that share a timestamp. The sort is stable, so ordering is a
// pure function of the input and repeated runs are byte-identical.
func sortRecordsStable(recs []Record) {
	sort.SliceStable(recs, func(i, j int) bool {
		return recs[i].T.Before(recs[j].T)
	})
}

// BuildSeries groups time-sorted records into the requested scope.
//
//	file scope: one series per file, holding that file's measurements in time
//	           order. Series are returned in sorted path order.
//	repo scope: one single series, holding the per-timestamp SUM of the metric
//	           across all files, in time order. Measurements sharing a
//	           timestamp are folded into a single observation, so a commit that
//	           touches five files contributes one repo observation, not five.
//
// metric selects which measurement builds the series; records that do not
// carry it must already have been removed by FilterSeries.
func BuildSeries(records []Record, scope, metric string) []Series {
	switch scope {
	case ScopeFile:
		return buildFileSeries(records, metric)
	case ScopeRepo:
		return buildRepoSeries(records, metric)
	default:
		return nil
	}
}

func buildFileSeries(records []Record, metric string) []Series {
	byFile := make(map[string]*Series)
	for _, r := range records {
		s, ok := byFile[r.File]
		if !ok {
			s = &Series{Key: r.File, File: r.File}
			byFile[r.File] = s
		}
		s.Times = append(s.Times, r.T)
		s.Values = append(s.Values, r.Value(metric))
	}
	out := make([]Series, 0, len(byFile))
	for _, s := range byFile {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func buildRepoSeries(records []Record, metric string) []Series {
	if len(records) == 0 {
		return nil
	}
	s := Series{Key: ScopeRepo}
	var cur time.Time
	haveCur := false
	var sum float64
	flush := func() {
		if !haveCur {
			return
		}
		s.Times = append(s.Times, cur)
		s.Values = append(s.Values, sum)
	}
	for _, r := range records {
		if haveCur && r.T.Equal(cur) {
			sum += r.Value(metric)
			continue
		}
		flush()
		cur = r.T
		sum = r.Value(metric)
		haveCur = true
	}
	flush()
	return []Series{s}
}

// FilterSeries splits a record set into the records usable for the requested
// metric and the number skipped for lacking it. A missing line count is not an
// error: it only removes the record from LOC-based series.
func FilterSeries(records []Record, metric string) ([]Record, int) {
	kept := make([]Record, 0, len(records))
	skipped := 0
	for _, r := range records {
		if !r.HasMetric(metric) {
			skipped++
			continue
		}
		kept = append(kept, r)
	}
	return kept, skipped
}
