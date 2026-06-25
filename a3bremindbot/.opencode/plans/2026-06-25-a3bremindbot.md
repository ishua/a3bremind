# a3bRemindBot · Фаза 1–2: store + domain

> Фаза 1: SQLite-слой, CRUD, MessageIDEntry. Фаза 2: scheduler, DailyReset, NextInstance, mock-уведомления.

## Контекст

Проект — телеграм-бот для напоминаний. Реализация снизу вверх. Фаза 1 (store) реализована: SQLite-схема, CRUD для User, Reminder, ReminderInstance, юнит-тесты.  
Фаза 1.8 добавляет в message_ids времена отправки (структура MessageIDEntry), чтобы domain мог считать попытки и интервалы без отдельных полей attempts/last_attempt_at.  
Фаза 2 (domain) — бизнес-логика: scheduler с time.Ticker, обработка pending instances, создание цепочек, daily reset.

Go module `github.com/a3bremind/a3bremindbot`, UUID через `google/uuid`, SQL через `database/sql` + `modernc.org/sqlite`. Тесты — `testing` + `stretchr/testify`.

## Цель

Получить полностью протестированный domain-слой, который:
- Запускает scheduler (time.Ticker, 1 раз в секунду)
- Обрабатывает pending instances: первое уведомление → повторы → missed
- Делает DailyReset в 03:00 по timezone пользователя: создаёт Instance для каждого daily Reminder
- Реализует NextInstance: после done/skipped → следующий time_index в цепочке
- Использует интерфейс Notifier для отправки сообщений (в тестах — mock)

---

## Фаза 1: store — слой данных

> 1.1–1.7 реализованы, 1.8 — замена []int на MessageIDEntry.

- [x] 1.1 Инициализация Go module и структуры директорий
- [x] 1.2 Схема БД (db.go)
- [x] 1.3 User CRUD (user.go)
- [x] 1.4 Reminder CRUD (reminder.go)
- [x] 1.5 ReminderInstance CRUD (instance.go)
- [x] 1.6 Юнит-тесты с SQLite in-memory
- [x] 1.7 Атомарный AddMessageID через json_set
- [ ] 1.8 Изменить MessageIDs с []int на []MessageIDEntry{MessageID, SentAt}
  - Определить в `instance.go` структуру `MessageIDEntry`:
    ```go
    type MessageIDEntry struct {
        MessageID int   `json:"message_id"`
        SentAt    int64 `json:"sent_at"` // unix timestamp
    }
    ```
  - Изменить `ReminderInstance.MessageIDs` с `[]int` на `[]MessageIDEntry`
  - Обновить `AddMessageID(db, id string, messageID int, sentAt time.Time)`: аппендит через `json_set`, `SentAt` — unix timestamp
  - Обновить `GetInstanceByMessageID`: искать по `entry.MessageID`
  - Обновить `CreateInstance`: инициализировать как `[]MessageIDEntry{}`, маршалить в JSON
  - Обновить `scanReminderInstance`: анмаршалить в `[]MessageIDEntry`
  - Обновить миграцию: `DEFAULT '[]'` остаётся, формат данных меняется только в коде
  - Обновить все тесты: `MessageIDs: []int{100}` → `MessageIDs: []MessageIDEntry{{MessageID: 100, SentAt: now.Unix()}}`
  - Обновить `TestAddMessageID_Concurrent`: проверять len == 20 по числу записей
  - Добавить `TestCreateInstance_MessageIDsInit`: убедиться что новый инстанс имеет `[]` а не `null` в JSON

---

## Фаза 2: domain — бизнес-логика

> Реализация: main loop, ProcessPending, DailyReset, NextInstance. Без Telegram — уведомления через интерфейс Notifier.

- [ ] 2.1 Определить `Notifier` interface
  - Файл: `/internal/domain/notifier.go`
  - ```go
    type Notifier interface {
        SendMessage(telegramID int64, text string) (messageID int, sentAt time.Time, err error)
    }
    ```
  - Domain ничего не знает о Telegram. `int64` — это `User.TelegramID` из store.
  - Интерфейс будет реализован в Фазе 3 (bot).

- [ ] 2.2 Определить доменные переменные конфигурации
  - Файл: `/internal/domain/config.go`
  - Переменные (не константы) — чтобы тесты могли переопределять:
    ```go
    var (
        SchedulerInterval = 1 * time.Second
        RepeatInterval    = 15 * time.Minute
        RepeatCount       = 3
        ResetHour         = 3  // 03:00 по timezone пользователя
    )
    ```

