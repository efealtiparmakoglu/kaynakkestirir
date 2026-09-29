// Package model parses timestamped code measurements, groups them into scopes,
// and fits the robust next-value model with its uncertainty band.
package model

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Metric names the quantity a series is built from.
const (
	MetricBytes = "bytes"
	MetricLoc   = "loc"
)

// maxLineBytes bounds a single JSONL record so that a pathological input cannot
// exhaust memory. Real commit-measurement records are far below this.
const maxLineBytes = 4 << 20

// Record is one timestamped code measurement.
type Record struct {
	// Seq is the zero-based position of the record in the input. It breaks ties
	// between records sharing a timestamp, preserving input order.
	Seq int
	// Line is the one-based input line the record was parsed from.
	Line int
	// T is the observation time, normalised to UTC.
	T time.Time
	// File is the path the measurement belongs to.
	File string
	// Bytes is the measured size; always present and never negative.
	Bytes float64
	// Loc is the optional line count; nil when the record omitted it.
	Loc *float64
}

// Value returns the measurement selected by the metric name.
func (r Record) Value(metric string) float64 {
	if metric == MetricLoc && r.Loc != nil {
		return *r.Loc
	}
	return r.Bytes
}

// HasMetric reports whether the record carries the requested metric. A record
// without a line count is skipped by LOC-based series but still usable for the
// byte-based series.
func (r Record) HasMetric(metric string) bool {
	return metric != MetricLoc || r.Loc != nil
}

// RecordError is a single input line that could not be turned into a record.
type RecordError struct {
	Line    int
	Message string
}

func (e RecordError) String() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Message)
}

// ParseResult is the outcome of reading a JSONL stream.
type ParseResult struct {
	// Records are the accepted measurements in input order.
	Records []Record
	// Errors are the rejected lines, in input order.
	Errors []RecordError
	// Total is the number of non-blank input lines seen.
	Total int
	// SkippedNoMetric counts records that lack the requested metric; they are
	// not errors, they simply cannot contribute to the selected series.
	SkippedNoMetric int
}

// Sort orders the records by timestamp ascending, breaking ties by input
// order. The sort is stable so that equal timestamps keep the order in which
// they appeared in the file.
func (p *ParseResult) Sort() {
	sortRecordsStable(p.Records)
}

type rawRecord struct {
	T     json.RawMessage `json:"t"`
	File  *string         `json:"file"`
	Bytes json.RawMessage `json:"bytes"`
	Loc   json.RawMessage `json:"loc"`
}

// ParseJSONL reads JSONL measurements from r. Every line is independent: a
// malformed line is recorded as an error and skipped, the rest of the stream is
// still processed, so one bad commit record never costs the whole run.
func ParseJSONL(r io.Reader) ParseResult {
	var res ParseResult
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	line := 0
	for sc.Scan() {
		line++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		res.Total++
		rec, err := parseLine(raw)
		if err != nil {
			res.Errors = append(res.Errors, RecordError{Line: line, Message: err.Error()})
			continue
		}
		rec.Seq = len(res.Records)
		rec.Line = line
		res.Records = append(res.Records, rec)
	}
	if err := sc.Err(); err != nil {
		res.Errors = append(res.Errors, RecordError{Line: line + 1, Message: "read error: " + err.Error()})
	}
	return res
}

func parseLine(raw []byte) (Record, error) {
	var rr rawRecord
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&rr); err != nil {
		return Record{}, fmt.Errorf("malformed JSON: %v", err)
	}
	if dec.More() {
		return Record{}, errors.New("trailing content after JSON object")
	}

	var rec Record
	t, err := parseTime(rr.T)
	if err != nil {
		return Record{}, err
	}
	rec.T = t

	if rr.File == nil {
		return Record{}, errors.New(`missing "file"`)
	}
	if strings.TrimSpace(*rr.File) == "" {
		return Record{}, errors.New(`empty "file"`)
	}
	rec.File = *rr.File

	bytesVal, err := parseNonNegative(rr.Bytes, "bytes", true)
	if err != nil {
		return Record{}, err
	}
	rec.Bytes = bytesVal

	if len(rr.Loc) > 0 && !isJSONNull(rr.Loc) {
		locVal, err := parseNonNegative(rr.Loc, "loc", false)
		if err != nil {
			return Record{}, err
		}
		rec.Loc = &locVal
	}
	return rec, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

func parseNonNegative(raw json.RawMessage, field string, required bool) (float64, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || isJSONNull(trimmed) {
		if required {
			return 0, fmt.Errorf("missing %q", field)
		}
		return 0, nil
	}
	if trimmed[0] == '"' {
		return 0, fmt.Errorf("%q is not a number", field)
	}
	v, err := strconv.ParseFloat(string(trimmed), 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", field)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%q is not a finite number", field)
	}
	if v < 0 {
		return 0, fmt.Errorf("%q is negative (%v)", field, v)
	}
	return v, nil
}

var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// parseTime accepts either a JSON string holding an ISO 8601 timestamp or a
// JSON number holding epoch seconds. A timestamp without a zone is read as UTC;
// every result is normalised to UTC.
func parseTime(raw json.RawMessage) (time.Time, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || isJSONNull(trimmed) {
		return time.Time{}, errors.New(`missing "t"`)
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return time.Time{}, fmt.Errorf(`malformed "t": %v`, err)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return time.Time{}, errors.New(`empty "t"`)
		}
		for _, layout := range timeLayouts {
			if ts, err := time.Parse(layout, s); err == nil {
				return ts.UTC(), nil
			}
		}
		return time.Time{}, fmt.Errorf(`"t" %q is not a recognised ISO 8601 timestamp`, s)
	}
	secs, err := strconv.ParseFloat(string(trimmed), 64)
	if err != nil || math.IsNaN(secs) || math.IsInf(secs, 0) {
		return time.Time{}, fmt.Errorf(`"t" %s is neither an ISO 8601 string nor epoch seconds`, string(trimmed))
	}
	whole, frac := math.Modf(secs)
	sec := int64(whole)
	nsec := int64(math.Round(frac * 1e9))
	if nsec >= 1e9 {
		sec++
		nsec -= 1e9
	}
	return time.Unix(sec, nsec).UTC(), nil
}
