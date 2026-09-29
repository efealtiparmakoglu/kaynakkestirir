package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cleanHistory is a small, valid history: one file with six byte measurements
// (the last one far out of line) and a second file with a single record.
const cleanHistory = `{"t": "2026-01-01T10:00:00Z", "file": "src/app.py", "bytes": 2048, "loc": 120}
{"t": "2026-01-02T10:00:00Z", "file": "src/app.py", "bytes": 2100, "loc": 126}
{"t": "2026-01-03T10:00:00Z", "file": "src/app.py", "bytes": 1980, "loc": 118}
{"t": "2026-01-04T10:00:00Z", "file": "src/app.py", "bytes": 2050, "loc": 121}
{"t": "2026-01-05T10:00:00Z", "file": "src/app.py", "bytes": 9000, "loc": 500}
{"t": "2026-01-03T10:00:00Z", "file": "README.md", "bytes": 512}
`

// dirtyHistory is cleanHistory plus one rejected line.
const dirtyHistory = cleanHistory + `{"t": "2026-01-06T10:00:00Z", "file": "src/app.py", "bytes": -1}
`

func writeFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("cannot write fixture: %v", err)
	}
	return path
}

// run executes the tool in-process and returns the exit code and both streams.
func run(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errBuf strings.Builder
	code := Run(args, strings.NewReader(stdin), &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestUsageExitCodes(t *testing.T) {
	t.Run("help and version succeed", func(t *testing.T) { testVersionAndHelp(t) })

	path := writeFixture(t, "history.jsonl", cleanHistory)
	other := writeFixture(t, "other.jsonl", cleanHistory)

	tests := []struct {
		name string
		args []string
	}{
		{"no arguments at all", nil},
		{"empty input name", []string{""}},
		{"window below two", []string{"-window", "1", path}},
		{"negative window", []string{"-window", "-4", path}},
		{"alpha above one", []string{"-alpha", "1.5", path}},
		{"negative alpha", []string{"-alpha", "-0.5", path}},
		{"negative z", []string{"-z", "-1", path}},
		{"ema span of zero", []string{"-ema-span", "0", path}},
		{"horizon of zero", []string{"-horizon", "0", path}},
		{"unknown scope", []string{"-scope", "galaxy", path}},
		{"unknown metric", []string{"-metric", "tokens", path}},
		{"unknown flag", []string{"-verbose", path}},
		{"flag with a bad value", []string{"-window", "many", path}},
		{"two input files", []string{path, other}},
		{"input given twice", []string{"-input", path, other}},
		{"missing file", []string{filepath.Join(t.TempDir(), "absent.jsonl")}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := run(t, "", tc.args...)
			if code != ExitUsage {
				t.Fatalf("exit code = %d, want %d", code, ExitUsage)
			}
			if strings.Contains(stdout, "STATUS") {
				t.Fatalf("a usage error must not produce a report:\n%s", stdout)
			}
			if stderr == "" {
				t.Fatal("a usage error must say something on stderr")
			}
		})
	}
}

func TestExitCodesForRecords(t *testing.T) {
	t.Run("clean history exits 0", func(t *testing.T) {
		path := writeFixture(t, "history.jsonl", cleanHistory)
		code, stdout, stderr := run(t, "", path)
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
		}
		if !strings.Contains(stdout, "file:src/app.py") {
			t.Fatalf("report does not mention the scope:\n%s", stdout)
		}
	})

	t.Run("a rejected record exits 1 but still reports", func(t *testing.T) {
		path := writeFixture(t, "history.jsonl", dirtyHistory)
		code, stdout, _ := run(t, "", path)
		if code != ExitRecordErrors {
			t.Fatalf("exit code = %d, want %d", code, ExitRecordErrors)
		}
		if !strings.Contains(stdout, "file:src/app.py") {
			t.Fatalf("the report must still be produced:\n%s", stdout)
		}
		if !strings.Contains(stdout, "record errors: 1") {
			t.Fatalf("the rejected line is not reported:\n%s", stdout)
		}
		if !strings.Contains(stdout, `"bytes" is negative`) {
			t.Fatalf("the reason is not reported:\n%s", stdout)
		}
	})

	t.Run("an empty input is not an error", func(t *testing.T) {
		path := writeFixture(t, "empty.jsonl", "\n\n")
		code, stdout, _ := run(t, "", path)
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d", code, ExitOK)
		}
		if !strings.Contains(stdout, "no scopes") {
			t.Fatalf("expected an explicit empty report:\n%s", stdout)
		}
	})
}

