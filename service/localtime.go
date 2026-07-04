// Package service provides time and date formatting utilities that mirror
// the TS localTimeService.ts behavior.
package service

import (
	"fmt"
	"time"
)

// FormatUtcSqlDateTime formats a time as "YYYY-MM-DD HH:MM:SS" in UTC.
// Mirrors TS formatUtcSqlDateTime().
func FormatUtcSqlDateTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// FormatLocalDateTime formats a time in the local timezone as "2006-01-02 15:04:05".
// Mirrors TS formatLocalDateTime().
func FormatLocalDateTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// FormatLocalDate formats a time in the local timezone as "2006-01-02".
// Mirrors TS formatLocalDate().
func FormatLocalDate(t time.Time) string {
	return t.Format("2006-01-02")
}

// GetResolvedTimeZone returns the current system timezone identifier.
// Mirrors TS getResolvedTimeZone().
func GetResolvedTimeZone() string {
	zone, _ := time.Now().Zone()
	return zone
}

// LocalDayRange holds the day range boundaries.
type LocalDayRange struct {
	LocalDay string
	StartUTC string
	EndUTC   string
}

// GetLocalDayRangeUTC returns the local day string and its UTC boundary timestamps.
// Mirrors TS getLocalDayRangeUtc().
func GetLocalDayRangeUTC(t time.Time) LocalDayRange {
	localDay := FormatLocalDate(t)

	// Start of local day in local time
	y, m, d := t.Date()
	startLocal := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	endLocal := time.Date(y, m, d, 23, 59, 59, 999999999, t.Location())

	return LocalDayRange{
		LocalDay: localDay,
		StartUTC: startLocal.UTC().Format(time.RFC3339),
		EndUTC:   endLocal.UTC().Format(time.RFC3339),
	}
}

// GetTodayUnixSecondsRange returns the start and end of today as Unix timestamps.
// Mirrors TS getTodayUnixSecondsRange().
func GetTodayUnixSecondsRange(t time.Time) (start, end int64) {
	y, m, d := t.Date()
	startTime := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	endTime := time.Date(y, m, d, 23, 59, 59, 999999999, t.Location())
	return startTime.Unix(), endTime.Unix()
}

// BuildTimeFootnote builds the time footnote string for notifications.
func BuildTimeFootnote(t time.Time) string {
	timeZone := GetResolvedTimeZone()
	return fmt.Sprintf("Local Time: %s (%s)\nUTC Time: %s",
		FormatLocalDateTime(t), timeZone, t.UTC().Format(time.RFC3339))
}
