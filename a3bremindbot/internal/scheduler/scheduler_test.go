package scheduler

import (
	"sync"
	"testing"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/domain"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockEngine struct {
	mu            sync.Mutex
	tickCalls     int
	notifications []domain.Notification
	recorded      []recordedCall
}

type recordedCall struct {
	Notification domain.Notification
	MessageID    int
	SentAt       time.Time
}

func (m *mockEngine) Tick(now time.Time) ([]domain.Notification, error) {
	m.mu.Lock()
	m.tickCalls++
	notifs := make([]domain.Notification, len(m.notifications))
	copy(notifs, m.notifications)
	m.mu.Unlock()
	return notifs, nil
}

func (m *mockEngine) RecordSent(notification domain.Notification, messageID int, sentAt time.Time) error {
	m.mu.Lock()
	m.recorded = append(m.recorded, recordedCall{Notification: notification, MessageID: messageID, SentAt: sentAt})
	m.mu.Unlock()
	return nil
}

type mockNotifier struct {
	mu       sync.Mutex
	msgCount int
}

func (m *mockNotifier) Notify(recipientID int64, notification domain.Notification) (int, time.Time, error) {
	m.mu.Lock()
	m.msgCount++
	msgID := m.msgCount
	m.mu.Unlock()
	return msgID, time.Now(), nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestTick_CallsEngineAndNotifier(t *testing.T) {
	engine := &mockEngine{}
	notifier := &mockNotifier{}

	n := domain.Notification{
		InstanceID:     "inst-1",
		ReminderID:     "rem-1",
		Label:          "Test",
		RecipientID:    12345,
		Attempt:        1,
		MaxAttempts:    3,
		Type:           domain.NotificationFirst,
		ReminderRepeat: "daily",
	}
	engine.notifications = append(engine.notifications, n)

	s := New(engine, notifier)
	Tick(s, time.Now())

	assert.Equal(t, 1, engine.tickCalls)
	assert.Len(t, engine.recorded, 1)
	assert.Equal(t, "inst-1", engine.recorded[0].Notification.InstanceID)
	assert.Equal(t, 1, engine.recorded[0].MessageID)
}

func TestTick_MultipleNotifications(t *testing.T) {
	engine := &mockEngine{}
	notifier := &mockNotifier{}

	for i := 0; i < 3; i++ {
		engine.notifications = append(engine.notifications, domain.Notification{
			InstanceID:     "inst-" + string(rune('1'+i)),
			RecipientID:    12345,
			Attempt:        1,
			MaxAttempts:    3,
			Type:           domain.NotificationFirst,
			ReminderRepeat: "daily",
		})
	}

	s := New(engine, notifier)
	Tick(s, time.Now())

	assert.Equal(t, 1, engine.tickCalls)
	assert.Len(t, engine.recorded, 3)
}

func TestTick_NoNotifications(t *testing.T) {
	engine := &mockEngine{}
	notifier := &mockNotifier{}

	s := New(engine, notifier)
	Tick(s, time.Now())

	assert.Equal(t, 1, engine.tickCalls)
	assert.Empty(t, engine.recorded)
}

func TestNew_StartStop(t *testing.T) {
	engine := &mockEngine{}
	notifier := &mockNotifier{}

	s := New(engine, notifier)

	// Should not panic.
	s.Stop()
}

// ---------------------------------------------------------------------------
// Engine interface compliance test
// ---------------------------------------------------------------------------

// Ensure domain.Engine satisfies scheduler.Engine at compile time.
func TestEngineInterface(t *testing.T) {
	// Compile-time check: domain.Engine must implement scheduler.Engine.
	var _ Engine = (*domain.Engine)(nil)
}
