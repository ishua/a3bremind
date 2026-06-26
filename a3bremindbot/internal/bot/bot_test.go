package bot_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/a3bremind/a3bremindbot/internal/bot"
	"github.com/a3bremind/a3bremindbot/internal/domain"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

// ---------------------------------------------------------------------------
// Mock BotAPI
// ---------------------------------------------------------------------------

type mockBot struct {
	sent  []tgbotapi.MessageConfig
	msgID int
}

func (m *mockBot) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	cfg := c.(tgbotapi.MessageConfig)
	m.msgID++
	m.sent = append(m.sent, cfg)
	return tgbotapi.Message{MessageID: m.msgID}, nil
}

func (m *mockBot) LastText() string {
	if len(m.sent) == 0 {
		return ""
	}
	return m.sent[len(m.sent)-1].Text
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

func setup(t *testing.T) (*sql.DB, *mockBot, *bot.Handler) {
	t.Helper()
	db := newTestDB(t)
	mock := &mockBot{}
	s := domain.New(db, bot.NewNotifier(mock))
	t.Cleanup(s.Stop)
	h := bot.NewHandler(db, mock, s, "test")
	return db, mock, h
}

func updateWithText(text string) tgbotapi.Update {
	return tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: text,
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
		},
	}
}

func updateWithCommand(text string) tgbotapi.Update {
	upd := updateWithText(text)
	upd.Message.Entities = []tgbotapi.MessageEntity{
		{Type: "bot_command", Offset: 0, Length: len(text)},
	}
	return upd
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// ---------------------------------------------------------------------------
// /start tests
// ---------------------------------------------------------------------------

func TestHandleStart_NewUser(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithCommand("/start")
	h.HandleUpdate(upd)

	// Should send welcome message + timezone hint.
	text := mock.LastText()
	assert.Contains(t, text, "Привет")
	assert.Contains(t, text, "часовой пояс")
}

func TestHandleStart_ExistingUser(t *testing.T) {
	db, mock, h := setup(t)

	// Create user with timezone already set.
	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "Europe/Berlin")
	require.NoError(t, err)

	upd := updateWithCommand("/start")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "С возвращением")
}

// ---------------------------------------------------------------------------
// /settings timezone tests
// ---------------------------------------------------------------------------

func TestHandleSettingsTimezone(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithCommand("/settings timezone Europe/Berlin")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")
}

// ---------------------------------------------------------------------------
// Reschedule notification tests
// ---------------------------------------------------------------------------

func TestHandleDone_RescheduleWarning(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	// minGap=1440 (24h) — after any done, the next time will be pushed past midnight → warning
	minGap := 1440
	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Late test",
		Times:  []string{"09:00", "10:00"},
		Repeat: "daily",
		MinGap: &minGap,
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 100, time.Now())
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 100, inst.ID)
	require.NoError(t, err)

	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "done",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 100,
			},
		},
	}

	h.HandleUpdate(upd)

	// Should have sent ✅ + possibly ⚠️ (3 messages if also 📅)
	require.GreaterOrEqual(t, len(mock.sent), 2)
	assert.Contains(t, mock.sent[0].Text, "✅")

	// Check if ⚠️ was sent
	var hasWarning bool
	for _, msg := range mock.sent {
		if strings.Contains(msg.Text, "⚠️") {
			hasWarning = true
			break
		}
	}
	assert.True(t, hasWarning, "expected ⚠️ warning for midnight overflow")
}

func TestHandleDone_RescheduleNotification(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	minGap := 600 // 10h — large enough that shift always occurs regardless of current time
	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Gap test",
		Times:  []string{"09:00", "11:00"},
		Repeat: "daily",
		MinGap: &minGap,
	})
	require.NoError(t, err)

	// Schedule instance at past time.
	doneAt := time.Now().Add(-30 * time.Minute).Truncate(time.Second)
	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     doneAt,
		TimeIndex:   0,
		ScheduledAt: doneAt.Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 200, doneAt)
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 200, inst.ID)
	require.NoError(t, err)

	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "done",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 200,
			},
		},
	}

	h.HandleUpdate(upd)

	// Should have sent at least 2 messages: ✅ + possibly 📅
	require.GreaterOrEqual(t, len(mock.sent), 2)
	assert.Contains(t, mock.sent[0].Text, "✅")

	// Check if 📅 was sent — shift should always happen with 10h gap
	var hasSchedule bool
	for _, msg := range mock.sent {
		if strings.Contains(msg.Text, "📅") {
			hasSchedule = true
			break
		}
	}
	assert.True(t, hasSchedule, "expected 📅 notification")
}

