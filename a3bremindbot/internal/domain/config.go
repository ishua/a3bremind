package domain

import "time"

// Domain configuration variables.
// These are variables (not constants) so tests can override them.
var (
	// SchedulerInterval is how often the scheduler ticks.
	SchedulerInterval = 1 * time.Second

	// RepeatInterval is the minimum gap between repeat notifications.
	RepeatInterval = 15 * time.Minute

	// RepeatCount is the number of repeat attempts before marking as missed.
	RepeatCount = 3

	// ResetHour is the hour (in user's timezone) when DailyReset triggers (default 03:00).
	ResetHour = 3
)
