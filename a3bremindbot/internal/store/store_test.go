package store

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestDB opens an in-memory SQLite database and runs migration.
// It sets MaxOpenConns(1) so that all operations use the same connection,
// which is required for :memory: databases where each connection has its own DB.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := InitDB("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() }) //nolint:errcheck // test cleanup
	return db
}

// ---------------------------------------------------------------------------
// User tests
// ---------------------------------------------------------------------------

func TestGetOrCreate(t *testing.T) {
	db := newTestDB(t)

	// Create a new user.
	u, err := GetOrCreate(db, 100500)
	require.NoError(t, err)
	assert.Equal(t, int64(100500), u.TelegramID)
	assert.Equal(t, "", u.Timezone)
	assert.Equal(t, false, u.Paused)
	assert.Nil(t, u.LastResetAt)
	assert.NotEmpty(t, u.ID)
	assert.False(t, u.CreatedAt.IsZero())

	// GetOrCreate again with the same telegram_id should return the same user.
	u2, err := GetOrCreate(db, 100500)
	require.NoError(t, err)
	assert.Equal(t, u.ID, u2.ID)
	assert.Equal(t, u.TelegramID, u2.TelegramID)
}

func TestGetByTelegramID_NotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := GetByTelegramID(db, 999)
	assert.ErrorContains(t, err, "not found")
}

func TestSetTimezone(t *testing.T) {
	db := newTestDB(t)

	u, err := GetOrCreate(db, 1)
	require.NoError(t, err)

	err = SetTimezone(db, u.ID, "Europe/Moscow")
	require.NoError(t, err)

	got, err := GetByTelegramID(db, 1)
	require.NoError(t, err)
	assert.Equal(t, "Europe/Moscow", got.Timezone)
}

func TestSetTimezone_NotFound(t *testing.T) {
	db := newTestDB(t)
	err := SetTimezone(db, "nonexistent-uuid", "UTC")
	assert.ErrorContains(t, err, "not found")
}

func TestSetPaused(t *testing.T) {
	db := newTestDB(t)

	u, err := GetOrCreate(db, 2)
	require.NoError(t, err)

	err = SetPaused(db, u.ID, true)
	require.NoError(t, err)

	got, err := GetByTelegramID(db, 2)
	require.NoError(t, err)
	assert.True(t, got.Paused)

	// Toggle back.
	err = SetPaused(db, u.ID, false)
	require.NoError(t, err)

	got, err = GetByTelegramID(db, 2)
	require.NoError(t, err)
	assert.False(t, got.Paused)
}

func TestSetLastResetAt(t *testing.T) {
	db := newTestDB(t)

	u, err := GetOrCreate(db, 3)
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)
	err = SetLastResetAt(db, u.ID, now)
	require.NoError(t, err)

	got, err := GetByTelegramID(db, 3)
	require.NoError(t, err)
	require.NotNil(t, got.LastResetAt)
	assert.Equal(t, now.Unix(), got.LastResetAt.Unix())
}

func TestSetLastResetAt_NotFound(t *testing.T) {
	db := newTestDB(t)
	err := SetLastResetAt(db, "nonexistent", time.Now())
	assert.ErrorContains(t, err, "not found")
}

// ---------------------------------------------------------------------------
// Reminder tests
// ---------------------------------------------------------------------------

func TestCreateReminder(t *testing.T) {
	db := newTestDB(t)

	u, err := GetOrCreate(db, 10)
	require.NoError(t, err)

	r := Reminder{
		UserID: u.ID,
		Label:  "Test reminder",
		Times:  []string{"09:00", "17:00"},
		Repeat: "daily",
	}

	created, err := Create(db, r)
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, u.ID, created.UserID)
	assert.Equal(t, "Test reminder", created.Label)
	assert.Equal(t, []string{"09:00", "17:00"}, created.Times)
	assert.Equal(t, "daily", created.Repeat)
	assert.Nil(t, created.MinGap)
	assert.False(t, created.CreatedAt.IsZero())
	assert.False(t, created.UpdatedAt.IsZero())
}

func TestCreateReminder_WithID(t *testing.T) {
	db := newTestDB(t)

	u, err := GetOrCreate(db, 11)
	require.NoError(t, err)

	r := Reminder{
		ID:     "my-custom-id",
		UserID: u.ID,
		Label:  "Custom ID",
		Times:  []string{"12:00"},
		Repeat: "once",
	}

	created, err := Create(db, r)
	require.NoError(t, err)
	assert.Equal(t, "my-custom-id", created.ID)
}