func TestHandleDone_NoRescheduleNotification(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	// No MinGap — should not send 📅
	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "No gap",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 300, time.Now())
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 300, inst.ID)
	require.NoError(t, err)

	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "done",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 300,
			},
		},
	}

	h.HandleUpdate(upd)

	// Should only have 1 message (✅), no 📅
	require.Len(t, mock.sent, 1)
	assert.Contains(t, mock.sent[0].Text, "✅")
}

func TestHandleSettingsTimezone_Invalid(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithCommand("/settings timezone Invalid/TZ")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Неверный часовой пояс")
}

func TestHandleSettings_NoSubcommand(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithCommand("/settings")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Использование")
	assert.Contains(t, text, "/settings")
}

// ---------------------------------------------------------------------------
// /add tests
// ---------------------------------------------------------------------------

func TestHandleAdd_Daily(t *testing.T) {
	db, mock, h := setup(t)

	// Must have timezone set first.
	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "Europe/Berlin")
	require.NoError(t, err)

	upd := updateWithCommand(`/add "Test" daily 09:00`)
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")
	assert.Contains(t, text, "Test")
	assert.Contains(t, text, "09:00")

	// Verify reminder was created in DB.
	reminders, err := store.GetAll(db, user.ID)
	require.NoError(t, err)
	require.Len(t, reminders, 1)
	assert.Equal(t, "Test", reminders[0].Label)
	assert.Equal(t, "daily", reminders[0].Repeat)
	assert.Equal(t, []string{"09:00"}, reminders[0].Times)
}

func TestHandleAdd_Once(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "Europe/Berlin")
	require.NoError(t, err)

	upd := updateWithCommand(`/add "Pushups" once 09:00`)
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")

	reminders, err := store.GetAll(db, user.ID)
	require.NoError(t, err)
	require.Len(t, reminders, 1)
	assert.Equal(t, "once", reminders[0].Repeat)
}

func TestHandleAdd_NoTimezone(t *testing.T) {
	_, mock, h := setup(t)

	// User exists but no timezone set.
	upd := updateWithCommand(`/add "Test" daily 09:00`)
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "часовой пояс")
}

func TestHandleAdd_InvalidTime(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "Europe/Berlin")
	require.NoError(t, err)

	upd := updateWithCommand(`/add "Test" daily 25:00`)
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Неверный формат")
}

func TestHandleAdd_Series(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "Europe/Berlin")
	require.NoError(t, err)

	upd := updateWithCommand(`/add "Капли" daily 07:00 11:00 15:00`)
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")
	assert.Contains(t, text, "Капли")
	assert.Contains(t, text, "07:00")

	// Verify reminder was created with multiple times.
	reminders, err := store.GetAll(db, user.ID)
	require.NoError(t, err)
	require.Len(t, reminders, 1)
	assert.Equal(t, "Капли", reminders[0].Label)
	assert.Equal(t, "daily", reminders[0].Repeat)
	assert.Equal(t, []string{"07:00", "11:00", "15:00"}, reminders[0].Times)
	assert.Nil(t, reminders[0].MinGap)

	// First instance should be at time_index=0.
	active, err := store.GetActiveByUser(db, user.ID)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, 0, active[0].TimeIndex)
}

func TestHandleAdd_SeriesWithGap(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "Europe/Berlin")
	require.NoError(t, err)

	upd := updateWithCommand(`/add "Капли" daily gap:3h 07:00 11:00 15:00`)
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")
	assert.Contains(t, text, "Капли")
	assert.Contains(t, text, "gap")
	assert.Contains(t, text, "180")

	reminders, err := store.GetAll(db, user.ID)
	require.NoError(t, err)
	require.Len(t, reminders, 1)
	assert.Equal(t, []string{"07:00", "11:00", "15:00"}, reminders[0].Times)
	require.NotNil(t, reminders[0].MinGap)
	assert.Equal(t, 180, *reminders[0].MinGap)
}

// ---------------------------------------------------------------------------
// done/ok/+ tests
// ---------------------------------------------------------------------------

func TestHandleDone_Reply(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Test reminder",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	// Add a message ID so we can reply.
	err = store.AddMessageID(db, inst.ID, 100, time.Now())
	require.NoError(t, err)

	// Insert reply mapping (as scheduler would).
	err = store.InsertInstanceReply(db, 100, inst.ID)
	require.NoError(t, err)

	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "done",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 100,
			},
		},
	}

	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")
	assert.Contains(t, text, "Test reminder")

	// Verify instance is done.
	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)
	require.NotNil(t, got.DoneAt)
}

