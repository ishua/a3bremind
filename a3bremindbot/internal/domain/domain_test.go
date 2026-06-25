package domain

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock Notifier
// ---------------------------------------------------------------------------

type mockNotifier struct {
	calls []mockCall
	mu    sync.Mutex
}

type mockCall struct {
	TelegramID int64
	Text       string
}

func (m *mockNotifier) SendMessage(telegramID int64, text string) (int, time.Time, error) {
	m.mu.Lock()
	m.calls = append(m.calls, mockCall{TelegramID: telegramID, Text: text})
	m.mu.Unlock()
	return len(m.calls), time.Now(), nil
}

func (m *mockNotifier) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.InitDB("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func createTestUser(t *testing.T, db *sql.DB, telegramID int64, timezone string) store.User {
	t.Helper()
	u, err := store.GetOrCreate(db, telegramID)
	require.NoError(t, err)
	if timezone != "" {
		err = store.SetTimezone(db, u.ID, timezone)
		require.NoError(t, err)
		u.Timezone = timezone
	}
	return u
}

func createTestReminder(t *testing.T, db *sql.DB, userID, label string, times []string, repeat string) store.Reminder {
	t.Helper()
	r, err := store.Create(db, store.Reminder{
		UserID: userID,
		Label:  label,
		Times:  times,
		Repeat: repeat,
	})
	require.NoError(t, err)
	return r
}

func resetGlobals() {
	SchedulerInterval = 1 * time.Second
	RepeatInterval = 15 * time.Minute
	RepeatCount = 3
	ResetHour = 3
}

// ---------------------------------------------------------------------------
// ProcessPending tests
// ---------------------------------------------------------------------------

