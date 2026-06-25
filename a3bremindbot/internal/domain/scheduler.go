package domain

import (
	"database/sql"
	"time"
)

// Scheduler runs the domain logic loop at a fixed interval.
type Scheduler struct {
	db       *sql.DB
	notifier Notifier
	stopCh   chan struct{}
}

// New creates a new Scheduler.
func New(db *sql.DB, notifier Notifier) *Scheduler {
	return &Scheduler{
		db:       db,
		notifier: notifier,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the scheduler loop in a goroutine.
func (s *Scheduler) Start() {
	go func() {
		ticker := time.NewTicker(SchedulerInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.tick(time.Now())
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop signals the scheduler loop to exit.
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

// tick runs one iteration of the domain logic.
func (s *Scheduler) tick(now time.Time) {
	s.processPending(now)
	s.checkDailyReset(now)
}

// processPending is implemented in pending.go.
// checkDailyReset is implemented in dailyreset.go.