- [ ] 2.3 Реализовать `Scheduler`
  - Файл: `/internal/domain/scheduler.go`
  - ```go
    type Scheduler struct {
        db       *sql.DB
        notifier Notifier
        stopCh   chan struct{}
    }
    func New(db *sql.DB, notifier Notifier) *Scheduler
    func (s *Scheduler) Start()          // goroutine: time.Ticker(SchedulerInterval)
    func (s *Scheduler) Stop()           // close(stopCh)
    func (s *Scheduler) tick(now time.Time) // приватный, вызывается каждый тик
    ```
  - `tick` остаётся приватным. Для тестов экспортируется через `export_test.go`:
    ```go
    // export_test.go
    var Tick = (*Scheduler).tick
    ```
  - Метод `tick(now time.Time)`:
    1. `processPending(now)` — найти и обработать pending instances
    2. `checkDailyReset(now)` — проверить нужен ли сброс дня

- [ ] 2.4 ProcessPending
  - Файл: `/internal/domain/pending.go`
  - Алгоритм для каждого pending instance:
    1. Загрузить `Reminder` по `instance.ReminderID` через `store.GetByID`
    2. Загрузить `User` через `store.GetUserByID(reminder.UserID)` (новый метод, см. 2.7)
    3. Определить действие по длине `instance.MessageIDs`:
       - `len == 0` → первое уведомление: `⏰ HH:MM · Label`
       - `0 < len < RepeatCount` → повтор, но только если `now - lastEntry.SentAt >= RepeatInterval`: `🔔 Напоминаю: Label (попытка N/RepeatCount)`
       - `len < RepeatCount` но интервал ещё не прошёл → пропустить тик
    4. Отправить через `notifier.SendMessage(user.TelegramID, text)` → получить `(messageID, sentAt)`
    5. Вызвать `store.AddMessageID(db, instance.ID, messageID, sentAt)`
    6. Если `len(instance.MessageIDs) + 1 >= RepeatCount` → SetStatus("missed")

  **Важно:** missed выставляется после отправки последнего сообщения. Пользователь всегда видит финальное уведомление перед тем как instance уходит в missed. Отдельного сообщения `❌` нет — статус missed выставляется тихо после последней попытки.

- [ ] 2.5 DailyReset
  - Файл: `/internal/domain/dailyreset.go`
  - `DailyReset(db *sql.DB, userID string) error`:
    - Загрузить все reminders пользователя (`store.GetAll(userID)`)
    - Отфильтровать: только `repeat == "daily"`
    - Для каждого: взять `times[0]`, вычислить `ScheduledAt = сегодняшняя дата + times[0]` по timezone пользователя
    - Создать `ReminderInstance` через `store.CreateInstance`
    - Обновить `user.LastResetAt = now` через `store.SetLastResetAt`
  - `checkDailyReset(now time.Time) error`:
    - Получить всех пользователей через `store.GetAllUsers()` (новый метод, см. 2.7)
    - Для каждого пользователя:
      - Загрузить timezone, получить `userLoc`
      - `localNow := now.In(userLoc)`
      - Если `localNow.Hour() == ResetHour && localNow.Minute() == 0`:
        - Если `last_reset_at` nil ИЛИ `last_reset_at` был не сегодня (по timezone пользователя) → вызвать `DailyReset`
        - Иначе → пропустить (уже сброшено сегодня)

  **Важно:** проверка `last_reset_at` по дате (не по времени) в timezone пользователя защищает от повторного срабатывания в течение той же минуты при каждом тике.

- [ ] 2.6 NextInstance
  - Файл: `/internal/domain/nextinstance.go`
  - `NextInstance(db *sql.DB, instance store.ReminderInstance) error`
  - Вызывается только после `done` или `skipped` — не после `missed`
  - Загрузить `Reminder` по `instance.ReminderID`
  - Если `instance.TimeIndex < len(reminder.Times)-1`:
    - `nextIndex = instance.TimeIndex + 1`
    - `ScheduledAt = сегодняшняя дата + reminder.Times[nextIndex]` (без рескедулера — Фаза 4)
    - Создать новый Instance через `store.CreateInstance`
  - Если `instance.TimeIndex` последний в серии → цепочка завершена, ничего не создаём
    - Это одинаково для `daily` и `once`: следующий Instance для `daily` создаст DailyReset завтра в 03:00; для `once` больше ничего не происходит