func TestCreateReminder_WithMinGap(t *testing.T) {
	db := newTestDB(t)

	u, err := GetOrCreate(db, 12)
	require.NoError(t, err)

	gap := 60
	r := Reminder{
		UserID: u.ID,
		Label:  "With gap",
		Times:  []string{"10:00", "11:00", "12:00"},
		MinGap: &gap,
		Repeat: "daily",
	}

	created, err := Create(db, r)
	require.NoError(t, err)
	require.NotNil(t, created.MinGap)
	assert.Equal(t, 60, *created.MinGap)
}

func TestGetReminderByID(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 13)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Get me", Times: []string{"08:00"}, Repeat: "daily"})

	got, err := GetByID(db, r.ID)
	require.NoError(t, err)
	assert.Equal(t, r.ID, got.ID)
	assert.Equal(t, "Get me", got.Label)
}

func TestGetReminderByID_NotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := GetByID(db, "nonexistent")
	assert.ErrorContains(t, err, "not found")
}

func TestGetAllReminders(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 14)
	Create(db, Reminder{UserID: u.ID, Label: "A", Times: []string{"09:00"}, Repeat: "daily"}) //nolint:errcheck // test setup
	Create(db, Reminder{UserID: u.ID, Label: "B", Times: []string{"10:00"}, Repeat: "once"})  //nolint:errcheck // test setup
	Create(db, Reminder{UserID: u.ID, Label: "C", Times: []string{"11:00"}, Repeat: "daily"}) //nolint:errcheck // test setup

	all, err := GetAll(db, u.ID)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// Another user gets empty list.
	u2, _ := GetOrCreate(db, 99)
	all2, err := GetAll(db, u2.ID)
	require.NoError(t, err)
	assert.Len(t, all2, 0)
}

func TestUpdateReminder(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 15)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Original", Times: []string{"09:00"}, Repeat: "daily"})

	// Read the created_at to use as baseline.
	gotBefore, err := GetByID(db, r.ID)
	require.NoError(t, err)
	originalUpdatedAt := gotBefore.UpdatedAt

	// Wait a bit so updated_at differs.
	time.Sleep(10 * time.Millisecond)

	r.Label = "Updated"
	r.Times = []string{"10:00", "11:00"}
	r.Repeat = "once"
	gap := 30
	r.MinGap = &gap

	err = Update(db, r)
	require.NoError(t, err)

	got, err := GetByID(db, r.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated", got.Label)
	assert.Equal(t, []string{"10:00", "11:00"}, got.Times)
	assert.Equal(t, "once", got.Repeat)
	require.NotNil(t, got.MinGap)
	assert.Equal(t, 30, *got.MinGap)

	// updated_at should have been bumped (compare Unix timestamps, since we truncate to seconds).
	assert.GreaterOrEqual(t, got.UpdatedAt.Unix(), originalUpdatedAt.Unix(),
		"updated_at should be >= original updated_at")
}

func TestUpdateReminder_NotFound(t *testing.T) {
	db := newTestDB(t)
	err := Update(db, Reminder{ID: "nonexistent", Times: []string{}, Repeat: "daily"})
	assert.ErrorContains(t, err, "not found")
}

func TestDeleteReminder(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 16)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Delete me", Times: []string{"09:00"}, Repeat: "daily"})

	err := Delete(db, r.ID)
	require.NoError(t, err)

	_, err = GetByID(db, r.ID)
	assert.ErrorContains(t, err, "not found")
}

func TestDeleteReminder_NotFound(t *testing.T) {
	db := newTestDB(t)
	err := Delete(db, "nonexistent")
	assert.ErrorContains(t, err, "not found")
}

// ---------------------------------------------------------------------------
// ReminderInstance tests
// ---------------------------------------------------------------------------

func TestCreateReminderInstance(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 20)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Test", Times: []string{"09:00"}, Repeat: "daily"})

	now := time.Now().Truncate(time.Second)
	inst := ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ForDate:     now,
		ScheduledAt: now,
		Status:      "pending",
		MessageIDs:  []MessageIDEntry{},
	}

	created, err := CreateInstance(db, inst)
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, r.ID, created.ReminderID)
	assert.Equal(t, 0, created.TimeIndex)
	assert.Equal(t, now.Unix(), created.ScheduledAt.Unix())
	assert.Equal(t, "pending", created.Status)
	assert.Empty(t, created.MessageIDs)
	assert.Nil(t, created.DoneAt)
}

