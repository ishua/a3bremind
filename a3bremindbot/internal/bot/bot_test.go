package bot_test

import (
	"database/sql"
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
	h := bot.NewHandler(db, mock, s)
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
	assert.Contains(t, text, "Europe/Berlin")
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
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	// Add a message ID so we can reply.
	err = store.AddMessageID(db, inst.ID, 100, time.Now())
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
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 100, time.Now())
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
	db, mock, h := setup(t)

	user, err := store.GetOrCreate(db, 12345)
	require.NoError(t, err)
	err = store.SetTimezone(db, user.ID, "UTC")
	require.NoError(t, err)

	r, err := store.Create(db, store.Reminder{
		UserID: user.ID,
		Label:  "Fallback test",
		Times:  []string{"09:00"},
		Repeat: "daily",
	})
	require.NoError(t, err)

	_, err = store.CreateInstance(db, store.ReminderInstance{
		ReminderID:  r.ID,
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	upd := updateWithText("done")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "✅")
	assert.Contains(t, text, "Fallback test")
}

func TestHandleDone_NoActive(t *testing.T) {
	_, mock, h := setup(t)

	upd := updateWithText("done")
	h.HandleUpdate(upd)

	text := mock.LastText()
	assert.Contains(t, text, "Нет активных напоминаний")
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
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "done",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 200, time.Now())
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
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 300, time.Now())
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
		TimeIndex:   0,
		ScheduledAt: time.Now().Add(-1 * time.Hour),
		Status:      "pending",
	})
	require.NoError(t, err)

	err = store.AddMessageID(db, inst.ID, 400, time.Now())
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