- [ ] 2.7 Добавить недостающие методы в store
  - `store.GetUserByID(userID string) (User, error)` — в `user.go`, нужен для ProcessPending
  - `store.GetAllUsers() ([]User, error)` — в `user.go`, нужен для checkDailyReset
  - Написать тесты для обоих методов
  - `store.GetReminderByID` — уже есть как `GetByID`, ничего не добавлять

- [ ] 2.8 Юнит-тесты domain
  - Файл: `/internal/domain/domain_test.go`
  - Mock Notifier:
    ```go
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
    ```
  - **TestProcessPending_FirstNotification**: instance с `scheduled_at` в прошлом и пустым `MessageIDs` → tick → Notifier получил вызов с текстом `⏰`, messageID добавлен в instance
  - **TestProcessPending_Repeat**: instance с одним `MessageIDEntry` где `SentAt` старше `RepeatInterval` → tick → вызов с `🔔` и `(попытка 2/3)`
  - **TestProcessPending_RepeatTooEarly**: instance с одним `MessageIDEntry` где `SentAt` свежий → tick → Notifier не вызван
  - **TestProcessPending_Missed**: instance с `RepeatCount-1` записями в `MessageIDs`, последняя старше `RepeatInterval` → tick → отправляется последнее сообщение → статус становится `missed`
  - **TestDailyReset**: создать пользователя с daily reminder, установить `ResetHour` на текущий час, запустить `checkDailyReset` → проверить что создан Instance с правильным `scheduled_at`
  - **TestDailyReset_SkipNotResetHour**: час != ResetHour → Instance не создаётся
  - **TestDailyReset_SkipOnceReminder**: `repeat == "once"` → Instance не создаётся
  - **TestDailyReset_SkipAlreadyReset**: `last_reset_at` сегодня → DailyReset не вызывается повторно
  - **TestNextInstance_NextInChain**: instance с `time_index=0` переходит в done → NextInstance создаёт instance с `time_index=1`
  - **TestNextInstance_LastIndex**: instance с последним `time_index` → NextInstance не создаёт новый instance
  - **Integration: ProcessPending → NextInstance**: вручную SetStatus("done") + вызов NextInstance → проверить создание следующего instance в цепочке

---

## Решения и договорённости

- **Go module**: `github.com/a3bremind/a3bremindbot`, версия Go 1.25
- **UUID**: `github.com/google/uuid` (uuid.New())
- **Тесты**: `testing` + `github.com/stretchr/testify/assert`
- **Миграция**: automigration в Go-коде, без .sql файлов
- **Типы моделей**: domain использует store-типы напрямую (User, Reminder, ReminderInstance) — без дублирования структур
- **MessageIDEntry**: структура в store, хранит messageID + unix-время отправки как JSON-объект в массиве message_ids
- **AddMessageID**: принимает `messageID int, sentAt time.Time`, конвертирует в `SentAt.Unix()`, аппендит через `json_set`
- **missed**: выставляется тихо после отправки последнего сообщения — отдельного `❌`-сообщения нет
- **tick**: приватный метод, экспортируется для тестов через `export_test.go`
- **SchedulerInterval**: 1 секунда (через time.Ticker)
- **RepeatInterval**: 15 минут (переменная, тесты могут переопределить)
- **RepeatCount**: 3 (переменная, тесты могут переопределить)
- **ResetHour**: 03:00 по timezone пользователя (переменная, тесты могут переопределить)
- **DailyReset защита**: проверка `last_reset_at` по дате в timezone пользователя, не по времени
- **NextInstance**: срабатывает только при done/skipped, не при missed
- **NextInstance для последнего index**: ничего не создаёт — одинаково для daily и once; для daily новую цепочку создаст DailyReset
- **Rescheduler**: отложен в Фазу 4
- **Notifier**: симплексный (SendMessage → (messageID, sentAt)). Domain не знает о Telegram. Реальная реализация в Фазе 3.
- **once-reminder завершение**: в Фазе 2 не помечается явно — DailyReset и так пропускает once. Отложено.

## Открытые вопросы

- **once-reminder завершение**: достаточно ли что DailyReset пропускает once, или нужно поле `completed` в Reminder? Отложено на Фазу 5.