func TestCreateInstance_MessageIDsInit(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 21)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "MsgIDs init", Times: []string{"09:00"}, Repeat: "daily"})

	inst := ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ForDate:     time.Now(),
		ScheduledAt: time.Now(),
		// MessageIDs is nil — should be initialized to []MessageIDEntry{}.
	}

	created, err := CreateInstance(db, inst)
	require.NoError(t, err)
	assert.NotNil(t, created.MessageIDs)
	assert.Empty(t, created.MessageIDs)

	// Re-read from DB and verify the JSON is "[]" not "null".
	got, err := GetInstanceByID(db, created.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.MessageIDs)
	assert.Empty(t, got.MessageIDs)
}

func TestGetInstanceByID(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 22)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Get instance", Times: []string{"09:00"}, Repeat: "daily"})

	inst, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: time.Now(), TimeIndex: 0, ScheduledAt: time.Now()})

	got, err := GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, inst.ID, got.ID)
	assert.Equal(t, r.ID, got.ReminderID)
}

func TestGetInstanceByID_NotFound(t *testing.T) {
	db := newTestDB(t)
	_, err := GetInstanceByID(db, "nonexistent")
	assert.ErrorContains(t, err, "not found")
}

func TestGetPending(t *testing.T) {
	db := newTestDB(t)

	now := time.Now().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	u, _ := GetOrCreate(db, 23)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Pending test", Times: []string{"09:00"}, Repeat: "daily"})

	// Past — should be pending.
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: past, TimeIndex: 0, ScheduledAt: past, Status: "pending"}) //nolint:errcheck // test setup
	// Future — should NOT be pending yet.
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: future, TimeIndex: 1, ScheduledAt: future, Status: "pending"}) //nolint:errcheck // test setup
	// Past but done — should NOT be selected.
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: past, TimeIndex: 2, ScheduledAt: past, Status: "done"}) //nolint:errcheck // test setup

	pending, err := GetPending(db, now)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	assert.Equal(t, 0, pending[0].TimeIndex)
}

func TestGetLastByReminder(t *testing.T) {
	db := newTestDB(t)

	now := time.Now().Truncate(time.Second)

	u, _ := GetOrCreate(db, 27)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Last test", Times: []string{"09:00", "17:00"}, Repeat: "daily"})

	// TimeIndex 0 — create two instances at different times.
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: now, TimeIndex: 0, ScheduledAt: now.Add(-2 * time.Hour), Status: "done"}) //nolint:errcheck
	latest, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: now, TimeIndex: 0, ScheduledAt: now.Add(-1 * time.Hour), Status: "done"})

	// TimeIndex 1 — one instance.
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: now, TimeIndex: 1, ScheduledAt: now, Status: "pending"}) //nolint:errcheck

	got, err := GetLastByReminder(db, r.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, latest.ID, got.ID)
	assert.Equal(t, now.Add(-1*time.Hour).Unix(), got.ScheduledAt.Unix())

	got2, err := GetLastByReminder(db, r.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "pending", got2.Status)
}

func TestGetLastByReminder_NotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := GetLastByReminder(db, "nonexistent", 0)
	assert.ErrorContains(t, err, "not found")
}

func TestSetStatus(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 28)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Status test", Times: []string{"09:00"}, Repeat: "daily"})
	inst, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: time.Now(), TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"})

	// Read from DB to get the exact stored updated_at.
	gotBefore, err := GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	originalUpdatedAt := gotBefore.UpdatedAt
	time.Sleep(10 * time.Millisecond)

	err = SetStatus(db, inst.ID, "done")
	require.NoError(t, err)

	got, err := GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)
	assert.GreaterOrEqual(t, got.UpdatedAt.Unix(), originalUpdatedAt.Unix(),
		"updated_at should be >= original")
}

func TestSetStatus_NotFound(t *testing.T) {
	db := newTestDB(t)
	err := SetStatus(db, "nonexistent", "done")
	assert.ErrorContains(t, err, "not found")
}

