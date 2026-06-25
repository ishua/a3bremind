# a3bRemindBot · Фаза 1: слой данных (store)

> Реализация SQLite-слоя: схема БД, CRUD для User, Reminder, ReminderInstance, юнит-тесты.

## Контекст

Проект — телеграм-бот для напоминаний. Реализация снизу вверх. В репозитории есть только `spec.md` и пустой git-коммит. Фаза 1 закладывает фундамент: SQLite-схема и CRUD-операции. Go module `github.com/a3bremind/a3bremindbot`, UUID через `google/uuid`, SQL через `database/sql` + `modernc.org/sqlite`. Миграция — automigration в Go-коде (CREATE TABLE IF NOT EXISTS). Тесты — `testing` + `stretchr/testify`.

## Цель

Получить полностью протестированный слой `store`, который domain-слой (Фаза 2) сможет импортировать и использовать.

## Фаза 1: store — слой данных

> 1.1-1.6 реализованы, 1.7 запланирована: атомарный AddMessageID через json_set.

- [x] 1.1 Инициализация Go module и структуры директорий
  - `go mod init github.com/a3bremind/a3bremindbot`
  - `go get modernc.org/sqlite`, `google/uuid`, `stretchr/testify`
  - создать `/internal/store/` с файлами `db.go`, `user.go`, `reminder.go`, `instance.go`
- [x] 1.2 Схема БД (`db.go`)
  - функция `InitDB(driverName, dataSourceName string) (*sql.DB, error)`
  - automigration: три таблицы (users, reminders, reminder_instances) через CREATE TABLE IF NOT EXISTS
  - корректные типы SQLite для каждого поля согласно spec
- [x] 1.3 User CRUD (`user.go`)
  - `GetOrCreate(telegramID int64) (User, error)` — upsert через INSERT OR IGNORE + SELECT; возвращает существующего пользователя без ошибки
  - `GetByTelegramID(telegramID int64) (User, error)`
  - `SetTimezone(userID string, tz string) error`
  - `SetPaused(userID string, paused bool) error`
  - `SetLastResetAt(userID string, t time.Time) error`
- [x] 1.4 Reminder CRUD (`reminder.go`)
  - `Create(r Reminder) (Reminder, error)`
  - `GetAll(userID string) ([]Reminder, error)`
  - `GetByID(id string) (Reminder, error)`
  - `Update(r Reminder) error` — обновляет `updated_at` автоматически
  - `Delete(id string) error`
- [x] 1.5 ReminderInstance CRUD (`instance.go`)
  - `Create(i ReminderInstance) (ReminderInstance, error)`
  - `GetByID(id string) (ReminderInstance, error)`
  - `GetPending(now time.Time) ([]ReminderInstance, error)` — `scheduled_at <= now AND status = 'pending'`
  - `GetActiveByUser(userID string) ([]ReminderInstance, error)` — fallback для done без reply
  - `GetByMessageID(messageID int) (ReminderInstance, error)` — привязка reply
  - `GetLastByReminder(reminderID string, timeIndex int) (ReminderInstance, error)` — для рескедулера
  - `SetStatus(id string, status string) error` — обновляет `updated_at`
  - `SetDoneAt(id string, t time.Time) error` — обновляет `updated_at`
  - `AddMessageID(id string, messageID int) error` — аппендит message_id через json_set, атомарно; обновляет `updated_at`
- [x] 1.6 Юнит-тесты с SQLite in-memory
  - табличные тесты на каждую CRUD-операцию
  - test helper: `newTestDB(t) *sql.DB` — открывает in-memory SQLite, запускает миграцию
  - покрыть edge cases: дубликаты, отсутствующие записи, граничные значения

- [ ] 1.7 Сделать AddMessageID атомарным через SQLite json_set
  - заменить read-modify-write (SELECT + unmarshal + append + marshal + UPDATE) на один UPDATE с json_set(message_ids, '$[#]', ?)
  - убрать импорт encoding/json из instance.go (если больше не нужен)
  - добавить тест TestAddMessageID_Concurrent с запуском parallel горутин на одной записи

## Решения и договорённости

- **Go module**: `github.com/a3bremind/a3bremindbot`, версия Go 1.25
- **UUID**: `github.com/google/uuid` (uuid.New())
- **Тесты**: `testing` + `github.com/stretchr/testify/assert`
- **Миграция**: automigration в Go-коде, без .sql файлов
- **Тип telegram_id**: `int64` (Telegram API возвращает int64)
- **user.ID / reminder.ID**: храним как TEXT в SQLite (uuid string), а не BLOB — проще отладка
- **times[] в Reminder**: храним как TEXT (JSON-массив строк "HH:MM"), парсинг на уровне domain
- **message_ids[] в Instance**: храним как TEXT (JSON-массив int), парсинг на уровне domain
- **status**: храним как TEXT, не enum — SQLite не поддерживает enum нативно, валидация на уровне domain
- **Update Reminder**: нужен сразу — для пометки `once` как завершённого, для статистики и контроля повторов
- **updated_at**: на Reminder и ReminderInstance — обновляется при любом изменении, помогает при отладке и истории
- **GetOrCreate**: семантика upsert через `INSERT OR IGNORE` + `SELECT`, возвращает существующего пользователя без ошибки
- **AddMessageID**: атомарна через SQLite json_set(message_ids, '$[#]', ?). Конкурентный доступ безопасен.
- **GetByID для Instance**: добавлен — domain-слою нужно закрывать Instance по id