func TestHandleDone_NextInstanceCreated(t *testing.T) {
	db, _, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	// Reminder with 2 times — NextInstance should create the second one.
	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Chain test",
		Times:  []string{"09:00", "12:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 100, time.Now())
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 100, inst.ID)
	require.NoError(t, err)

	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "done",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 100,
			},
		},
	}

	h.HandleUpdate(upd)

	// Should have created a new instance with time_index=1.
	active, err := store.GetActiveByUser(db, user.ID)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, 1, active[0].TimeIndex)
}

func TestHandleDone_NoReplyFallback(t *testing.T) {
	_, mock, h := setup(t)

	// Without reply, done should tell the user to use reply or /done <uuid>.
	upd := updateWithText("done")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Используй reply")
}

func TestHandleDone_NoActive(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithText("done")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Используй reply")
}

func TestHandleDone_AlreadyDone(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Done already",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "done",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 200, time.Now())
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 200, inst.ID)
	require.NoError(t, err)

	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "done",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 200,
			},
		},
	}

	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "уже выполнено")
}

func TestHandleDone_OkSynonym(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Ok test",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 300, time.Now())
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 300, inst.ID)
	require.NoError(t, err)

	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "ok",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 300,
			},
		},
	}

	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")
}

func TestHandleDone_PlusSynonym(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Plus test",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 400, time.Now())
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 400, inst.ID)
	require.NoError(t, err)

	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "+",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 400,
			},
		},
	}

	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")
}

// ---------------------------------------------------------------------------
// /schedule tests
// ---------------------------------------------------------------------------

func TestHandleSchedule_Today(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Test reminder",
		Times:  []string{"09:00", "12:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)
	// Сегодня в 09:00
	today09 := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.UTC)
	// Сегодня в 12:00
	today12 := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)

	store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: today09,
		Status:      "done",
	})
	store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   1,
		ScheduledAt: today12,
		Status:      "pending",
	})

	upd := updateWithCommand("/schedule")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "📅")
	assert.Contains(t, text, "Test reminder")
	assert.Contains(t, text, "✅")
	assert.Contains(t, text, "⏳")
	assert.Contains(t, text, "09:00")
	assert.Contains(t, text, "12:00")
}

func TestHandleSchedule_Empty(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	upd := updateWithCommand("/schedule")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Нет напоминаний на сегодня")
}

func TestHandleSchedule_NoTimezone(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithCommand("/schedule")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "часовой пояс")
}

// ---------------------------------------------------------------------------
// /list tests
// ---------------------------------------------------------------------------

func TestHandleList_WithReminders(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	minGap := 180
	store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Капли",
		Times:  []string{"07:00", "11:00", "15:00", "18:00", "21:00"},
		MinGap: &minGap,
		Repeat: "daily",
	})
	store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Отжимания",
		Times:  []string{"09:00"},
		Repeat: "once",
	})

	upd := updateWithCommand("/list")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "📋")
	assert.Contains(t, text, "Капли")
	assert.Contains(t, text, "Отжимания")
	assert.Contains(t, text, "daily")
	assert.Contains(t, text, "once")
	assert.Contains(t, text, "07:00 11:00 15:00 18:00 21:00")
	assert.Contains(t, text, "09:00")
	assert.Contains(t, text, "gap: 3ч")
	assert.Contains(t, text, "/delete")
	assert.Contains(t, text, "/list instances")
	// 🆔 should not appear anymore — replaced by clickable commands
	assert.NotContains(t, text, "🆔")
}

func TestHandleList_Empty(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	upd := updateWithCommand("/list")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Нет настроенных напоминаний")
}

// ---------------------------------------------------------------------------
// /list instances tests
// ---------------------------------------------------------------------------