func TestAddMessageID(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 30)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "AddMsg test", Times: []string{"09:00"}, Repeat: "daily"})
	inst, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: time.Now(), TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"})

	// Initial message_ids should be empty.
	got, _ := GetInstanceByID(db, inst.ID)
	assert.Empty(t, got.MessageIDs)

	originalUpdatedAt := got.UpdatedAt
	time.Sleep(10 * time.Millisecond)

	now := time.Now()

	// Add first message.
	err := AddMessageID(db, inst.ID, 100, now)
	require.NoError(t, err)

	got, err = GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, []MessageIDEntry{{MessageID: 100, SentAt: now.Unix()}}, got.MessageIDs)

	// Add second message.
	err = AddMessageID(db, inst.ID, 200, now)
	require.NoError(t, err)

	got, err = GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, []MessageIDEntry{{MessageID: 100, SentAt: now.Unix()}, {MessageID: 200, SentAt: now.Unix()}}, got.MessageIDs)
	assert.GreaterOrEqual(t, got.UpdatedAt.Unix(), originalUpdatedAt.Unix(),
		"updated_at should be >= original")
}

func TestAddMessageID_Concurrent(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 31)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Concurrent test", Times: []string{"09:00"}, Repeat: "daily"})
	inst, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: time.Now(), TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"})

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	now := time.Now()

	for i := 0; i < numGoroutines; i++ {
		msgID := i + 1
		go func() {
			defer wg.Done()
			err := AddMessageID(db, inst.ID, msgID, now)
			assert.NoError(t, err)
		}()
	}

	wg.Wait()

	got, err := GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	// Should have exactly numGoroutines entries.
	assert.Len(t, got.MessageIDs, numGoroutines)

	// Each message ID from 1..numGoroutines should be present.
	seen := make(map[int]bool)
	for _, entry := range got.MessageIDs {
		assert.False(t, seen[entry.MessageID], "duplicate message_id %d", entry.MessageID)
		seen[entry.MessageID] = true
		assert.GreaterOrEqual(t, entry.MessageID, 1)
		assert.LessOrEqual(t, entry.MessageID, numGoroutines)
	}
}

func TestAddMessageID_NotFound(t *testing.T) {
	db := newTestDB(t)
	err := AddMessageID(db, "nonexistent", 1, time.Now())
	assert.ErrorContains(t, err, "not found")
}

// ---------------------------------------------------------------------------
// AddMessageIDAndMarkMissedDeleteOnce tests
// ---------------------------------------------------------------------------

func TestAddMessageIDAndMarkMissedDeleteOnce_NowJustMisses(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 110)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Once atomic", Times: []string{"09:00"}, Repeat: "once"})
	inst, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: time.Now(), TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"})

	now := time.Now()

	// Call the atomic function.
	err := AddMessageIDAndMarkMissedDeleteOnce(db, inst.ID, r.ID, 42, now)
	require.NoError(t, err)

	// Instance should still exist (no longer deleted) but be "missed".
	got, err := GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "missed", got.Status)

	// Reminder should still exist (no longer deleted).
	reminder, err := GetByID(db, r.ID)
	require.NoError(t, err)
	assert.Equal(t, r.ID, reminder.ID)
}

func TestAddMessageIDAndMarkMissedDeleteOnce_AlreadyDone(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 111)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Once atomic already done", Times: []string{"09:00"}, Repeat: "once"})
	inst, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: time.Now(), TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"})

	// Done handler runs first.
	err := SetStatus(db, inst.ID, "done")
	require.NoError(t, err)

	// Now scheduler calls the atomic function — should be a no-op.
	err = AddMessageIDAndMarkMissedDeleteOnce(db, inst.ID, r.ID, 42, time.Now())
	require.NoError(t, err)

	// Instance should still exist with "done".
	got, err := GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)

	// Reminder should still exist.
	reminder, err := GetByID(db, r.ID)
	require.NoError(t, err)
	assert.Equal(t, r.ID, reminder.ID)
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestMultipleUsersNoCrossContamination(t *testing.T) {
	db := newTestDB(t)

	u1, _ := GetOrCreate(db, 100)
	u2, _ := GetOrCreate(db, 200)

	Create(db, Reminder{UserID: u1.ID, Label: "U1 reminder", Times: []string{"09:00"}, Repeat: "daily"}) //nolint:errcheck
	Create(db, Reminder{UserID: u2.ID, Label: "U2 reminder", Times: []string{"10:00"}, Repeat: "daily"}) //nolint:errcheck

	r1, _ := GetAll(db, u1.ID)
	r2, _ := GetAll(db, u2.ID)

	assert.Len(t, r1, 1)
	assert.Len(t, r2, 1)
	assert.Equal(t, "U1 reminder", r1[0].Label)
	assert.Equal(t, "U2 reminder", r2[0].Label)
}

