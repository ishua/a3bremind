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
	t.Cleanup(func() { db.Close() })
	return db
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return tm
}

func timePtr(t time.Time) *time.Time {
	return &t
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
	Create(db, Reminder{UserID: u.ID, Label: "A", Times: []string{"09:00"}, Repeat: "daily"})
	Create(db, Reminder{UserID: u.ID, Label: "B", Times: []string{"10:00"}, Repeat: "once"})
	Create(db, Reminder{UserID: u.ID, Label: "C", Times: []string{"11:00"}, Repeat: "daily"})

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

	inst, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 0, ScheduledAt: time.Now()})

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
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 0, ScheduledAt: past, Status: "pending"})
	// Future — should NOT be pending yet.
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 1, ScheduledAt: future, Status: "pending"})
	// Past but done — should NOT be selected.
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 2, ScheduledAt: past, Status: "done"})

	pending, err := GetPending(db, now)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	assert.Equal(t, 0, pending[0].TimeIndex)
}

func TestGetActiveByUser(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 24)
	u2, _ := GetOrCreate(db, 25)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Active test", Times: []string{"09:00"}, Repeat: "daily"})
	r2, _ := Create(db, Reminder{UserID: u2.ID, Label: "Other user", Times: []string{"09:00"}, Repeat: "daily"})

	CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"})
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 1, ScheduledAt: time.Now(), Status: "pending"})
	// This instance belongs to other user.
	CreateInstance(db, ReminderInstance{ReminderID: r2.ID, TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"})
	// Done — should not appear.
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 2, ScheduledAt: time.Now(), Status: "done"})

	active, err := GetActiveByUser(db, u.ID)
	require.NoError(t, err)
	assert.Len(t, active, 2)
}

func TestGetInstanceByMessageID(t *testing.T) {
	db := newTestDB(t)

	u, _ := GetOrCreate(db, 26)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "MsgID test", Times: []string{"09:00"}, Repeat: "daily"})

	inst, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})

	// Add a message ID.
	now := time.Now()
	err := AddMessageID(db, inst.ID, 42, now)
	require.NoError(t, err)

	got, err := GetInstanceByMessageID(db, 42)
	require.NoError(t, err)
	assert.Equal(t, inst.ID, got.ID)

	// Try with a second instance that also has a message ID.
	inst2, _ := CreateInstance(db, ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   1,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})
	err = AddMessageID(db, inst2.ID, 99, now)
	require.NoError(t, err)

	got2, err := GetInstanceByMessageID(db, 99)
	require.NoError(t, err)
	assert.Equal(t, inst2.ID, got2.ID)
}

func TestGetInstanceByMessageID_NotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := GetInstanceByMessageID(db, 999)
	assert.ErrorContains(t, err, "not found")
}

func TestGetLastByReminder(t *testing.T) {
	db := newTestDB(t)

	now := time.Now().Truncate(time.Second)

	u, _ := GetOrCreate(db, 27)
	r, _ := Create(db, Reminder{UserID: u.ID, Label: "Last test", Times: []string{"09:00", "17:00"}, Repeat: "daily"})

	// TimeIndex 0 — create two instances at different times.
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 0, ScheduledAt: now.Add(-2 * time.Hour), Status: "done"})
	latest, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 0, ScheduledAt: now.Add(-1 * time.Hour), Status: "done"})

	// TimeIndex 1 — one instance.
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 1, ScheduledAt: now, Status: "pending"})

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
	inst, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"})

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
	inst, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"})

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
	inst, _ := CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"})

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
// Edge cases
// ---------------------------------------------------------------------------

func TestMultipleUsersNoCrossContamination(t *testing.T) {
	db := newTestDB(t)

	u1, _ := GetOrCreate(db, 100)
	u2, _ := GetOrCreate(db, 200)

	Create(db, Reminder{UserID: u1.ID, Label: "U1 reminder", Times: []string{"09:00"}, Repeat: "daily"})
	Create(db, Reminder{UserID: u2.ID, Label: "U2 reminder", Times: []string{"10:00"}, Repeat: "daily"})

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

func TestDuplicateTelegramIDReturnsExisting(t *testing.T) {
	db := newTestDB(t)

	u1, err := GetOrCreate(db, 400)
	require.NoError(t, err)

	// Set some data on the first one.
	SetTimezone(db, u1.ID, "Asia/Tokyo")
	SetPaused(db, u1.ID, true)

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
	_, err := CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 0, ScheduledAt: time.Now()})
	require.NoError(t, err)

	// Large index (25 reminders per day with 30-min slots = 48 max, but test a large value).
	_, err = CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 100, ScheduledAt: time.Now()})
	require.NoError(t, err)

	// Both should be retrievable by GetActiveByUser.
	active, err := GetActiveByUser(db, u.ID)
	require.NoError(t, err)
	assert.Len(t, active, 2)
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
	CreateInstance(db, ReminderInstance{ReminderID: r.ID, TimeIndex: 0, ScheduledAt: time.Now(), Status: "pending"})

	err := Delete(db, r.ID)
	require.NoError(t, err)

	// Reminder is gone.
	_, err = GetByID(db, r.ID)
	assert.ErrorContains(t, err, "not found")
}