func TestHandleListInstances_ShowsInstances(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Капли",
		Times:  []string{"07:00", "11:00", "15:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)
	// Create instances for today
	inst0, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, time.UTC),
		Status:      "done",
	})
	require.NoError(t, err)
	inst1, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   1,
		ScheduledAt: time.Date(now.Year(), now.Month(), now.Day(), 11, 0, 0, 0, time.UTC),
		Status:      "missed",
	})
	require.NoError(t, err)
	inst2, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   2,
		ScheduledAt: time.Date(now.Year(), now.Month(), now.Day(), 15, 0, 0, 0, time.UTC),
		Status:      "pending",
	})
	require.NoError(t, err)

	upd := updateWithCommand(fmt.Sprintf("/list instances %s", r.ID))
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "💊")
	assert.Contains(t, text, "Капли")
	assert.Contains(t, text, "✅ 07:00")
	assert.Contains(t, text, "❌ 11:00")
	assert.Contains(t, text, "⏳ 15:00")
	// Should show /done commands with full UUID
	assert.Contains(t, text, fmt.Sprintf("/done %s 07:00", inst0.ID))
	assert.Contains(t, text, fmt.Sprintf("/done %s 11:00", inst1.ID))
	assert.Contains(t, text, fmt.Sprintf("/done %s 15:00", inst2.ID))
}

func TestHandleListInstances_WrongUser(t *testing.T) {
	db, mock, h := setup(t)

	// Create user 12345 with timezone
	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	// Create a reminder for a different user
	otherUser, err := store.GetOrCreate(db, 99999)
	require.NoError(t, err)
	r, err := store.Create(db, store.Reminder{
		UserID: otherUser.ID,
		Label:  "Чужое",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	upd := updateWithCommand(fmt.Sprintf("/list instances %s", r.ID))
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Напоминание не найдено")
}

func TestHandleListInstances_NoTimezone(t *testing.T) {
	_, mock, h := setup(t)

	// User exists but no timezone
	upd := updateWithCommand("/list instances some-uuid")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "часовой пояс")
}

func TestHandleListInstances_NoArgs(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithCommand("/list instances")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Использование")
}

func TestHandleSkip_Active(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Skip test",
		Times:  []string{"09:00", "12:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)
	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: now.Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	upd := updateWithCommand("/skip")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "⏭️")
	assert.Contains(t, text, "Skip test")
	assert.Contains(t, text, "пропущено")

	// Verify instance is skipped
	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "skipped", got.Status)

	// Verify next instance was created (time_index=1)
	active, err := store.GetActiveByUser(db, user.ID)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, 1, active[0].TimeIndex)
}

func TestHandleSkip_NoActive(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithCommand("/skip")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Нет активных напоминаний")
}

func TestHandleSkip_LastIndex(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	// Only one time — last index is 0
	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Single",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)
	store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: now.Add(-1 * time.Hour),
		Status:      "pending",
	})

	upd := updateWithCommand("/skip")
	h.HandleUpdate(upd)

	// Should be skipped without creating next instance
	text := mock.LastText()
	assert.Contains(t, text, "⏭️")

	// No active instances remain
	active, err := store.GetActiveByUser(db, user.ID)
	require.NoError(t, err)
	assert.Empty(t, active)
}

// ---------------------------------------------------------------------------
// /snooze tests
// ---------------------------------------------------------------------------

func TestHandleSnooze(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Snooze test",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	originalTime := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     originalTime,
		TimeIndex:   0,
		ScheduledAt: originalTime,
		Status:      "pending",
	})
	require.NoError(t, err)

	// Snooze for 30 minutes
	upd := updateWithCommand("/snooze 30")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "🔇")
	assert.Contains(t, text, "Snooze test")
	assert.Contains(t, text, "30 минут")

	// Verify scheduled_at was shifted
	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Greater(t, got.ScheduledAt.Unix(), originalTime.Unix(),
		"scheduled_at should be shifted forward")
}

func TestHandleSnooze_Invalid(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithCommand("/snooze abc")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Использование")
}

func TestHandleSnooze_OutOfRange(t *testing.T) {
	_, mock, h := setup(t)

	// Test 0
	upd := updateWithCommand("/snooze 0")
	h.HandleUpdate(upd)
	text := mock.LastText()
	assert.Contains(t, text, "Использование")

	// Test 1441 (over 24h)
	mock.sent = nil
	upd = updateWithCommand("/snooze 1441")
	h.HandleUpdate(upd)
	text = mock.LastText()
	assert.Contains(t, text, "Использование")
}

func TestHandleSnooze_NoActive(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithCommand("/snooze 30")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Нет активных напоминаний")
}

// ---------------------------------------------------------------------------
// /pause and /resume tests
// ---------------------------------------------------------------------------

func TestHandlePause(t *testing.T) {
	db, mock, h := setup(t)

	_, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)

	upd := updateWithCommand("/pause")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "⏸")

	got, err := store.GetByTelegramID(db, 12345)
	require.NoError(t, err)
	assert.True(t, got.Paused)
}