func TestEmptyTimesJSON(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 300)
	r, err := Create(db, Reminder{UserID: u.ID, Label: "Empty times", Times: []string{}, Repeat: "daily"})
	require.NoError(t, err)

	got, err := GetByID(db, r.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Times)
}

// ---------------------------------------------------------------------------
// Instance reply tests
// ---------------------------------------------------------------------------

func TestInsertInstanceReplyAndGet(t *testing.T) {
	db := newTestDB(t)

	err := InsertInstanceReply(db, 42, "instance-1")
	require.NoError(t, err)

	got, err := GetInstanceIDByReply(db, 42)
	require.NoError(t, err)
	assert.Equal(t, "instance-1", got)
}

func TestInsertInstanceReply_Overwrite(t *testing.T) {
	db := newTestDB(t)

	// Insert, then overwrite with new instance ID.
	err := InsertInstanceReply(db, 42, "instance-1")
	require.NoError(t, err)

	err = InsertInstanceReply(db, 42, "instance-2")
	require.NoError(t, err)

	got, err := GetInstanceIDByReply(db, 42)
	require.NoError(t, err)
	assert.Equal(t, "instance-2", got)
}

func TestGetInstanceIDByReply_NotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := GetInstanceIDByReply(db, 999)
	assert.ErrorContains(t, err, "not found")
}

func TestDuplicateTelegramIDReturnsExisting(t *testing.T) {
	db := newTestDB(t)

	u1, err := GetOrCreate(db, 400)
	require.NoError(t, err)

	// Set some data on the first one.
	SetTimezone(db, u1.ID, "Asia/Tokyo") //nolint:errcheck
	SetPaused(db, u1.ID, true)           //nolint:errcheck

	// GetOrCreate again.
	u2, err := GetOrCreate(db, 400)
	require.NoError(t, err)
	assert.Equal(t, u1.ID, u2.ID)
	assert.Equal(t, "Asia/Tokyo", u2.Timezone)
	assert.True(t, u2.Paused)
}

func TestTimeIndexBoundaries(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 500)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Index test", Times: []string{"09:00"}, Repeat: "daily"})

	// TimeIndex = 0 is valid.
	_, err := CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: time.Now(), TimeIndex: 0, ScheduledAt: time.Now()})
	require.NoError(t, err)

	// Large index (25 reminders per day with 30-min slots = 48 max, but test a large value).
	_, err = CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: time.Now(), TimeIndex: 100, ScheduledAt: time.Now()})
	require.NoError(t, err)

	// Both should be retrievable by GetReminderInstancesByReminder.
	instances, err := GetReminderInstancesByReminder(db, r.ID)
	require.NoError(t, err)
	assert.Len(t, instances, 2)
}

func TestGetUserByID(t *testing.T) {
	db := newTestDB(t)

	u, err := GetOrCreate(db, 700)
	require.NoError(t, err)

	got, err := GetUserByID(db, u.ID)
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)
	assert.Equal(t, int64(700), got.TelegramID)
}

func TestGetUserByID_NotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := GetUserByID(db, "nonexistent")
	assert.ErrorContains(t, err, "not found")
}

func TestGetAllUsers(t *testing.T) {
	db := newTestDB(t)

	// No users yet.
	all, err := GetAllUsers(db)
	require.NoError(t, err)
	assert.Empty(t, all)

	_, _ = GetOrCreate(db, 800)
	_, _ = GetOrCreate(db, 801)
	_, _ = GetOrCreate(db, 802)

	all, err = GetAllUsers(db)
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestDeleteReminderCascadesNothing(t *testing.T) {
	// SQLite foreign key constraints are not enforced by default in modernc.org/sqlite
	// unless PRAGMA foreign_keys = ON. We don't enable it, so deleting a reminder
	// should succeed even if instances exist.
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 600)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Cascade test", Times: []string{"09:00"}, Repeat: "daily"})
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, ForDate: time.Now(), TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"}) //nolint:errcheck

	err := Delete(db, r.ID)
	require.NoError(t, err)

	// Reminder is gone.
	_, err = GetByID(db, r.ID)
	assert.ErrorContains(t, err, "not found")
}

