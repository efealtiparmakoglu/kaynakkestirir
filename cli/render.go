package cli

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"

	"kaynakkestirir/model"
)

// dash marks a value that could not be computed.
const dash = "-"

// writeText renders the human-readable report: one table row per scope, then
// the anomalies, then anything that went wrong with the input.
func writeText(w io.Writer, report Report) error {
	s := report.Settings
	fmt.Fprintf(w, "kaynakkestirir %s  scope=%s metric=%s window=%d alpha=%s z=%s ema-span=%d horizon=%d drop-anomalies=%s\n",
		Version, s.Scope, s.Metric, s.Window, fmtNum(s.Alpha), fmtNum(s.Z), s.EMASpan, s.Horizon, boolStr(s.DropAnomalies))
	fmt.Fprintf(w, "records: %d read, %d accepted, %d without %s, %d rejected\n\n",
		report.Records.Total, report.Records.Accepted, report.Records.SkippedNoMetric, s.Metric, report.Records.Errors)

	if len(report.Scopes) == 0 {
		fmt.Fprintln(w, "no scopes: nothing to predict (input held no usable records)")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "STATUS\tSCOPE\tN\tMEDIAN\tGEOMEAN\tCENTER\tBAND\tSMA\tEMA")
		ok, insufficient := 0, 0
		for _, r := range report.Scopes {
			fmt.Fprintln(tw, scopeRow(r))
			if r.Status == model.StatusOK {
				ok++
			} else {
				insufficient++
			}
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(w, "\nscopes: %d (%d predicted, %d insufficient)\n", len(report.Scopes), ok, insufficient)
	}

	writeAnomalies(w, report)
	if report.Records.Errors > 0 {
		fmt.Fprintf(w, "\nrecord errors: %d\n", report.Records.Errors)
		for _, e := range report.RecordErrors {
			fmt.Fprintf(w, "  %s\n", e.String())
		}
		if report.Records.Errors > len(report.RecordErrors) {
			fmt.Fprintf(w, "  ... and %d more\n", report.Records.Errors-len(report.RecordErrors))
		}
	}

	return nil
}

func scopeRow(r model.ScopeResult) string {
	status := r.Status
	scope := r.Scope + ":" + r.Key
	if r.Window == nil {
		return strings.Join([]string{status, scope, "0", dash, dash, dash, dash, dash, dash}, "\t")
	}
	w := r.Window
	geo := dash
	if w.Geomean != nil {
		geo = fmtNum(*w.Geomean)
	}
	if w.GeomeanExcluded > 0 {
		geo = fmt.Sprintf("%s (%d excluded)", geo, w.GeomeanExcluded)
	}
	return strings.Join([]string{
		status,
		scope,
		strconv.Itoa(w.N),
		fmtNum(w.Median),
		geo,
		fmtNum(w.Center),
		band(w.Low, w.High),
		fmtNum(w.SMA),
		fmtNum(w.EMA),
	}, "\t")
}

func writeAnomalies(w io.Writer, report Report) {
	fmt.Fprintf(w, "\nanomalies: %d\n", report.AnomalyTotal)
	for _, r := range report.Scopes {
		if len(r.Anomalies) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n  %s: %d\n", r.Scope+":"+r.Key, len(r.Anomalies))
		shown := r.Anomalies
		truncated := false
		if len(shown) > maxReportedRecordErrors {
			shown = shown[:maxReportedRecordErrors]
			truncated = true
		}
		for _, a := range shown {
			fmt.Fprintf(w, "    %s\n", anomalyLine(a))
		}
		if truncated {
			fmt.Fprintf(w, "    ... and %d more\n", len(r.Anomalies)-len(shown))
		}
	}
	if report.AnomalyTotal == 0 {
		fmt.Fprintln(w, "  none")
	}
}

func anomalyLine(a model.Anomaly) string {
	file := a.File
	if file == "" {
		file = "(repo)"
	}
	return fmt.Sprintf("ANOMALY t=%s file=%s v=%s outside [%s, %s]",
		model.FormatTime(a.T), file, fmtNum(a.V), fmtNum(a.Low), fmtNum(a.High))
}

func band(low, high float64) string {
	return "[" + fmtNum(low) + ", " + fmtNum(high) + "]"
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// fmtNum renders a number for a report: integers stay integers, fractions are
// trimmed to three decimals, and the degenerate cases are spelled out rather
// than printed as NaN.
func fmtNum(v float64) string {
	switch {
	case math.IsNaN(v):
		return "nan"
	case math.IsInf(v, 1):
		return "+inf"
	case math.IsInf(v, -1):
		return "-inf"
	}
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}
