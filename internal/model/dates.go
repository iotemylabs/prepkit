package model

import (
	"fmt"
	"time"
)

// DateLayout is the ISO-8601 date form used everywhere a schema field says
// "format: date".
const DateLayout = "2006-01-02"

// ParseDate parses a strict ISO-8601 date (YYYY-MM-DD) in UTC. Date fields
// are stored and exported as strings; business logic parses them here, in Go,
// where timezone handling is explicit — never with SQLite date functions.
func ParseDate(s string) (time.Time, error) {
	t, err := time.ParseInLocation(DateLayout, s, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", s)
	}
	return t, nil
}

// ParseDateTime parses an RFC 3339 timestamp, the form used by exported_at.
func ParseDateTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q: expected RFC 3339", s)
	}
	return t, nil
}
