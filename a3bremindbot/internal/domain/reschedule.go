package domain

import (
	"fmt"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/store"
)

// Reschedule computes adjusted times for remaining instances in a series
// based on the actual doneAt time, the minGap constraint, and the current time (now).
// It returns the adjusted times relative to now in the given location,
// and a warning if the last adjusted time exceeds midnight.
func Reschedule(reminder store.Reminder, doneAt time.Time, fromIndex int, loc *time.Location, now time.Time) (adjustedTimes []time.Time, warning string) {
	now = now.In(loc)
	// All times from fromIndex+1 onward
	remaining := reminder.Times[fromIndex+1:]
	if len(remaining) == 0 {
		return nil, ""
	}

	if reminder.MinGap == nil {
		// No gap constraint — return original times converted to time.Time for today.
		times := make([]time.Time, len(remaining))
		for i, tStr := range remaining {
			parsed, _ := time.ParseInLocation("15:04", tStr, loc)
			times[i] = time.Date(
				now.Year(), now.Month(), now.Day(),
				parsed.Hour(), parsed.Minute(), 0, 0,
				loc,
			)
		}
		return times, ""
	}

	minGap := time.Duration(*reminder.MinGap) * time.Minute
	times := make([]time.Time, len(remaining))

	prevTime := doneAt.In(loc)

	for i, tStr := range remaining {
		parsed, _ := time.ParseInLocation("15:04", tStr, loc)
		originalTime := time.Date(
			now.Year(), now.Month(), now.Day(),
			parsed.Hour(), parsed.Minute(), 0, 0,
			loc,
		)
		earliestNext := prevTime.Add(minGap)

		if originalTime.After(earliestNext) || originalTime.Equal(earliestNext) {
			times[i] = originalTime
		} else {
			times[i] = earliestNext
		}
		prevTime = times[i]
	}

	// Check if last adjusted time exceeds midnight (23:59:59 in the given location)
	midnight := time.Date(
		now.Year(), now.Month(), now.Day(),
		23, 59, 59, 0, loc,
	)
	last := times[len(times)-1]
	if last.After(midnight) {
		warning = fmt.Sprintf("Последний приём выходит за полночь (%s)", last.Format("15:04"))
	}

	return times, warning
}