// ---------------------------------------------------------------------------
// GetInstancesByUserAndDay tests
// ---------------------------------------------------------------------------

func TestGetInstancesByUserAndDay(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 900)
	_ = SetTimezone(db, u.ID, "Europe/Moscow")
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Test", Times: []string{"09:00", "12:00"}, Repeat: "daily"})

	moscow, _ := time.LoadLocation("Europe/Moscow")
	// June 25 in Moscow
	date := time.Date(2026, 6, 25, 0, 0, 0, 0, moscow)

	inst1, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Date(2026, 6, 25, 0, 0, 0, 0, moscow),
		TimeIndex:   0,
		ScheduledAt: time.Date(2026, 6, 25, 9, 0, 0, 0, moscow),
		Status:      "pending",
	})
	inst2, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Date(2026, 6, 25, 0, 0, 0, 0, moscow),
		TimeIndex:   1,
		ScheduledAt: time.Date(2026, 6, 25, 12, 0, 0, 0, moscow),
		Status:      "done",
	})
	// Instance for a different day
	//nolint:errcheck // test setup
	CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Date(2026, 6, 26, 0, 0, 0, 0, moscow),
		TimeIndex:   0,
		ScheduledAt: time.Date(2026, 6, 26, 9, 0, 0, 0, moscow),
		Status:      "pending",
	})

	instances, err := GetInstancesByUserAndDay(db, u.ID, date, moscow)
	require.NoError(t, err)
	require.Len(t, instances, 2)
	assert.Equal(t, inst1.ID, instances[0].ID)
	assert.Equal(t, inst2.ID, instances[1].ID)
	assert.Equal(t, "pending", instances[0].Status)
	assert.Equal(t, "done", instances[1].Status)
}

func TestGetInstancesByUserAndDay_Empty(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 901)
	moscow, _ := time.LoadLocation("Europe/Moscow")
	date := time.Date(2026, 6, 25, 0, 0, 0, 0, moscow)

	instances, err := GetInstancesByUserAndDay(db, u.ID, date, moscow)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestGetInstancesByUserAndDay_TimezoneBoundary(t *testing.T) {
	// Test that for UTC+3, "today" starts at 21:00 UTC the previous day.
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 902)
	_ = SetTimezone(db, u.ID, "Europe/Moscow") // UTC+3
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Test", Times: []string{"09:00"}, Repeat: "daily"})

	moscow, _ := time.LoadLocation("Europe/Moscow")
	// June 25 in Moscow starts at June 24 21:00 UTC.
	utc := time.UTC

	// Instance at 2026-06-24 22:00 UTC = 2026-06-25 01:00 MSK — should be June 25 in Moscow
	instJune25, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Date(2026, 6, 25, 0, 0, 0, 0, moscow),
		TimeIndex:   0,
		ScheduledAt: time.Date(2026, 6, 24, 22, 0, 0, 0, utc),
		Status:      "pending",
	})
	// Instance at 2026-06-24 20:00 UTC = 2026-06-24 23:00 MSK — should be June 24 in Moscow
	//nolint:errcheck // test setup
	CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Date(2026, 6, 24, 0, 0, 0, 0, moscow),
		TimeIndex:   0,
		ScheduledAt: time.Date(2026, 6, 24, 20, 0, 0, 0, utc),
		Status:      "pending",
	})

	date := time.Date(2026, 6, 25, 0, 0, 0, 0, moscow)
	instances, err := GetInstancesByUserAndDay(db, u.ID, date, moscow)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, instJune25.ID, instances[0].ID)
}