## Открытые вопросы

Нет — все вопросы согласованы.

---

## Промпт для реализации (вставить в новый чат)

```
Реализуй Фазу 1 телеграм-бота a3bRemindBot — слой данных (store) на Go.

## Стек
- Go 1.25
- github.com/go-telegram-bot-api/telegram-bot-api — только в будущих фазах, сейчас не нужен
- modernc.org/sqlite — pure Go SQLite, без cgo
- database/sql — стандартная библиотека
- github.com/google/uuid — генерация UUID
- github.com/stretchr/testify/assert — тесты

## Структура
go mod: github.com/a3bremind/a3bremindbot

/internal/store/
  db.go        — InitDB, automigration
  user.go      — User CRUD
  reminder.go  — Reminder CRUD
  instance.go  — ReminderInstance CRUD

## Модели

type User struct {
    ID            string    // uuid, TEXT в SQLite
    TelegramID    int64
    Timezone      string
    Paused        bool
    LastResetAt   *time.Time
    CreatedAt     time.Time
}

type Reminder struct {
    ID          string    // uuid
    UserID      string
    Label       string
    Times       []string  // ["07:00","11:00"] — хранится как JSON TEXT
    MinGap      *int      // минуты, null если одно время
    Repeat      string    // "daily" или "once"
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type ReminderInstance struct {
    ID           string    // uuid
    ReminderID   string
    TimeIndex    int
    ScheduledAt  time.Time
    DoneAt       *time.Time
    Status       string    // "pending","done","missed","skipped"
    MessageIDs   []int     // хранится как JSON TEXT
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

## Методы

### user.go
- GetOrCreate(telegramID int64) (User, error)  — INSERT OR IGNORE + SELECT
- GetByTelegramID(telegramID int64) (User, error)
- SetTimezone(userID string, tz string) error
- SetPaused(userID string, paused bool) error
- SetLastResetAt(userID string, t time.Time) error

### reminder.go
- Create(r Reminder) (Reminder, error)
- GetAll(userID string) ([]Reminder, error)
- GetByID(id string) (Reminder, error)
- Update(r Reminder) error  — обновляет updated_at
- Delete(id string) error

### instance.go
- Create(i ReminderInstance) (ReminderInstance, error)
- GetByID(id string) (ReminderInstance, error)
- GetPending(now time.Time) ([]ReminderInstance, error)  — scheduled_at <= now AND status='pending'
- GetActiveByUser(userID string) ([]ReminderInstance, error)  — status='pending', для fallback done
- GetByMessageID(messageID int) (ReminderInstance, error)
- GetLastByReminder(reminderID string, timeIndex int) (ReminderInstance, error)
- SetStatus(id string, status string) error
- SetDoneAt(id string, t time.Time) error
- AddMessageID(id string, messageID int) error  — атомарный аппенд через json_set

## Схема БД (automigration в InitDB через CREATE TABLE IF NOT EXISTS)

users:
  id TEXT PRIMARY KEY
  telegram_id INTEGER UNIQUE NOT NULL
  timezone TEXT NOT NULL DEFAULT ''
  paused INTEGER NOT NULL DEFAULT 0
  last_reset_at INTEGER  -- unix timestamp, nullable
  created_at INTEGER NOT NULL

reminders:
  id TEXT PRIMARY KEY
  user_id TEXT NOT NULL REFERENCES users(id)
  label TEXT NOT NULL
  times TEXT NOT NULL  -- JSON array
  min_gap INTEGER  -- nullable
  repeat TEXT NOT NULL  -- 'daily' | 'once'
  created_at INTEGER NOT NULL
  updated_at INTEGER NOT NULL

reminder_instances:
  id TEXT PRIMARY KEY
  reminder_id TEXT NOT NULL REFERENCES reminders(id)
  time_index INTEGER NOT NULL
  scheduled_at INTEGER NOT NULL  -- unix timestamp
  done_at INTEGER  -- nullable
  status TEXT NOT NULL DEFAULT 'pending'
  message_ids TEXT NOT NULL DEFAULT '[]'  -- JSON array
  created_at INTEGER NOT NULL
  updated_at INTEGER NOT NULL

## Тесты
- newTestDB(t *testing.T) *sql.DB — in-memory SQLite с миграцией
- табличные тесты для каждого метода
- edge cases: дубликаты, несуществующие записи, пустые массивы

Реализуй все файлы полностью, с тестами.
```