func TestTextReportContent(t *testing.T) {
	path := writeFixture(t, "history.jsonl", dirtyHistory)
	code, stdout, _ := run(t, "", "-window", "4", "-z", "1.5", path)
	if code != ExitRecordErrors {
		t.Fatalf("exit code = %d, want %d", code, ExitRecordErrors)
	}

	for _, want := range []string{
		"scope=file metric=bytes window=4 alpha=0.5 z=1.5",
		"STATUS",
		"GEOMEAN",
		"insufficient",
		"file:README.md",
		"scopes: 2 (1 predicted, 1 insufficient)",
		"ANOMALY t=2026-01-05T10:00:00Z file=src/app.py v=9000 outside [",
		"anomalies: 2",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("report is missing %q:\n%s", want, stdout)
		}
	}

	// The band is printed as [low, high] with no exponent notation.
	if !strings.Contains(stdout, "[") || !strings.Contains(stdout, "]") {
		t.Fatalf("no band in the report:\n%s", stdout)
	}
	if strings.Contains(stdout, "e+") {
		t.Fatalf("numbers must not be printed in exponent form:\n%s", stdout)
	}
}

func TestJSONReportShape(t *testing.T) {
	path := writeFixture(t, "history.jsonl", dirtyHistory)
	code, stdout, _ := run(t, "", "-json", "-window", "4", path)
	if code != ExitRecordErrors {
		t.Fatalf("exit code = %d, want %d", code, ExitRecordErrors)
	}

	var report struct {
		Scopes []struct {
			Scope  string `json:"scope"`
			Key    string `json:"key"`
			Status string `json:"status"`
			Window *struct {
				N              int      `json:"n"`
				Median         float64  `json:"median"`
				Geomean        *float64 `json:"geomean"`
				GeomeanExclude int      `json:"geomean_excluded"`
				Center         float64  `json:"center"`
				Low            float64  `json:"low"`
				High           float64  `json:"high"`
				SMA            float64  `json:"sma"`
				EMA            float64  `json:"ema"`
			} `json:"window"`
			Predictions []struct {
				Step   int     `json:"step"`
				Center float64 `json:"center"`
				Low    float64 `json:"low"`
				High   float64 `json:"high"`
			} `json:"predictions"`
			Anomalies []struct {
				Step int     `json:"step"`
				T    string  `json:"t"`
				V    float64 `json:"v"`
			} `json:"anomalies"`
		} `json:"scopes"`
		Settings struct {
			Window int     `json:"window"`
			Alpha  float64 `json:"alpha"`
			Scope  string  `json:"scope"`
			Metric string  `json:"metric"`
			Drop   bool    `json:"drop_anomalies"`
		} `json:"settings"`
		Records struct {
			Total    int `json:"total"`
			Accepted int `json:"accepted"`
			Errors   int `json:"errors"`
		} `json:"records"`
		RecordErrors []struct {
			Line    int    `json:"line"`
			Message string `json:"message"`
		} `json:"record_errors"`
		AnomalyTotal int `json:"anomaly_total"`
	}

	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}

	if report.Settings.Window != 4 || report.Settings.Alpha != 0.5 {
		t.Fatalf("settings = %+v", report.Settings)
	}
	if report.Settings.Scope != "file" || report.Settings.Metric != "bytes" || report.Settings.Drop {
		t.Fatalf("settings = %+v", report.Settings)
	}
	if report.Records.Total != 7 || report.Records.Accepted != 6 || report.Records.Errors != 1 {
		t.Fatalf("records = %+v", report.Records)
	}
	if len(report.RecordErrors) != 1 || report.RecordErrors[0].Line != 7 {
		t.Fatalf("record_errors = %+v", report.RecordErrors)
	}
	if len(report.Scopes) != 2 {
		t.Fatalf("got %d scopes, want 2", len(report.Scopes))
	}

	var app, readme int = -1, -1
	for i, s := range report.Scopes {
		switch s.Key {
		case "src/app.py":
			app = i
		case "README.md":
			readme = i
		default:
			t.Fatalf("unexpected scope key %q", s.Key)
		}
	}
	if app < 0 || readme < 0 {
		t.Fatalf("missing a scope in %+v", report.Scopes)
	}

	a := report.Scopes[app]
	if a.Scope != "file" || a.Status != "ok" {
		t.Fatalf("app scope = %+v", a)
	}
	if a.Window == nil || a.Window.N != 4 {
		t.Fatalf("window = %+v, want n=4", a.Window)
	}
	if a.Window.Geomean == nil {
		t.Fatal("geomean must not be null for an all positive window")
	}
	if a.Window.Low > a.Window.High || a.Window.Center < a.Window.Low || a.Window.Center > a.Window.High {
		t.Fatalf("band does not contain the centre: %+v", a.Window)
	}
	if len(a.Predictions) != 1 || a.Predictions[0].Step != 1 {
		t.Fatalf("predictions = %+v", a.Predictions)
	}
	if a.Predictions[0].Center != a.Window.Center {
		t.Fatal("the prediction must match the fitted window")
	}
	// The 1980 dip and the 9000 spike both leave the band their prior
	// observations implied; the five surrounding 2000-ish values do not.
	if len(a.Anomalies) != 2 {
		t.Fatalf("anomalies = %+v, want 2", a.Anomalies)
	}
	if a.Anomalies[0].Step != 3 || a.Anomalies[0].V != 1980 {
		t.Fatalf("first anomaly = %+v, want step 3 with v=1980", a.Anomalies[0])
	}
	if a.Anomalies[1].Step != 5 || a.Anomalies[1].V != 9000 {
		t.Fatalf("second anomaly = %+v, want step 5 with v=9000", a.Anomalies[1])
	}
	if a.Anomalies[1].T != "2026-01-05T10:00:00Z" {
		t.Fatalf("anomaly time = %q, want ISO 8601 UTC", a.Anomalies[1].T)
	}
	if report.AnomalyTotal != 2 {
		t.Fatalf("anomaly_total = %d, want 2", report.AnomalyTotal)
	}

	r := report.Scopes[readme]
	if r.Status != "insufficient" || len(r.Predictions) != 0 {
		t.Fatalf("README scope = %+v, want insufficient with no predictions", r)
	}

	t.Run("repo scope and a flat horizon", func(t *testing.T) { repoScopeAndHorizonInJSON(t) })
}

