package cmd

import "time"

const datetimeFmt = "Jan 2, 2006 15:04:05"

// fmtTime formats a time.Time for human-readable display.
func fmtTime(t time.Time) string {
	return t.Format(datetimeFmt)
}

// fmtTimePTime formats a *time.Time, returns "" if nil.
func fmtTimePTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(datetimeFmt)
}

// fmtTimeStr parses an RFC3339 string and formats it for human-readable display.
// Falls back to the raw string if parsing fails.
func fmtTimeStr(s string) string {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		// try without nanoseconds
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return s
		}
	}
	return t.Format(datetimeFmt)
}

// fmtTimePStr formats a *string RFC3339, returns "" if nil.
func fmtTimePStr(s *string) string {
	if s == nil {
		return ""
	}
	return fmtTimeStr(*s)
}
