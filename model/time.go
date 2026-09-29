package model

import "time"

// isoLayout renders timestamps as second-precision ISO 8601 in UTC.
const isoLayout = "2006-01-02T15:04:05Z"

// FormatTime renders a timestamp as ISO 8601 in UTC, the single time format the
// tool ever prints.
func FormatTime(t time.Time) string {
	return t.UTC().Format(isoLayout)
}
