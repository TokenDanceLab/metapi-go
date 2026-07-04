package routing

import (
	"sort"
)

// GetRoundRobinCandidates sorts candidates by lastSelectedAt || lastUsedAt ascending.
func GetRoundRobinCandidates(candidates []RouteChannelCandidate) []RouteChannelCandidate {
	sorted := make([]RouteChannelCandidate, len(candidates))
	copy(sorted, candidates)
	sort.SliceStable(sorted, func(i, j int) bool {
		left := sorted[i]
		right := sorted[j]
		lo := left.Channel.LastSelectedAt
		if lo == nil {
			lo = left.Channel.LastUsedAt
		}
		ro := right.Channel.LastSelectedAt
		if ro == nil {
			ro = right.Channel.LastUsedAt
		}
		cmp := CompareNullableTimeAsc(lo, ro)
		if cmp != 0 {
			return cmp < 0
		}
		cmp = CompareNullableTimeAsc(left.Channel.LastUsedAt, right.Channel.LastUsedAt)
		if cmp != 0 {
			return cmp < 0
		}
		return left.Channel.ID < right.Channel.ID
	})
	return sorted
}

// SelectRoundRobinCandidate picks the first candidate in round-robin order.
func SelectRoundRobinCandidate(candidates []RouteChannelCandidate) *RouteChannelCandidate {
	ordered := GetRoundRobinCandidates(candidates)
	if len(ordered) == 0 {
		return nil
	}
	return &ordered[0]
}

// ApplyRoundRobinCooldown applies tiered cooldown to a failure-aware struct.
// Increments consecutiveFailCount, and if threshold is reached, increments cooldownLevel and resets.
// Returns the updated values and cooldownUntil ISO string.
func ApplyRoundRobinCooldown(
	consecutiveFailCount int64,
	cooldownLevel int64,
	nowMs int64,
	configuredMaxSec int,
) (nextConsecutiveFailCount int64, nextCooldownLevel int64, cooldownUntilISO *string) {
	nextConsecutiveFailCount = consecutiveFailCount + 1
	nextCooldownLevel = cooldownLevel

	if nextConsecutiveFailCount >= RoundRobinFailureThreshold {
		nextCooldownLevel = min64(cooldownLevel+1, int64(len(RoundRobinCooldownLevelsSec)-1))
		cooldownSec := ResolveRoundRobinCooldownSec(int(nextCooldownLevel))
		if cooldownSec > 0 {
			untilMs := nowMs + ClampFailureCooldownMs(cooldownSec*1000, configuredMaxSec)
			iso := timeFromMs(untilMs)
			cooldownUntilISO = &iso
		}
		nextConsecutiveFailCount = 0
	}
	return
}

// ApplyFibonacciCooldown applies Fibonacci backoff cooldown.
func ApplyFibonacciCooldown(failCount int64, nowMs int64, configuredMaxSec int) (cooldownUntilISO *string) {
	fc := failCount
	effectiveMs := ResolveEffectiveFailureCooldownMs(&fc, configuredMaxSec)
	untilMs := nowMs + effectiveMs
	iso := timeFromMs(untilMs)
	return &iso
}

// timeFromMs formats Unix milliseconds as ISO 8601.
func timeFromMs(ms int64) string {
	return timeMsToISO(ms)
}

func timeMsToISO(ms int64) string {
	// format as "2006-01-02T15:04:05.000Z"
	t := secToTime(ms / 1000)
	ns := (ms % 1000) * 1000000
	return t.Format("2006-01-02T15:04:05") + "." + pad3(int(ns/1000000)) + "Z"
}

func secToTime(sec int64) timeTime {
	return timeUnix(sec, 0)
}

type timeTime struct {
	unix int64
}

func timeUnix(sec int64, nsec int64) timeTime {
	return timeTime{unix: sec}
}

func (t timeTime) Format(layout string) string {
	// Compute the date/time parts from unix seconds
	// This is a simplified implementation
	sec := t.unix
	// Adjust for days since Unix epoch (1970-01-01)
	day := sec / 86400
	timeOfDay := sec % 86400
	if timeOfDay < 0 {
		timeOfDay += 86400
		day--
	}

	// Compute year/month/day from day count
	year, month, mday := daysToDate(day)
	hour := timeOfDay / 3600
	minute := (timeOfDay % 3600) / 60
	second := timeOfDay % 60

	switch layout {
	case "2006-01-02T15:04:05":
		return formatInt2(int64(year)) + "-" + formatInt2(int64(month)) + "-" + formatInt2(int64(mday)) +
			"T" + formatInt2(hour) + ":" + formatInt2(minute) + ":" + formatInt2(second)
	default:
		return ""
	}
}

func formatInt2(v int64) string {
	s := formatInt(v)
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

func pad3(v int) string {
	s := formatInt(int64(v))
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

func daysToDate(days int64) (year int, month int, day int) {
	// Algorithm from Howard Hinnant
	days += 719468
	era := days / 146097
	if days < 0 {
		era--
	}
	doe := days - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp + 3
	if m > 12 {
		m -= 12
		y++
	}
	return int(y), int(m), int(d)
}