func repoScopeAndHorizonInJSON(t *testing.T) {
	path := writeFixture(t, "history.jsonl", cleanHistory)
	code, stdout, _ := run(t, "", "-json", "-scope", "repo", "-horizon", "3", path)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	var report struct {
		Scopes []struct {
			Scope       string           `json:"scope"`
			Key         string           `json:"key"`
			Window      *struct{ N int } `json:"window"`
			Predictions []struct {
				Step   int     `json:"step"`
				Center float64 `json:"center"`
				Low    float64 `json:"low"`
				High   float64 `json:"high"`
			} `json:"predictions"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(report.Scopes) != 1 {
		t.Fatalf("repo scope must produce exactly one series, got %d", len(report.Scopes))
	}
	s := report.Scopes[0]
	if s.Scope != "repo" || s.Key != "repo" {
		t.Fatalf("scope = %+v", s)
	}
	// Six records over five distinct timestamps: 2026-01-03 carries two files.
	if s.Window == nil || s.Window.N != 5 {
		t.Fatalf("window = %+v, want 5 observations", s.Window)
	}
	if len(s.Predictions) != 3 {
		t.Fatalf("got %d predictions, want 3", len(s.Predictions))
	}
	first := s.Predictions[0]
	for i, p := range s.Predictions {
		if p.Step != i+1 {
			t.Fatalf("prediction %d has step %d", i, p.Step)
		}
		if p.Center != first.Center || p.Low != first.Low || p.High != first.High {
			t.Fatalf("the horizon is not flat: %+v vs %+v", p, first)
		}
	}
}

func TestStdinInputAndDeterminism(t *testing.T) {
	t.Run("stdin", func(t *testing.T) {
		code, stdout, _ := run(t, cleanHistory, "-input", "-")
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d", code, ExitOK)
		}
		if !strings.Contains(stdout, "file:src/app.py") {
			t.Fatalf("stdin history was not read:\n%s", stdout)
		}
	})

	path := writeFixture(t, "history.jsonl", dirtyHistory)
	for _, mode := range [][]string{
		{"-window", "5", path},
		{"-window", "5", "-json", path},
		{"-scope", "repo", "-json", path},
		{"-scope", "repo", "-z", "2", "-drop-anomalies", path},
	} {
		t.Run(strings.Join(mode, " "), func(t *testing.T) {
			code1, out1, err1 := run(t, "", mode...)
			code2, out2, err2 := run(t, "", mode...)
			if code1 != code2 {
				t.Fatalf("exit codes differ: %d then %d", code1, code2)
			}
			if out1 != out2 {
				t.Fatalf("two identical runs produced different output:\n%s\n---\n%s", out1, out2)
			}
			if err1 != err2 {
				t.Fatalf("two identical runs produced different stderr: %q vs %q", err1, err2)
			}
			if out1 == "" {
				t.Fatal("expected output")
			}
		})
	}
}

func testVersionAndHelp(t *testing.T) {
	code, stdout, _ := run(t, "", "-version")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(stdout, Version) {
		t.Fatalf("version output = %q", stdout)
	}

	code, stdout, stderr := run(t, "", "-h")
	if code != ExitOK {
		t.Fatalf("-h exit code = %d, want %d", code, ExitOK)
	}
	if strings.Contains(stdout, "usage:") == false && !strings.Contains(stderr, "usage:") {
		t.Fatal("-h must print usage")
	}
}
