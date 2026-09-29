// Package cli wires the estimator together: flag parsing, input reading, and
// rendering of the human and JSON reports.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"kaynakkestirir/model"
)

// Exit codes. They are part of the tool's contract with whatever runs it.
const (
	// ExitOK means the run completed and no input record was rejected.
	ExitOK = 0
	// ExitRecordErrors means the report was produced but at least one input
	// line was rejected. The report is still written.
	ExitRecordErrors = 1
	// ExitUsage means the invocation or the input source was unusable, and no
	// report was produced.
	ExitUsage = 2
)

// maxReportedRecordErrors bounds how many rejected lines are echoed back, in
// both the human and the JSON report. The total count is always reported.
const maxReportedRecordErrors = 10

// options is the fully resolved command line.
type options struct {
	settings model.Settings
	input    string
	json     bool
	version  bool
}

// Run executes the tool and returns the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opt, code, err := parseArgs(args, stderr)
	if err != nil {
		if !errors.Is(err, errRequestHelp) {
			// Usage text was already printed for a flag level failure; every
			// other failure explains itself here.
			fmt.Fprintf(stderr, "kaynakkestirir: %v\n", err)
		}
		return code
	}
	if opt.version {
		fmt.Fprintf(stdout, "kaynakkestirir %s\n", Version)
		return ExitOK
	}

	reader, closeFn, err := openInput(opt.input, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "kaynakkestirir: %v\n", err)
		return ExitUsage
	}
	if closeFn != nil {
		defer closeFn()
	}

	parsed := model.ParseJSONL(reader)
	parsed.Sort()

	usable, skipped := model.FilterSeries(parsed.Records, opt.settings.Metric)
	series := model.BuildSeries(usable, opt.settings.Scope, opt.settings.Metric)
	results := model.FitAll(series, opt.settings)

	report := Report{
		Scopes:   results,
		Settings: settingsJSON(opt),
		Records: RecordsSummary{
			Total:           parsed.Total,
			Accepted:        len(parsed.Records),
			SkippedNoMetric: skipped,
			Errors:          len(parsed.Errors),
		},
		RecordErrors: capErrors(parsed.Errors),
		AnomalyTotal: totalAnomalies(results),
	}

	if opt.json {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "kaynakkestirir: %v\n", err)
			return ExitUsage
		}
	} else if err := writeText(stdout, report); err != nil {
		fmt.Fprintf(stderr, "kaynakkestirir: %v\n", err)
		return ExitUsage
	}

	if len(parsed.Errors) > 0 {
		return ExitRecordErrors
	}
	return ExitOK
}

func openInput(path string, stdin io.Reader) (io.Reader, func() error, error) {
	if path == "-" {
		return stdin, nil, nil
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read input %q: %w", path, err)
	}
	return f, f.Close, nil
}

func parseArgs(args []string, stderr io.Writer) (options, int, error) {
	opt := options{settings: model.DefaultSettings()}

	rest, code, err := parseFlags(args, &opt, stderr)
	if err != nil {
		return opt, code, err
	}

	// flag stops at the first non-flag argument, so "tool in.jsonl -window 5"
	// would leave the trailing flags looking like extra inputs. Re-parse the
	// tail until only the input path is left.
	for len(rest) > 1 {
		if opt.input != "" {
			return opt, ExitUsage, errors.New("input given both as a flag and as an argument")
		}
		opt.input = rest[0]
		rest, code, err = parseFlags(rest[1:], &opt, stderr)
		if err != nil {
			return opt, code, err
		}
	}
	if len(rest) == 1 {
		if opt.input != "" && opt.input != rest[0] {
			return opt, ExitUsage, errors.New("input given both as a flag and as an argument")
		}
		opt.input = rest[0]
	}

	if err := validate(opt); err != nil {
		return opt, ExitUsage, err
	}
	return opt, ExitOK, nil
}