func TestProcessPending_FirstNotification(t *testing.T) {
	resetGlobals()
	db, notifier, s := setup(t)

	u := createTestUser(t, db, 100, "UTC")
	r := createTestReminder(t, db, u.ID, "Test reminder", []string{"09:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	Tick(s, now)

	// Should have sent one notification.
	assert.Equal(t, 1, notifier.Len())
	assert.Equal(t, int64(100), notifier.calls[0].TelegramID)
	assert.Contains(t, notifier.calls[0].Text, "⏰")
	assert.Contains(t, notifier.calls[0].Text, "Test reminder")

	// Should have recorded the message ID.
	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Len(t, got.MessageIDs, 1)
	assert.Equal(t, "pending", got.Status)
}

func TestProcessPending_Repeat(t *testing.T) {
	resetGlobals()
	RepeatInterval = 1 * time.Millisecond
	defer resetGlobals()

	db, notifier, s := setup(t)


	u := createTestUser(t, db, 101, "UTC")
	r := createTestReminder(t, db, u.ID, "Repeat test", []string{"09:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	// Add one existing message entry far in the past.
	oldSentAt := now.Add(-2 * RepeatInterval)
	err = store.AddMessageID(db, inst.ID, 1, oldSentAt)
	require.NoError(t, err)

	Tick(s, now)

	// Should have sent a repeat notification.
	assert.Equal(t, 1, notifier.Len())
	assert.Equal(t, int64(101), notifier.calls[0].TelegramID)
	assert.Contains(t, notifier.calls[0].Text, "🔔")
	assert.Contains(t, notifier.calls[0].Text, "Repeat test")
	assert.Contains(t, notifier.calls[0].Text, "(попытка 2/3)")

	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Len(t, got.MessageIDs, 2)
	assert.Equal(t, "pending", got.Status)
}

func TestProcessPending_RepeatTooEarly(t *testing.T) {
	resetGlobals()
	db, notifier, s := setup(t)


	u := createTestUser(t, db, 102, "UTC")
	r := createTestReminder(t, db, u.ID, "Early test", []string{"09:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	// Add one existing message entry sent just now.
	err = store.AddMessageID(db, inst.ID, 1, now)
	require.NoError(t, err)

	Tick(s, now)

	// No new notification — too early for repeat.
	assert.Equal(t, 0, notifier.Len())
}

func TestProcessPending_Missed(t *testing.T) {
	resetGlobals()
	RepeatCount = 2
	RepeatInterval = 1 * time.Millisecond
	defer resetGlobals()

	db, notifier, s := setup(t)


	u := createTestUser(t, db, 103, "UTC")
	r := createTestReminder(t, db, u.ID, "Missed test", []string{"09:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	// Add RepeatCount-1 entries (1 entry since RepeatCount=2).
	oldSentAt := now.Add(-2 * RepeatInterval)
	err = store.AddMessageID(db, inst.ID, 1, oldSentAt)
	require.NoError(t, err)

	Tick(s, now)

	// Should have sent the last notification.
	assert.Equal(t, 1, notifier.Len())
	assert.Contains(t, notifier.calls[0].Text, "🔔")
	assert.Contains(t, notifier.calls[0].Text, "(попытка 2/2)")

	// Should now be missed.
	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "missed", got.Status)
	assert.Len(t, got.MessageIDs, 2)
}

func TestProcessPending_OnceMissedDeleted(t *testing.T) {
	resetGlobals()
	RepeatCount = 2
	RepeatInterval = 1 * time.Millisecond
	defer resetGlobals()

	db, notifier, s := setup(t)

	u := createTestUser(t, db, 104, "UTC")
	r := createTestReminder(t, db, u.ID, "Once missed", []string{"09:00"}, "once")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	// Add RepeatCount-1 entries (1 entry since RepeatCount=2).
	oldSentAt := now.Add(-2 * RepeatInterval)
	err = store.AddMessageID(db, inst.ID, 1, oldSentAt)
	require.NoError(t, err)

	Tick(s, now)

	// Should have sent the last notification.
	assert.Equal(t, 1, notifier.Len())
	assert.Contains(t, notifier.calls[0].Text, "🔔")
	assert.Contains(t, notifier.calls[0].Text, "(попытка 2/2)")

	// Instance should no longer exist (deleted with the once reminder).
	_, err = store.GetInstanceByID(db, inst.ID)
	assert.ErrorContains(t, err, "not found")

	// Reminder should also be deleted.
	_, err = store.GetByID(db, r.ID)
	assert.ErrorContains(t, err, "not found")

	// No active instances for user.
	active, err := store.GetActiveByUser(db, u.ID)
	require.NoError(t, err)
	assert.Empty(t, active)
}

func setup(t *testing.T) (*sql.DB, *mockNotifier, *Scheduler) {
	t.Helper()
	db := newTestDB(t)
	notifier := &mockNotifier{}
	s := New(db, notifier)
	t.Cleanup(s.Stop)
	return db, notifier, s
}

// ---------------------------------------------------------------------------
// DailyReset tests
// ---------------------------------------------------------------------------

func TestDailyReset(t *testing.T) {
	resetGlobals()
	db, _, _ := setup(t)

	u := createTestUser(t, db, 200, "Europe/Moscow")
	r := createTestReminder(t, db, u.ID, "Morning", []string{"08:00"}, "daily")

	// Pick a time at reset hour.
	moscow, _ := time.LoadLocation("Europe/Moscow")
	now := time.Date(2026, 6, 25, 3, 0, 0, 0, moscow)

	err := DailyReset(db, u.ID, now)
	require.NoError(t, err)

	// Should have created an instance at 08:00 Moscow time.
	pending, err := store.GetPending(db, now.Add(10*time.Hour))
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, r.ID, pending[0].ReminderID)
	assert.Equal(t, 0, pending[0].TimeIndex)
	assert.Equal(t, "pending", pending[0].Status)

	expectedScheduled := time.Date(2026, 6, 25, 8, 0, 0, 0, moscow)
	assert.Equal(t, expectedScheduled.Unix(), pending[0].ScheduledAt.Unix())

	// last_reset_at should be updated.
	userUpdated, err := store.GetUserByID(db, u.ID)
	require.NoError(t, err)
	require.NotNil(t, userUpdated.LastResetAt)
	assert.Equal(t, now.Unix(), userUpdated.LastResetAt.Unix())
}

func TestDailyReset_SkipNotResetHour(t *testing.T) {
	resetGlobals()
	oldResetHour := ResetHour
	ResetHour = 3
	defer func() { ResetHour = oldResetHour }()

	db, notifier, s := setup(t)


	u := createTestUser(t, db, 201, "Europe/Moscow")
	_ = createTestReminder(t, db, u.ID, "Morning", []string{"08:00"}, "daily")

	moscow, _ := time.LoadLocation("Europe/Moscow")
	// Hour is 4, not 3 — should skip.
	now := time.Date(2026, 6, 25, 4, 0, 0, 0, moscow)

	Tick(s, now)

	// No instance should be created.
	pending, err := store.GetPending(db, now.Add(10*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, pending)

	// No notification sent.
	assert.Equal(t, 0, notifier.Len())
}

func TestDailyReset_SkipOnceReminder(t *testing.T) {
	db, _, _ := setup(t)

	u := createTestUser(t, db, 202, "UTC")
	_ = createTestReminder(t, db, u.ID, "Once reminder", []string{"09:00"}, "once")

	now := time.Now().Truncate(time.Second)

	err := DailyReset(db, u.ID, now)
	require.NoError(t, err)

	// No instance should be created for "once" reminder.
	pending, err := store.GetPending(db, now.Add(10*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestDailyReset_SkipAlreadyReset(t *testing.T) {
	resetGlobals()
	oldResetHour := ResetHour
	ResetHour = 3
	defer func() { ResetHour = oldResetHour }()

	db, _, s := setup(t)


	u := createTestUser(t, db, 203, "Europe/Moscow")
	_ = createTestReminder(t, db, u.ID, "Morning", []string{"08:00"}, "daily")

	moscow, _ := time.LoadLocation("Europe/Moscow")
	resetTime := time.Date(2026, 6, 25, 3, 0, 0, 0, moscow)

	// First tick at reset hour — should create instance.
	Tick(s, resetTime)

	// Second tick at same minute — should NOT create another instance.
	Tick(s, resetTime)

	// Only one instance should exist.
	pending, err := store.GetPending(db, resetTime.Add(10*time.Hour))
	require.NoError(t, err)
	assert.Len(t, pending, 1)
}

// ---------------------------------------------------------------------------
// NextInstance tests
// ---------------------------------------------------------------------------

func TestNextInstance_NextInChain(t *testing.T) {
	resetGlobals()
	db, _, _ := setup(t)

	u := createTestUser(t, db, 300, "UTC")
	r := createTestReminder(t, db, u.ID, "Chain test", []string{"09:00", "12:00", "17:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "done",
	})
	require.NoError(t, err)

	// Create next instance.
	_, err = NextInstance(db, inst)
	require.NoError(t, err)

	// Should have created an instance with time_index=1.
	instances, err := store.GetActiveByUser(db, u.ID)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, 1, instances[0].TimeIndex)
	assert.Equal(t, r.ID, instances[0].ReminderID)
	assert.Equal(t, "pending", instances[0].Status)
}

func TestNextInstance_LastIndex(t *testing.T) {
	resetGlobals()
	db, _, _ := setup(t)

	u := createTestUser(t, db, 301, "UTC")
	r := createTestReminder(t, db, u.ID, "Last test", []string{"09:00", "12:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	// Last index = 1 (len(Times)-1 = 1).
	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   1,
		ScheduledAt: past,
		Status:      "done",
	})
	require.NoError(t, err)

	_, err = NextInstance(db, inst)
	require.NoError(t, err)

	// No new instance should be created.
	instances, err := store.GetActiveByUser(db, u.ID)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

// ---------------------------------------------------------------------------
// Reschedule tests
// ---------------------------------------------------------------------------

func TestReschedule_ShiftsForward(t *testing.T) {
	// done at 09:00, min_gap=3h, original times 07:00/11:00/15:00, fromIndex=0
	// Remaining: [11:00, 15:00]
	// Earliest for 11:00 = 09:00 + 3h = 12:00 > 11:00 → shift to 12:00
	// Earliest for 15:00 = 12:00 + 3h = 15:00 == 15:00 → no shift
	// Expected: [12:00, 15:00]
	loc := time.UTC
	minGap := 180
	doneAt := time.Date(2026, 6, 25, 9, 0, 0, 0, loc)

	reminder := store.Reminder{
		Times:  []string{"07:00", "11:00", "15:00"},
		MinGap: &minGap,
	}

	adjusted, warning := Reschedule(reminder, doneAt, 0, loc)
	assert.Empty(t, warning)
	require.Len(t, adjusted, 2)

	assert.Equal(t, "12:00", adjusted[0].In(loc).Format("15:04"), "expected 12:00, got %s", adjusted[0].Format("15:04"))
	assert.Equal(t, "15:00", adjusted[1].In(loc).Format("15:04"), "expected 15:00, got %s", adjusted[1].Format("15:04"))
}

func TestReschedule_NoShiftNeeded(t *testing.T) {
	// done at 06:00, min_gap=2h, original times 09:00/12:00, fromIndex=0
	// Earliest for 09:00 = 06:00 + 2h = 08:00 < 09:00 → no shift
	// Earliest for 12:00 = 09:00 + 2h = 11:00 < 12:00 → no shift
	loc := time.UTC
	minGap := 120
	doneAt := time.Date(2026, 6, 25, 6, 0, 0, 0, loc)

	reminder := store.Reminder{
		Times:  []string{"07:00", "09:00", "12:00"},
		MinGap: &minGap,
	}

	adjusted, warning := Reschedule(reminder, doneAt, 0, loc)
	assert.Empty(t, warning)
	require.Len(t, adjusted, 2)

	assert.Equal(t, "09:00", adjusted[0].In(loc).Format("15:04"))
	assert.Equal(t, "12:00", adjusted[1].In(loc).Format("15:04"))
}

func TestReschedule_NilMinGap(t *testing.T) {
	loc := time.UTC

	reminder := store.Reminder{
		Times:  []string{"07:00", "09:00", "12:00"},
		MinGap: nil,
	}

	doneAt := time.Date(2026, 6, 25, 6, 0, 0, 0, loc)
	adjusted, warning := Reschedule(reminder, doneAt, 0, loc)
	assert.Empty(t, warning)
	require.Len(t, adjusted, 2)

	// Should return the original times as time.Time for today (date is time.Now(), only time matters)
	assert.Equal(t, "09:00", adjusted[0].In(loc).Format("15:04"))
	assert.Equal(t, "12:00", adjusted[1].In(loc).Format("15:04"))
}

func TestReschedule_LastPastMidnight(t *testing.T) {
	// done at 22:00, min_gap=3h, original times 07:00/11:00/23:00
	// Remaining after index 1: [23:00]
	// Earliest for 23:00 = 22:00 + 3h = 01:00 next day > 23:00
	// adjusted = 01:00 next day → warning
	loc := time.UTC
	minGap := 180
	doneAt := time.Date(2026, 6, 25, 22, 0, 0, 0, loc)

	reminder := store.Reminder{
		Times:  []string{"07:00", "11:00", "23:00"},
		MinGap: &minGap,
	}

	adjusted, warning := Reschedule(reminder, doneAt, 1, loc)
	require.Len(t, adjusted, 1)
	assert.NotEmpty(t, warning)
	assert.Contains(t, warning, "полночь")

	// Adjusted time should be 22:00 + 3h = 01:00
	assert.Equal(t, "01:00", adjusted[0].In(loc).Format("15:04"))
}

func TestNextInstance_WithReschedule(t *testing.T) {
	resetGlobals()
	db, _, _ := setup(t)

	u := createTestUser(t, db, 500, "UTC")
	minGap := 180
	r, err := store.Create(db, store.Reminder{
		UserID: u.ID,
		Label:  "Reschedule test",
		Times:  []string{"07:00", "11:00", "15:00"},
		Repeat: "daily",
		MinGap: &minGap,
	})
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	// Create a done instance at time_index=0
	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "done",
		DoneAt:      &now, // done just now
	})
	require.NoError(t, err)

	// Call NextInstance — should create time_index=1 with adjusted time
	_, err = NextInstance(db, inst)
	require.NoError(t, err)

	// Should have created an instance with time_index=1
	instances, err := store.GetActiveByUser(db, u.ID)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, 1, instances[0].TimeIndex)

	// The rescheduled time should be >= now + 3h
	minExpected := now.Add(3 * time.Hour)
	assert.True(t, instances[0].ScheduledAt.Unix() >= minExpected.Unix(),
		"expected scheduled_at >= %s, got %s", minExpected.Format("15:04"), instances[0].ScheduledAt.Format("15:04"))
}

func TestNextInstance_RescheduleWarning(t *testing.T) {
	resetGlobals()
	db, _, _ := setup(t)

	u := createTestUser(t, db, 501, "UTC")
	minGap := 180
	r, err := store.Create(db, store.Reminder{
		UserID: u.ID,
		Label:  "Warning test",
		Times:  []string{"07:00", "23:00"},
		Repeat: "daily",
		MinGap: &minGap,
	})
	require.NoError(t, err)

	// done at 22:00, remaining 23:00 → shifted to 01:00 next day
	now := time.Date(2026, 6, 25, 22, 0, 0, 0, time.UTC)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: now.Add(-1 * time.Hour),
		Status:      "done",
		DoneAt:      &now,
	})
	require.NoError(t, err)

	warning, err := NextInstance(db, inst)
	require.NoError(t, err)
	assert.NotEmpty(t, warning)
	assert.Contains(t, warning, "полночь")
}

// ---------------------------------------------------------------------------
// Integration test
// ---------------------------------------------------------------------------

func TestIntegration_ProcessPending_NextInstance(t *testing.T) {
	resetGlobals()
	db, notifier, s := setup(t)


	u := createTestUser(t, db, 400, "UTC")
	r := createTestReminder(t, db, u.ID, "Integration", []string{"09:00", "12:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	// Tick — should send first notification.
	Tick(s, now)
	assert.Equal(t, 1, notifier.Len())
	assert.Contains(t, notifier.calls[0].Text, "⏰")

	// Manually set done.
	err = store.SetStatus(db, inst.ID, "done")
	require.NoError(t, err)

	// Create next instance.
	_, err = NextInstance(db, inst)
	require.NoError(t, err)

	// Next instance should be at time_index=1.
	instances, err := store.GetActiveByUser(db, u.ID)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, 1, instances[0].TimeIndex)
}
