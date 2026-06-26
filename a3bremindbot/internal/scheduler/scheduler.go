package scheduler

import (
	"log/slog"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/domain"
)

// Scheduler runs domain logic at a fixed interval.
// It is a thin coordinator:
//   tick → Engine.Tick() → []Notification → Notifier.Notify() → Engine.RecordSent()
type Scheduler struct {
	engine   Engine
	notifier Notifier
	stopCh   chan struct{}
}

// New creates a new Scheduler.
func New(engine Engine, notifier Notifier) *Scheduler {
	return &Scheduler{
		engine:   engine,
		notifier: notifier,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the scheduler loop in a goroutine.
func (s *Scheduler) Start() {
	go func() {
		ticker := time.NewTicker(domain.SchedulerInterval)
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

// tick runs one iteration: engine → notifier → record.
func (s *Scheduler) tick(now time.Time) {
	notifications, err := s.engine.Tick(now)
	if err != nil {
		slog.Error("engine tick", "error", err)
		return
	}

	for _, n := range notifications {
		messageID, sentAt, err := s.notifier.Notify(n.RecipientID, n)
		if err != nil {
			slog.Error("send notification", "recipient_id", n.RecipientID, "instance_id", n.InstanceID, "error", err)
			continue
		}

		if err := s.engine.RecordSent(n, messageID, sentAt); err != nil {
			slog.Error("record sent notification", "instance_id", n.InstanceID, "error", err)
			continue
		}
	}
}
