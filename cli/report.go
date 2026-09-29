package cli

import (
	"kaynakkestirir/model"
)

// Version is the tool version, reported by -version.
const Version = "1.0.0"

// Report is the machine-readable result of a run.
type Report struct {
	// Scopes holds one entry per scope, in report order.
	Scopes []model.ScopeResult `json:"scopes"`
	// Settings echoes every knob the run was made with, so a stored report can
	// be reproduced.
	Settings ReportSettings `json:"settings"`
	// Records summarises what happened to the input.
	Records RecordsSummary `json:"records"`
	// RecordErrors lists up to the first rejected lines, with line numbers.
	RecordErrors []model.RecordError `json:"record_errors"`
	// AnomalyTotal is the number of anomalies across all scopes.
	AnomalyTotal int `json:"anomaly_total"`
}

// ReportSettings is the resolved configuration of a run.
type ReportSettings struct {
	Window        int     `json:"window"`
	Alpha         float64 `json:"alpha"`
	Z             float64 `json:"z"`
	EMASpan       int     `json:"ema_span"`
	Horizon       int     `json:"horizon"`
	Scope         string  `json:"scope"`
	Metric        string  `json:"metric"`
	DropAnomalies bool    `json:"drop_anomalies"`
	Input         string  `json:"input"`
}

// RecordsSummary counts the input lines and what became of them.
type RecordsSummary struct {
	// Total is the number of non-blank input lines.
	Total int `json:"total"`
	// Accepted is the number of records that became observations.
	Accepted int `json:"accepted"`
	// SkippedNoMetric counts accepted records that lacked the selected metric
	// and therefore did not enter any series.
	SkippedNoMetric int `json:"skipped_no_metric"`
	// Errors is the number of rejected lines.
	Errors int `json:"errors"`
}

func settingsJSON(opt options) ReportSettings {
	return ReportSettings{
		Window:        opt.settings.Window,
		Alpha:         opt.settings.Alpha,
		Z:             opt.settings.Z,
		EMASpan:       opt.settings.EMASpan,
		Horizon:       opt.settings.Horizon,
		Scope:         opt.settings.Scope,
		Metric:        opt.settings.Metric,
		DropAnomalies: opt.settings.DropAnomalies,
		Input:         opt.input,
	}
}