// parseFlags builds a fresh flag set bound to opt and returns the remaining
// positional arguments.
func parseFlags(args []string, opt *options, stderr io.Writer) ([]string, int, error) {
	fs := flag.NewFlagSet("kaynakkestirir", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usageText) }

	fs.IntVar(&opt.settings.Window, "window", opt.settings.Window, "trailing window size in observations (min 2)")
	fs.Float64Var(&opt.settings.Alpha, "alpha", opt.settings.Alpha, "blend of median vs geomean, 0..1")
	fs.Float64Var(&opt.settings.Z, "z", opt.settings.Z, "band multiplier applied to the robust spread")
	fs.IntVar(&opt.settings.EMASpan, "ema-span", opt.settings.EMASpan, "EMA span, alpha=2/(span+1)")
	fs.IntVar(&opt.settings.Horizon, "horizon", opt.settings.Horizon, "number of steps to repeat the flat prediction")
	fs.StringVar(&opt.settings.Scope, "scope", opt.settings.Scope, "scope: file (per file) or repo (per-timestamp sum)")
	fs.StringVar(&opt.settings.Metric, "metric", opt.settings.Metric, "metric to model: bytes or loc")
	fs.BoolVar(&opt.settings.DropAnomalies, "drop-anomalies", false, "recompute the fit excluding flagged observations")
	fs.BoolVar(&opt.json, "json", false, "emit the machine-readable report")
	fs.StringVar(&opt.input, "input", opt.input, "input JSONL file, or - for stdin")
	fs.BoolVar(&opt.version, "version", false, "print the version and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, ExitOK, errRequestHelp
		}
		return nil, ExitUsage, err
	}
	return fs.Args(), ExitOK, nil
}

// errRequestHelp signals that usage was already printed and the process should
// stop successfully.
var errRequestHelp = errors.New("help requested")

func validate(opt options) error {
	if opt.version {
		// Asking for the version needs no input and no other setting.
		return nil
	}
	s := opt.settings
	if s.Window < 2 {
		return fmt.Errorf("-window must be at least 2, got %d", s.Window)
	}
	if s.Alpha < 0 || s.Alpha > 1 {
		return fmt.Errorf("-alpha must be between 0 and 1, got %g", s.Alpha)
	}
	if s.Z < 0 {
		return fmt.Errorf("-z must not be negative, got %g", s.Z)
	}
	if s.EMASpan < 1 {
		return fmt.Errorf("-ema-span must be at least 1, got %d", s.EMASpan)
	}
	if s.Horizon < 1 {
		return fmt.Errorf("-horizon must be at least 1, got %d", s.Horizon)
	}
	if s.Scope != model.ScopeFile && s.Scope != model.ScopeRepo {
		return fmt.Errorf("-scope must be %q or %q, got %q", model.ScopeFile, model.ScopeRepo, s.Scope)
	}
	if s.Metric != model.MetricBytes && s.Metric != model.MetricLoc {
		return fmt.Errorf("-metric must be %q or %q, got %q", model.MetricBytes, model.MetricLoc, s.Metric)
	}
	if opt.input == "" {
		return errors.New("missing input: pass a JSONL file as an argument or with -input (use - for stdin)")
	}
	return nil
}

func capErrors(errs []model.RecordError) []model.RecordError {
	if len(errs) <= maxReportedRecordErrors {
		if errs == nil {
			return []model.RecordError{}
		}
		return errs
	}
	return errs[:maxReportedRecordErrors]
}

func totalAnomalies(results []model.ScopeResult) int {
	n := 0
	for _, r := range results {
		n += len(r.Anomalies)
	}
	return n
}

func writeJSON(w io.Writer, report Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

const usageText = `usage: kaynakkestirir [flags] <history.jsonl>

Estimates the next code measurement for every scope from a trailing window of
timestamped measurements, with a robust uncertainty band and anomaly alerts.
Read JSONL records {"t":<ISO 8601 or epoch seconds>,"file":"path","bytes":N,"loc":N}
from the given file, or from stdin with "-".

flags:
  -window N        trailing window size in observations (default 30, min 2)
  -alpha A         blend of median vs geomean, 0..1 (default 0.5)
  -z Z             band multiplier on the robust spread (default 1)
  -ema-span S      EMA span, alpha=2/(span+1) (default 5)
  -horizon H       steps to repeat the flat prediction (default 1)
  -scope S         "file" (default, one prediction per file) or "repo"
                   (per-timestamp sum across files, one series)
  -metric M        "bytes" (default) or "loc"
  -drop-anomalies  recompute the fit excluding flagged observations
  -json            emit the machine-readable report
  -input PATH      input file; "-" reads stdin
  -version         print the version and exit

exit codes: 0 ok, 1 some input records were rejected, 2 usage error.
`