func TestHandleResume(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)

	// First pause
	_ = store.SetPaused(db, user.ID, true)

	upd := updateWithCommand("/resume")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "▶️")

	got, err := store.GetByTelegramID(db, 12345)
	require.NoError(t, err)
	assert.False(t, got.Paused)
}

func TestHandleDone_WhilePaused(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	// Pause the user
	err = store.SetPaused(db, user.ID, true)
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Paused test",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 500, time.Now())
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 500, inst.ID)
	require.NoError(t, err)

	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "done",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 500,
			},
		},
	}

	h.HandleUpdate(upd)

	// done should work even when paused
	text := mock.LastText()
	assert.Contains(t, text, "✅")

	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)
}

// ---------------------------------------------------------------------------
// done HH:MM tests
// ---------------------------------------------------------------------------

func TestHandleDone_WithTime_Past(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Past test",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-2 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 101, time.Now())
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 101, inst.ID)
	require.NoError(t, err)

	// Reply with "done 06:30"
	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "done 06:30",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 101,
			},
		},
	}
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Записать")
	assert.Contains(t, text, "06:30")
	assert.Contains(t, text, "+")
}

func TestHandleDone_TimeConfirm_Yes(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Confirm test",
		Times:  []string{"09:00", "12:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-2 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 102, time.Now())
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 102, inst.ID)
	require.NoError(t, err)

	// First: done HH:MM with reply to set up pending confirm
	upd1 := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "done 06:30",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 102,
			},
		},
	}
	h.HandleUpdate(upd1)

	// Now: confirm with "+"
	upd2 := updateWithText("+")
	h.HandleUpdate(upd2)

	// Should have sent "✅ Confirm test — записано в 06:30"
	text := mock.LastText()
	assert.Contains(t, text, "✅")
	assert.Contains(t, text, "Confirm test")
	assert.Contains(t, text, "06:30")

	// Verify instance is done with done_at = 06:30
	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)
	require.NotNil(t, got.DoneAt)
	assert.Equal(t, 6, got.DoneAt.In(time.UTC).Hour())
	assert.Equal(t, 30, got.DoneAt.In(time.UTC).Minute())

	// Next instance should be created
	active, err := store.GetActiveByUser(db, user.ID)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, 1, active[0].TimeIndex)
}

func TestHandleDone_WithTime_Future(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, _ := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Future test",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-2 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 103, time.Now())
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 103, inst.ID)
	require.NoError(t, err)

	// A time 1 hour in the future
	futureTime := time.Now().In(time.UTC).Add(1 * time.Hour).Format("15:04")
	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "done " + futureTime,
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 103,
			},
		},
	}
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "будущем")
}

func TestHandleDone_WithTime_NoConfirm(t *testing.T) {
	_, mock, h := setup(t)

	// Just "+" without any pending confirm → should be treated as normal done
	// Without reply, should say to use reply or /done <uuid>
	upd := updateWithText("+")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Используй reply")
}

func TestHandleDelete(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Delete me",
		Times:  []string{"09:00", "12:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	// Create instances
	store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now(),
		Status:      "pending",
	})
	store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   1,
		ScheduledAt: time.Now().Add(3 * time.Hour),
		Status:      "pending",
	})

	upd := updateWithCommand(fmt.Sprintf("/delete %s", r.ID))
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "🗑")
	assert.Contains(t, text, "Delete me")

	// Reminder should be gone
	_, err = store.GetByID(db, r.ID)
	assert.ErrorContains(t, err, "not found")

	// Instances should be gone
	instances, err := store.GetReminderInstancesByReminder(db, r.ID)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestHandleDelete_NotFound(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithCommand("/delete nonexistent-uuid")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Напоминание не найдено")
}

func TestHandleDelete_WrongUser(t *testing.T) {
	db, mock, h := setup(t)

	// Create user and reminder
	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Not yours",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	// Different chat ID — different user
	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: fmt.Sprintf("/delete %s", r.ID),
			Chat: &tgbotapi.Chat{ID: 99999},
			From: &tgbotapi.User{ID: 99999},
		},
	}
	// Manually set bot_command entities for IsCommand() to work
	upd.Message.Entities = []tgbotapi.MessageEntity{
		{Type: "bot_command", Offset: 0, Length: len(fmt.Sprintf("/delete %s", r.ID))},
	}

	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Напоминание не найдено")

	// Original reminder should still exist
	_, err = store.GetByID(db, r.ID)
	require.NoError(t, err)
}

