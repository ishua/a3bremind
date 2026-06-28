package domain

import (
	"database/sql"
	"testing"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.InitDB("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() }) //nolint:errcheck // test cleanup
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

func newEngine(t *testing.T, db *sql.DB) *Engine {
	t.Helper()
	return NewEngine(db)
}

// ---------------------------------------------------------------------------
// processInstance tests
// ---------------------------------------------------------------------------

func TestProcessInstance_FirstNotification(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)
	e := newEngine(t, db)

	u := createTestUser(t, db, 100, "UTC")
	r := createTestReminder(t, db, u.ID, "Test reminder", []string{"09:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	n := e.processInstance(inst, now)
	require.NotNil(t, n)
	assert.Equal(t, NotificationFirst, n.Type)
	assert.Equal(t, "Test reminder", n.Label)
	assert.Equal(t, inst.ID, n.InstanceID)
	assert.Equal(t, int64(100), n.RecipientID)
	assert.Equal(t, 1, n.Attempt)
	assert.Equal(t, 3, n.MaxAttempts)
	assert.Equal(t, "daily", n.ReminderRepeat)
}

func TestProcessInstance_Repeat(t *testing.T) {
	resetGlobals()
	RepeatInterval = 1 * time.Millisecond
	defer resetGlobals()

	db := newTestDB(t)
	e := newEngine(t, db)

	u := createTestUser(t, db, 101, "UTC")
	r := createTestReminder(t, db, u.ID, "Repeat test", []string{"09:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	oldSentAt := now.Add(-2 * RepeatInterval)
	err = store.AddMessageID(db, inst.ID, 1, oldSentAt)
	require.NoError(t, err)

	// Re-read instance to get updated MessageIDs.
	inst, err = store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)

	n := e.processInstance(inst, now)
	require.NotNil(t, n)
	assert.Equal(t, NotificationRepeat, n.Type)
	assert.Equal(t, "Repeat test", n.Label)
	assert.Equal(t, 2, n.Attempt)
	assert.Equal(t, 3, n.MaxAttempts)
}

func TestProcessInstance_RepeatTooEarly(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)
	e := newEngine(t, db)

	u := createTestUser(t, db, 102, "UTC")
	r := createTestReminder(t, db, u.ID, "Early test", []string{"09:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 1, now)
	require.NoError(t, err)

	// Re-read instance to get updated MessageIDs.
	inst, err = store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)

	n := e.processInstance(inst, now)
	assert.Nil(t, n, "too early for repeat, should return nil")
}

func TestProcessInstance_Paused(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)
	e := newEngine(t, db)

	u := createTestUser(t, db, 103, "UTC")
	_ = store.SetPaused(db, u.ID, true)

	r := createTestReminder(t, db, u.ID, "Paused test", []string{"09:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	n := e.processInstance(inst, now)
	assert.Nil(t, n, "paused user should not get notifications")
}

// ---------------------------------------------------------------------------
// processPending tests
// ---------------------------------------------------------------------------

func TestProcessPending_MultipleInstances(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)
	e := newEngine(t, db)

	u := createTestUser(t, db, 200, "UTC")
	r := createTestReminder(t, db, u.ID, "Test", []string{"09:00", "12:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	// Two pending instances at different time indices.
	_, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	_, err = store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   1,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	notifications := e.processPending(now)
	assert.Len(t, notifications, 2)
}

// ---------------------------------------------------------------------------
// RecordSent tests
// ---------------------------------------------------------------------------

func TestRecordSent_FirstNotification(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)
	e := newEngine(t, db)

	u := createTestUser(t, db, 300, "UTC")
	r := createTestReminder(t, db, u.ID, "Test", []string{"09:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	n := Notification{
		InstanceID:     inst.ID,
		ReminderID:     r.ID,
		Label:          "Test",
		ScheduledAt:    past,
		Attempt:        1,
		MaxAttempts:    3,
		Type:           NotificationFirst,
		UserID:         u.ID,
		RecipientID:    u.TelegramID,
		Timezone:       "UTC",
		ReminderRepeat: "daily",
	}

	err = e.RecordSent(n, 42, now)
	require.NoError(t, err)

	// Should have recorded message ID.
	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Len(t, got.MessageIDs, 1)
	assert.Equal(t, "pending", got.Status)

	// Should have reply mapping.
	instanceID, err := store.GetInstanceIDByReply(db, 42)
	require.NoError(t, err)
	assert.Equal(t, inst.ID, instanceID)
}

func TestRecordSent_LastAttemptMarksMissed(t *testing.T) {
	resetGlobals()
	RepeatCount = 2
	defer resetGlobals()

	db := newTestDB(t)
	e := newEngine(t, db)

	u := createTestUser(t, db, 301, "UTC")
	r := createTestReminder(t, db, u.ID, "Missed test", []string{"09:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	// Add first message — this is attempt 1.
	err = store.AddMessageID(db, inst.ID, 1, now)
	require.NoError(t, err)

	// Record sent for attempt 2 (last).
	n := Notification{
		InstanceID:     inst.ID,
		ReminderID:     r.ID,
		Label:          "Missed test",
		ScheduledAt:    past,
		Attempt:        2,
		MaxAttempts:    2,
		Type:           NotificationRepeat,
		UserID:         u.ID,
		RecipientID:    u.TelegramID,
		Timezone:       "UTC",
		ReminderRepeat: "daily",
	}

	err = e.RecordSent(n, 42, now)
	require.NoError(t, err)

	// Should now be missed.
	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "missed", got.Status)
}

func TestRecordSent_OnceReminderNotDeletedAnymore(t *testing.T) {
	resetGlobals()
	RepeatCount = 2
	defer resetGlobals()

	db := newTestDB(t)
	e := newEngine(t, db)

	u := createTestUser(t, db, 302, "UTC")
	r := createTestReminder(t, db, u.ID, "Once test", []string{"09:00"}, "once")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 1, now)
	require.NoError(t, err)

	n := Notification{
		InstanceID:     inst.ID,
		ReminderID:     r.ID,
		Label:          "Once test",
		ScheduledAt:    past,
		Attempt:        2,
		MaxAttempts:    2,
		Type:           NotificationRepeat,
		UserID:         u.ID,
		RecipientID:    u.TelegramID,
		Timezone:       "UTC",
		ReminderRepeat: "once",
	}

	err = e.RecordSent(n, 42, now)
	require.NoError(t, err)

	// Instance should still exist but be "missed" (no longer deleted on miss).
	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "missed", got.Status)

	// Reminder should still exist (no longer deleted on miss).
	reminder, err := store.GetByID(db, r.ID)
	require.NoError(t, err)
	assert.Equal(t, r.ID, reminder.ID)
}

// ---------------------------------------------------------------------------
// DailyReset tests
// ---------------------------------------------------------------------------

func TestDailyReset(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)
	e := newEngine(t, db)

	u := createTestUser(t, db, 200, "Europe/Moscow")
	r := createTestReminder(t, db, u.ID, "Morning", []string{"08:00"}, "daily")

	moscow, _ := time.LoadLocation("Europe/Moscow")
	now := time.Date(2026, 6, 25, 3, 0, 0, 0, moscow)

	err := e.DailyReset(u.ID, now)
	require.NoError(t, err)

	pending, err := store.GetPending(db, now.Add(10*time.Hour))
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, r.ID, pending[0].ReminderID)
	assert.Equal(t, 0, pending[0].TimeIndex)

	expectedScheduled := time.Date(2026, 6, 25, 8, 0, 0, 0, moscow)
	assert.Equal(t, expectedScheduled.Unix(), pending[0].ScheduledAt.Unix())

	userUpdated, err := store.GetUserByID(db, u.ID)
	require.NoError(t, err)
	require.NotNil(t, userUpdated.LastResetAt)
	assert.Equal(t, now.Unix(), userUpdated.LastResetAt.Unix())
}

func TestDailyReset_SkipOnceReminder(t *testing.T) {
	db := newTestDB(t)
	e := newEngine(t, db)

	u := createTestUser(t, db, 202, "UTC")
	_ = createTestReminder(t, db, u.ID, "Once reminder", []string{"09:00"}, "once")

	now := time.Now().Truncate(time.Second)

	err := e.DailyReset(u.ID, now)
	require.NoError(t, err)

	pending, err := store.GetPending(db, now.Add(10*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, pending)
}

// ---------------------------------------------------------------------------
// NextInstance tests
// ---------------------------------------------------------------------------

func TestNextInstance_NextInChain(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)

	u := createTestUser(t, db, 300, "UTC")
	r := createTestReminder(t, db, u.ID, "Chain test", []string{"09:00", "12:00", "17:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "done",
	})
	require.NoError(t, err)

	_, err = NextInstance(db, inst, now)
	require.NoError(t, err)

	instances, err := store.GetReminderInstancesByReminder(db, r.ID)
	require.NoError(t, err)
	var pending []store.ReminderInstance
	for _, i := range instances {
		if i.Status == "pending" {
			pending = append(pending, i)
		}
	}
	require.Len(t, pending, 1)
	assert.Equal(t, 1, pending[0].TimeIndex)
	assert.Equal(t, r.ID, pending[0].ReminderID)
	assert.Equal(t, "pending", pending[0].Status)
}

func TestNextInstance_LastIndex(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)

	u := createTestUser(t, db, 301, "UTC")
	r := createTestReminder(t, db, u.ID, "Last test", []string{"09:00", "12:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   1,
		ScheduledAt: past,
		Status:      "done",
	})
	require.NoError(t, err)

	_, err = NextInstance(db, inst, now)
	require.NoError(t, err)

	instances, err := store.GetReminderInstancesByReminder(db, r.ID)
	require.NoError(t, err)
	var pending []store.ReminderInstance
	for _, i := range instances {
		if i.Status == "pending" {
			pending = append(pending, i)
		}
	}
	assert.Empty(t, pending)
}

// ---------------------------------------------------------------------------
// Reschedule tests
// ---------------------------------------------------------------------------

func TestReschedule_ShiftsForward(t *testing.T) {
	loc := time.UTC
	minGap := 180
	doneAt := time.Date(2026, 6, 25, 9, 0, 0, 0, loc)

	reminder := store.Reminder{
		Times:  []string{"07:00", "11:00", "15:00"},
		MinGap: &minGap,
	}

	now := time.Date(2026, 6, 25, 9, 0, 0, 0, loc)
	adjusted, warning := Reschedule(reminder, doneAt, 0, loc, now)
	assert.Empty(t, warning)
	require.Len(t, adjusted, 2)
	assert.Equal(t, "12:00", adjusted[0].In(loc).Format("15:04"))
	assert.Equal(t, "15:00", adjusted[1].In(loc).Format("15:04"))
}

func TestReschedule_NoShiftNeeded(t *testing.T) {
	loc := time.UTC
	minGap := 120
	doneAt := time.Date(2026, 6, 25, 6, 0, 0, 0, loc)

	reminder := store.Reminder{
		Times:  []string{"07:00", "09:00", "12:00"},
		MinGap: &minGap,
	}

	adjusted, warning := Reschedule(reminder, doneAt, 0, loc, doneAt)
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
	adjusted, warning := Reschedule(reminder, doneAt, 0, loc, doneAt)
	assert.Empty(t, warning)
	require.Len(t, adjusted, 2)
	assert.Equal(t, "09:00", adjusted[0].In(loc).Format("15:04"))
	assert.Equal(t, "12:00", adjusted[1].In(loc).Format("15:04"))
}

func TestReschedule_LastPastMidnight(t *testing.T) {
	loc := time.UTC
	minGap := 180
	doneAt := time.Date(2026, 6, 25, 22, 0, 0, 0, loc)

	reminder := store.Reminder{
		Times:  []string{"07:00", "11:00", "23:00"},
		MinGap: &minGap,
	}

	adjusted, warning := Reschedule(reminder, doneAt, 1, loc, doneAt)
	require.Len(t, adjusted, 1)
	assert.NotEmpty(t, warning)
	assert.Contains(t, warning, "полночь")
	assert.Equal(t, "01:00", adjusted[0].In(loc).Format("15:04"))
}

func TestNextInstance_WithReschedule(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)

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

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "done",
		DoneAt:      &now,
	})
	require.NoError(t, err)

	_, err = NextInstance(db, inst, now)
	require.NoError(t, err)

	instances, err := store.GetReminderInstancesByReminder(db, r.ID)
	require.NoError(t, err)
	var pending []store.ReminderInstance
	for _, i := range instances {
		if i.Status == "pending" {
			pending = append(pending, i)
		}
	}
	require.Len(t, pending, 1)
	assert.Equal(t, 1, pending[0].TimeIndex)

	minExpected := now.Add(3 * time.Hour)
	assert.True(t, pending[0].ScheduledAt.Unix() >= minExpected.Unix())
}