func TestGetInstancesByUserAndDay_ForDateDifferentFromScheduledAt(t *testing.T) {
	// An instance with ScheduledAt on the next day (01:00) but ForDate = today
	// should still appear in today's GetInstancesByUserAndDay query.
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 903)
	_ = SetTimezone(db, u.ID, "Europe/Moscow")
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Valhalla test", Times: []string{"09:00"}, Repeat: "daily"})

	moscow, _ := time.LoadLocation("Europe/Moscow")
	forDate := time.Date(2026, 6, 25, 0, 0, 0, 0, moscow)

	// ScheduledAt = 2026-06-26 01:00 MSK (next day), ForDate = 2026-06-25 (today)
	inst, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     forDate,
		TimeIndex:   0,
		ScheduledAt: time.Date(2026, 6, 26, 1, 0, 0, 0, moscow),
		Status:      "pending",
	})

	date := time.Date(2026, 6, 25, 0, 0, 0, 0, moscow)
	instances, err := GetInstancesByUserAndDay(db, u.ID, date, moscow)
	require.NoError(t, err)
	require.Len(t, instances, 1, "instance with ScheduledAt on next day should appear in today's query")
	assert.Equal(t, inst.ID, instances[0].ID)
	assert.Equal(t, "pending", instances[0].Status)

	// Should NOT appear in tomorrow's query.
	tomorrow := time.Date(2026, 6, 26, 0, 0, 0, 0, moscow)
	instances2, err := GetInstancesByUserAndDay(db, u.ID, tomorrow, moscow)
	require.NoError(t, err)
	assert.Empty(t, instances2, "instance with ForDate=today should NOT appear in tomorrow's query")
}

// ---------------------------------------------------------------------------
// SetInstanceScheduledAt tests
// ---------------------------------------------------------------------------

func TestSetInstanceScheduledAt(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 910)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Test", Times: []string{"09:00"}, Repeat: "daily"})

	inst, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
		TimeIndex:   0,
		ScheduledAt: time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC),
		Status:      "pending",
	})

	newTime := time.Date(2026, 6, 25, 10, 30, 0, 0, time.UTC)
	err := SetInstanceScheduledAt(db, inst.ID, newTime)
	require.NoError(t, err)

	got, err := GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, newTime.Unix(), got.ScheduledAt.Unix())
	// UpdatedAt should be set (not zero), and at least >= creation time.
	assert.False(t, got.UpdatedAt.IsZero())
	assert.GreaterOrEqual(t, got.UpdatedAt.Unix(), inst.UpdatedAt.Unix())
}

func TestSetInstanceScheduledAt_NotFound(t *testing.T) {
	db := newTestDB(t)

	err := SetInstanceScheduledAt(db, "nonexistent", time.Now())
	assert.ErrorContains(t, err, "not found")
}

func TestSetInstanceScheduledAt_ForDateUnchanged(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 911)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "ForDate test", Times: []string{"09:00"}, Repeat: "daily"})

	forDate := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	inst, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     forDate,
		TimeIndex:   0,
		ScheduledAt: time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC),
		Status:      "pending",
	})

	// Set ScheduledAt to the next day (simulates snooze past midnight).
	newTime := time.Date(2026, 6, 26, 1, 0, 0, 0, time.UTC)
	err := SetInstanceScheduledAt(db, inst.ID, newTime)
	require.NoError(t, err)

	got, err := GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, newTime.Unix(), got.ScheduledAt.Unix(), "ScheduledAt should be updated")
	assert.Equal(t, forDate.Unix(), got.ForDate.Unix(), "ForDate should NOT be changed by SetInstanceScheduledAt")
}

// ---------------------------------------------------------------------------
// GetReminderInstancesByReminder tests
// ---------------------------------------------------------------------------

func TestGetReminderInstancesByReminder(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 920)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Test", Times: []string{"09:00", "12:00"}, Repeat: "daily"})

	inst1, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
		TimeIndex:   0,
		ScheduledAt: time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC),
		Status:      "done",
	})
	inst2, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
		TimeIndex:   1,
		ScheduledAt: time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC),
		Status:      "pending",
	})

	instances, err := GetReminderInstancesByReminder(db, r.ID)
	require.NoError(t, err)
	require.Len(t, instances, 2)
	assert.Equal(t, inst1.ID, instances[0].ID)
	assert.Equal(t, inst2.ID, instances[1].ID)
}

func TestGetReminderInstancesByReminder_Empty(t *testing.T) {
	db := newTestDB(t)

	instances, err := GetReminderInstancesByReminder(db, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, instances)
}

// ---------------------------------------------------------------------------
// DeleteReminderInstances tests
// ---------------------------------------------------------------------------