func TestHandleDone_WithTime_Reply(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Reply time",
		Times:  []string{"09:00", "12:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	// Create two pending instances — the first one is not the last active
	inst1, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-2 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	inst2, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   1,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	// Add a message ID to inst1 so we can reply to it
	err = store.AddMessageID(db, inst1.ID, 100, time.Now())
	require.NoError(t, err)

	err = store.InsertInstanceReply(db, 100, inst1.ID)
	require.NoError(t, err)

	// Reply to the first instance's message with "done 06:30"
	upd := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "done 06:30",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 100,
			},
		},
	}

	h.HandleUpdate(upd)

	// Should ask for confirmation with the first instance
	text := mock.LastText()
	assert.Contains(t, text, "Записать")
	assert.Contains(t, text, "06:30")

	// Confirm
	upd2 := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: "+",
			Chat: &tgbotapi.Chat{ID: 12345},
			From: &tgbotapi.User{ID: 12345},
		},
	}
	h.HandleUpdate(upd2)

	text = mock.LastText()
	assert.Contains(t, text, "✅")
	assert.Contains(t, text, "Reply time")

	// inst1 should be done with done_at = 06:30
	got, err := store.GetInstanceByID(db, inst1.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)

	// inst2 should still be pending (not the one we replied to)
	got2, err := store.GetInstanceByID(db, inst2.ID)
	require.NoError(t, err)
	assert.Equal(t, "pending", got2.Status)
}

// ---------------------------------------------------------------------------
// /done <uuid> tests
// ---------------------------------------------------------------------------

func TestHandleDoneByUUID_MissedToDone(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Капли",
		Times:  []string{"07:00", "11:00", "15:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)

	// Instance 0 is done
	_, err = store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   0,
		ScheduledAt: time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, time.UTC),
		Status:      "done",
		DoneAt:      timePtr(time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, time.UTC)),
	})
	require.NoError(t, err)

	// Instance 1 is missed — this is the one we'll mark done
	missedInst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   1,
		ScheduledAt: time.Date(now.Year(), now.Month(), now.Day(), 11, 0, 0, 0, time.UTC),
		Status:      "missed",
	})
	require.NoError(t, err)

	// Instance 2 is pending (should be deleted and recreated)
	pendingInst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     now,
		TimeIndex:   2,
		ScheduledAt: time.Date(now.Year(), now.Month(), now.Day(), 15, 0, 0, 0, time.UTC),
		Status:      "pending",
	})
	require.NoError(t, err)

	// Mark missed instance as done via /done <uuid>
	upd := updateWithCommand(fmt.Sprintf("/done %s 11:00", missedInst.ID))
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")
	assert.Contains(t, text, "Капли")
	assert.Contains(t, text, "11:00")

	// Verify missed instance is now done
	got, err := store.GetInstanceByID(db, missedInst.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)

	// Verify pending instance was deleted (time_index > 1)
	_, err = store.GetInstanceByID(db, pendingInst.ID)
	assert.ErrorContains(t, err, "not found")

	// Verify a new instance was created at index 2
	active, err := store.GetActiveByUser(db, user.ID)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, 2, active[0].TimeIndex)
}

func TestHandleDoneByUUID_WithTime(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Test",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-2 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	upd := updateWithCommand(fmt.Sprintf("/done %s 08:30", inst.ID))
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")
	assert.Contains(t, text, "08:30")

	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)
	require.NotNil(t, got.DoneAt)
	assert.Equal(t, 8, got.DoneAt.In(time.UTC).Hour())
	assert.Equal(t, 30, got.DoneAt.In(time.UTC).Minute())
}

func TestHandleDoneByUUID_WithoutTime(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Test",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-2 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	upd := updateWithCommand(fmt.Sprintf("/done %s", inst.ID))
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")
	assert.Contains(t, text, "Test")

	got, err := store.GetInstanceByID(db, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)
	require.NotNil(t, got.DoneAt)
}

func TestHandleDoneByUUID_AlreadyDone(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Done already",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	inst, err := store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		ForDate:     time.Now(),
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-2 * time.Hour),
		Status:      "done",
		DoneAt:      timePtr(time.Now()),
	})
	require.NoError(t, err)

	upd := updateWithCommand(fmt.Sprintf("/done %s", inst.ID))
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "уже выполнено")
}

func TestHandleDoneByUUID_InvalidUUID(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithCommand("/done short-uuid")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Неверный UUID")
}

func TestHandleDoneByUUID_NotFound(t *testing.T) {
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	// Valid-looking UUID but doesn't exist
	upd := updateWithCommand("/done 00000000-0000-0000-0000-000000000000")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Напоминание не найдено")
}