func TestNextInstance_RescheduleWarning(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)

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

	now := time.Date(2026, 6, 25, 22, 0, 0, 0, time.UTC)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: now.Add(-1 * time.Hour),
		Status:      "done",
		DoneAt:      &now,
	})
	require.NoError(t, err)

	warning, err := NextInstance(db, inst, now)
	require.NoError(t, err)
	assert.NotEmpty(t, warning)
	assert.Contains(t, warning, "полночь")
}

func TestNextInstance_ForDateUnchanged(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)

	u := createTestUser(t, db, 502, "UTC")
	r, err := store.Create(db, store.Reminder{
		UserID: u.ID,
		Label:  "ForDate test",
		Times:  []string{"09:00", "17:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	originalForDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     originalForDate,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "done",
		DoneAt:      &now,
	})
	require.NoError(t, err)

	_, err = NextInstance(db, inst, now)
	require.NoError(t, err)

	instances, err := store.GetReminderInstancesByReminder(db, r.ID)
	require.NoError(t, err)
	var pending []store.ReminderInstance
	for _, i := range instances {
		if i.Status == "pending" {
			pending = append(pending, i)
		}
	}
	require.Len(t, pending, 1)
	assert.Equal(t, 1, pending[0].TimeIndex)
	assert.Equal(t, originalForDate.Unix(), pending[0].ForDate.Unix())
}

// ---------------------------------------------------------------------------
// Race condition tests
// ---------------------------------------------------------------------------

func TestSetStatus_RaceGuard(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)

	u := createTestUser(t, db, 105, "UTC")
	r := createTestReminder(t, db, u.ID, "Race test", []string{"09:00"}, "daily")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	// Simulate done handler running first.
	err = store.SetStatus(db, inst.ID, "done")
	require.NoError(t, err)

	// Now try to set missed — guard (AND status = 'pending') should prevent overwrite.
	err = store.SetStatus(db, inst.ID, "missed")
	require.NoError(t, err)

	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)
}

func TestMarkMissedAndDeleteOnce_AlreadyDone(t *testing.T) {
	resetGlobals()
	db := newTestDB(t)

	u := createTestUser(t, db, 108, "UTC")
	r := createTestReminder(t, db, u.ID, "Tx already done", []string{"09:00"}, "once")

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: past,
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.SetStatus(db, inst.ID, "done")
	require.NoError(t, err)

	err = store.MarkMissedAndDeleteOnce(db, inst.ID, r.ID)
	require.NoError(t, err)

	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)

	_, err = store.GetByID(db, r.ID)
	require.NoError(t, err)
}