func TestDeleteReminderInstances(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 930)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Test", Times: []string{"09:00"}, Repeat: "daily"})

	CreateInstance(db, ReminderInstance{ //nolint:errcheck
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})
	CreateInstance(db, ReminderInstance{ //nolint:errcheck
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   1,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})

	err := DeleteReminderInstances(db, r.ID)
	require.NoError(t, err)

	instances, err := GetReminderInstancesByReminder(db, r.ID)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestDeleteReminderInstances_NoInstances(t *testing.T) {
	db := newTestDB(t)

	// Deleting instances for a reminder that has none should not error.
	err := DeleteReminderInstances(db, "nonexistent")
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// DeleteInstancesAfterIndex tests
// ---------------------------------------------------------------------------

func TestDeleteInstancesAfterIndex(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 950)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Test", Times: []string{"09:00", "10:00", "11:00"}, Repeat: "daily"})

	// Create 3 instances with indices 0, 1, 2.
	inst0, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})
	inst1, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   1,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})
	inst2, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   2,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})

	// Delete instances with time_index > 0 (should delete inst1 and inst2).
	err := DeleteInstancesAfterIndex(db, r.ID, 0)
	require.NoError(t, err)

	// inst0 should still exist.
	got, err := GetInstanceByID(db, inst0.ID)
	require.NoError(t, err)
	assert.Equal(t, inst0.ID, got.ID)

	// inst1 and inst2 should be gone.
	_, err = GetInstanceByID(db, inst1.ID)
	assert.ErrorContains(t, err, "not found")

	_, err = GetInstanceByID(db, inst2.ID)
	assert.ErrorContains(t, err, "not found")
}

func TestDeleteInstancesAfterIndex_RemovesOnlyAfter(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 951)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Test", Times: []string{"09:00", "10:00", "11:00"}, Repeat: "daily"})

	inst0, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})
	inst1, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   1,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})

	// Delete instances with time_index > 1 (should delete nothing).
	err := DeleteInstancesAfterIndex(db, r.ID, 1)
	require.NoError(t, err)

	// Both should still exist.
	got0, err := GetInstanceByID(db, inst0.ID)
	require.NoError(t, err)
	assert.Equal(t, inst0.ID, got0.ID)

	got1, err := GetInstanceByID(db, inst1.ID)
	require.NoError(t, err)
	assert.Equal(t, inst1.ID, got1.ID)
}

func TestDeleteInstancesAfterIndex_NoInstances(t *testing.T) {
	db := newTestDB(t)

	// Deleting for a reminder with no instances should not error.
	err := DeleteInstancesAfterIndex(db, "nonexistent", 0)
	require.NoError(t, err)
}

func TestDeleteInstancesAfterIndex_OnlyAffectsCorrectReminder(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 952)
	r1, _ := Create(db, Reminder{UserID: u.ID, Label: "R1", Times: []string{"09:00"}, Repeat: "daily"})
	r2, _ := Create(db, Reminder{UserID: u.ID, Label: "R2", Times: []string{"10:00"}, Repeat: "daily"})

	// Create instances for both reminders.
	instR1, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r1.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})
	CreateInstance(db, ReminderInstance{ //nolint:errcheck
		ReminderID:  r1.ID,
		ForDate:     time.Now(),
		TimeIndex:   1,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})
	instR2, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r2.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})

	// Delete from r1 with time_index > 0.
	err := DeleteInstancesAfterIndex(db, r1.ID, 0)
	require.NoError(t, err)

	// r1 instance with index 0 should still exist.
	got, err := GetInstanceByID(db, instR1.ID)
	require.NoError(t, err)
	assert.Equal(t, instR1.ID, got.ID)

	// r2 instance should be untouched.
	got2, err := GetInstanceByID(db, instR2.ID)
	require.NoError(t, err)
	assert.Equal(t, instR2.ID, got2.ID)
}

// ---------------------------------------------------------------------------
// SetStatusWithDoneAt tests
// ---------------------------------------------------------------------------

func TestSetStatusWithDoneAt(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 940)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Test", Times: []string{"09:00"}, Repeat: "daily"})

	inst, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
		TimeIndex:   0,
		ScheduledAt: time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC),
		Status:      "pending",
	})

	doneAt := time.Date(2026, 6, 25, 8, 30, 0, 0, time.UTC)
	err := SetStatusWithDoneAt(db, inst.ID, "done", doneAt)
	require.NoError(t, err)

	got, err := GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)
	require.NotNil(t, got.DoneAt)
	assert.Equal(t, doneAt.Unix(), got.DoneAt.Unix())
}

func TestSetStatusWithDoneAt_NotFound(t *testing.T) {
	db := newTestDB(t)

	err := SetStatusWithDoneAt(db, "nonexistent", "done", time.Now())
	assert.ErrorContains(t, err, "not found")
}
