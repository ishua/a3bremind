# a3bRemindBot · Фаза 1–3: store + domain + bot

> Фаза 1: SQLite-слой, CRUD, MessageIDEntry. Фаза 2: scheduler, DailyReset, NextInstance, mock-уведомления. Фаза 3: Telegram-интеграция — /start, /add, done.

## Контекст

Проект — телеграм-бот для напоминаний. Реализация снизу вверх. Фаза 1 (store) реализована: SQLite-схема, CRUD для User, Reminder, ReminderInstance, юнит-тесты.  
Фаза 1.8 добавляет в message_ids времена отправки (структура MessageIDEntry), чтобы domain мог считать попытки и интервалы без отдельных полей attempts/last_attempt_at.  
Фаза 2 (domain) — бизнес-логика: scheduler с time.Ticker, обработка pending instances, создание цепочек, daily reset.  
Фаза 3 (bot) — Telegram-интеграция: live-бот с /start, /add, done.

Go module `github.com/a3bremind/a3bremindbot`, UUID через `google/uuid`, SQL через `database/sql` + `modernc.org/sqlite`. Тесты — `testing` + `stretchr/testify`.

## Цель

Получить полностью протестированный store и domain-слой, и минимальный живой Telegram-бот, который:
- Запускает scheduler (time.Ticker, 1 раз в секунду)
- Обрабатывает pending instances: первое уведомление → повторы → missed
- Делает DailyReset в 03:00 по timezone пользователя
- Реализует NextInstance: после done/skipped → следующий time_index в цепочке
- Принимает `/start` с запросом timezone
- Принимает `/add` для создания напоминаний (одно время, без серии и рескедулера)
- Обрабатывает `done`/`ok`/`+` с reply и fallback на последний активный Instance

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
- [x] 1.8 Изменить MessageIDs с []int на []MessageIDEntry{MessageID, SentAt}
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
> Выполнено: 2.1–2.8. Изменён порядок (2.7 перенесена перед 2.4 из-за зависимости). Реализован scheduler, processPending, dailyReset, nextInstance, 11 domain-тестов.

- [x] 2.1 Определить `Notifier` interface
  - Файл: `/internal/domain/notifier.go`
  - ```go
    type Notifier interface {
        SendMessage(telegramID int64, text string) (messageID int, sentAt time.Time, err error)
    }
    ```
  - Domain ничего не знает о Telegram. `int64` — это `User.TelegramID` из store.
  - Интерфейс будет реализован в Фазе 3 (bot).

- [x] 2.2 Определить доменные переменные конфигурации
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

- [x] 2.3 Реализовать `Scheduler`
  - Файл: `/internal/domain/scheduler.go`
  - ```go
    type Scheduler struct {
        db       *sql.DB
        notifier Notifier
        stopCh   chan struct{}
    }
    func New(db *sql.DB, notifier Notifier) *Scheduler
    func (s *Scheduler) Start()             // goroutine: time.Ticker(SchedulerInterval)
    func (s *Scheduler) Stop()              // close(stopCh)
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

- [x] 2.7 Добавить недостающие методы в store
  - `store.GetUserByID(userID string) (User, error)` — в `user.go`, нужен для ProcessPending
  - `store.GetAllUsers() ([]User, error)` — в `user.go`, нужен для checkDailyReset
  - Написать тесты для обоих методов
  - `store.GetReminderByID` — уже есть как `GetByID`, ничего не добавлять

- [x] 2.4 ProcessPending
  - Файл: `/internal/domain/pending.go`
  - Алгоритм для каждого pending instance:
    1. Загрузить `Reminder` по `instance.ReminderID` через `store.GetByID`
    2. Загрузить `User` через `store.GetUserByID(reminder.UserID)`
    3. Определить действие по длине `instance.MessageIDs`:
       - `len == 0` → первое уведомление: `⏰ HH:MM · Label`
       - `0 < len < RepeatCount` → повтор, но только если `now - lastEntry.SentAt >= RepeatInterval`: `🔔 Напоминаю: Label (попытка N/RepeatCount)`
       - `len < RepeatCount` но интервал ещё не прошёл → пропустить тик
    4. Отправить через `notifier.SendMessage(user.TelegramID, text)` → получить `(messageID, sentAt)`
    5. Вызвать `store.AddMessageID(db, instance.ID, messageID, sentAt)`
    6. Если `len(instance.MessageIDs) + 1 >= RepeatCount` → `store.SetStatus(db, instance.ID, "missed")`

  **Важно:** missed выставляется после отправки последнего сообщения. Пользователь всегда видит финальное уведомление перед тем как instance уходит в missed. Отдельного сообщения `❌` нет — статус missed выставляется тихо после последней попытки.

- [x] 2.5 DailyReset
  - Файл: `/internal/domain/dailyreset.go`
  - `DailyReset(db *sql.DB, userID string) error`:
    - Загрузить все reminders пользователя (`store.GetAll(userID)`)
    - Отфильтровать: только `repeat == "daily"`
    - Для каждого: взять `times[0]`, вычислить `ScheduledAt = сегодняшняя дата + times[0]` по timezone пользователя
    - Создать `ReminderInstance` через `store.CreateInstance`
    - Обновить `user.LastResetAt = now` через `store.SetLastResetAt`
  - `checkDailyReset(now time.Time) error`:
    - Получить всех пользователей через `store.GetAllUsers()`
    - Для каждого пользователя:
      - Загрузить timezone, получить `userLoc`
      - `localNow := now.In(userLoc)`
      - Если `localNow.Hour() == ResetHour && localNow.Minute() == 0`:
        - Если `last_reset_at` nil ИЛИ `last_reset_at` был не сегодня (по timezone пользователя) → вызвать `DailyReset`
        - Иначе → пропустить (уже сброшено сегодня)

  **Важно:** проверка `last_reset_at` по дате (не по времени) в timezone пользователя защищает от повторного срабатывания в течение той же минуты при каждом тике.

- [x] 2.6 NextInstance
  - Файл: `/internal/domain/nextinstance.go`
  - `NextInstance(db *sql.DB, instance store.ReminderInstance) error`
  - Вызывается только после `done` или `skipped` — не после `missed`
  - Загрузить `Reminder` по `instance.ReminderID`
  - Если `instance.TimeIndex < len(reminder.Times)-1`:
    - `nextIndex = instance.TimeIndex + 1`
    - `ScheduledAt = сегодняшняя дата + reminder.Times[nextIndex]` (без рескедулера — Фаза 4)
    - Создать новый Instance через `store.CreateInstance`
  - Если `instance.TimeIndex` последний в серии → цепочка завершена, ничего не создаём
    - Одинаково для `daily` и `once`: для `daily` новую цепочку создаст DailyReset завтра в 03:00; для `once` больше ничего не происходит

- [x] 2.8 Юнит-тесты domain
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

## Фаза 3: bot — минимальный живой бот

> Telegram-интеграция. `/start`, `/settings timezone`, `/add` (одно время), `done`/`ok`/`+` с reply и fallback. Все 16 тестов проходят.

- [x] 3.1 Добавить зависимость go-telegram-bot-api
  - `go get github.com/go-telegram-bot-api/telegram-bot-api/v5`

- [x] 3.2 Определить BotAPI interface
  - Файл: `internal/bot/handler.go`
  - Интерфейс содержит ровно один метод — больше не добавлять:
    ```go
    type BotAPI interface {
        Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
    }
    ```
  - Реальный `*tgbotapi.BotAPI` удовлетворяет интерфейсу нативно, мок — в тестах

- [x] 3.3 `bot/notifier.go` — реализация domain.Notifier
  - ```go
    type Notifier struct {
        bot BotAPI
    }
    ```
  - `SendMessage(telegramID int64, text string) (messageID int, sentAt time.Time, err error)`
  - Создаёт `tgbotapi.NewMessage(telegramID, text)`, вызывает `bot.Send()`, возвращает `msg.MessageID` и `time.Now()`

- [x] 3.4 `bot/handler.go` — маршрутизация
  - Структура `Handler` с полями: `db`, `bot` (BotAPI интерфейс), `scheduler`
  - `HandleUpdate(update)`:
    - `update.Message == nil` → return
    - `IsCommand()` → роутинг по команде: `/start`, `/settings`, `/add`, неизвестная → "Неизвестная команда"
    - Текст `"done"`, `"ok"`, `"+"` (strings.ToLower, strings.TrimSpace) → `handleDone`
    - Остальное → игнор

- [x] 3.5 `bot/commands.go` — обработка команд

  **`/start`:**
  - `store.GetOrCreate` пользователя
  - Если timezone пустая → приветствие + "Укажи часовой пояс: `/settings timezone Europe/Berlin`"
  - Если timezone задана → "С возвращением!"

  **`/settings timezone <value>`:**
  - Парсинг: после `/settings` → субкоманда `timezone` + значение
  - Если субкоманда не `timezone` или нет значения → "Использование: `/settings timezone Europe/Berlin`"
  - Валидация через `time.LoadLocation(value)` — ошибка если невалидный
  - `store.SetTimezone` → "✅ Часовой пояс установлен: Europe/Berlin"

  **`/add "Label" daily|once HH:MM`** (ручной парсинг, без regexp):
  - Функция `parseAddCommand(text string) → (label, repeat, time string, err error)`:
    - Извлекает label из кавычек (первая пара `"..."`)
    - repeat: `daily` или `once` — ошибка если другое
    - ровно одно время `HH:MM` — валидация через `time.Parse("15:04", value)`
  - Проверка: пользователь существует и timezone задана — иначе "Сначала укажи часовой пояс"
  - `store.Create(reminder)` → `store.CreateInstance` с `ScheduledAt = today + HH:MM` в timezone пользователя
  - **Если время уже прошло сегодня**: Instance всё равно создаётся, scheduler пришлёт уведомление немедленно при следующем тике. Это осознанное поведение — пользователь получит напоминание сразу. В Фазе 5 можно добавить предупреждение.
  - Ответ: "✅ Напоминание «Label» создано. Первое — сегодня в HH:MM."

- [x] 3.6 `bot/done.go` — обработка подтверждений

  **`handleDone`:**
  1. Получить пользователя через `store.GetOrCreate`
  2. **Если есть reply:**
     - `update.Message.ReplyToMessage.MessageID` → `store.GetInstanceByMessageID`
     - Не найдено → "Не удалось найти напоминание"
     - Статус не `pending` → "Это напоминание уже выполнено"
     - `store.SetStatus(db, instance.ID, "done")` — проставляет `done_at = time.Now()` автоматически
     - `domain.NextInstance(db, instance)`
     - Ответ: "✅ Label — записано в HH:MM" где HH:MM берётся из `done_at` обновлённого instance (перечитать из store после SetStatus)
  3. **Если нет reply:**
     - `store.GetActiveByUser(user.ID)` → берём последний по `scheduled_at DESC`
     - Пусто → "Нет активных напоминаний"
     - `store.SetStatus(db, instance.ID, "done")` + `domain.NextInstance`
     - Ответ аналогичный

  **`done_at`:** всегда `time.Now()` в момент вызова `SetStatus("done")`. Ручное указание времени (`done 09:15`) — Фаза 5.

- [x] 3.7 `cmd/main.go` — точка входа
  - Токен из env: `os.Getenv("TELEGRAM_BOT_TOKEN")` — panic если пустой
  - `store.InitDB("sqlite", "bot.db")`
  - Создание `*tgbotapi.BotAPI`
  - Wire-up: `bot.NewNotifier(botAPI)` → `domain.New(db, notifier)` → `bot.NewHandler(db, botAPI, scheduler)`
  - `scheduler.Start()` / `defer scheduler.Stop()`
  - Long-polling loop: `botAPI.GetUpdatesChan(cfg)` → `handler.HandleUpdate(update)`

- [x] 3.8 Тесты (`internal/bot/bot_test.go`)

  Mock BotAPI:
  ```go
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
  ```

  - **TestHandleStart_NewUser** — `/start` → приветствие + запрос timezone
  - **TestHandleStart_ExistingUser** — `/start` повторно → "с возвращением"
  - **TestHandleSettingsTimezone** — `/settings timezone Europe/Berlin` → подтверждение
  - **TestHandleSettingsTimezone_Invalid** — `/settings timezone Invalid/TZ` → ошибка
  - **TestHandleSettings_NoSubcommand** — `/settings` → справка
  - **TestHandleAdd_Daily** — `/add "Test" daily 09:00` → reminder + instance созданы, repeat=daily
  - **TestHandleAdd_Once** — `/add "Pushups" once 09:00` → repeat=once
  - **TestHandleAdd_NoTimezone** — `/add` до установки timezone → ошибка
  - **TestHandleAdd_InvalidTime** — `/add "Test" daily 25:00` → ошибка парсинга
  - **TestHandleDone_Reply** — reply на сообщение бота → статус `done`, `done_at` проставлен
  - **TestHandleDone_NextInstanceCreated** — reply на instance с `time_index=0` в серии → после done создан instance с `time_index=1`
  - **TestHandleDone_NoReplyFallback** — `done` без reply → fallback к последнему активному
  - **TestHandleDone_NoActive** — `done` без активных → "нет активных напоминаний"
  - **TestHandleDone_AlreadyDone** — reply на уже `done` instance → "уже выполнено"
  - **TestHandleDone_OkSynonym** — `"ok"` → то же что done
  - **TestHandleDone_PlusSynonym** — `"+"` → то же что done

---

## Решения и договорённости

- **Go module**: `github.com/a3bremind/a3bremindbot`, версия Go 1.25
- **UUID**: `github.com/google/uuid` (uuid.New())
- **Тесты**: `testing` + `github.com/stretchr/testify/assert`
- **Миграция**: automigration в Go-коде, без .sql файлов
- **Типы моделей**: domain использует store-типы напрямую (User, Reminder, ReminderInstance) — без дублирования структур
- **MessageIDEntry**: структура в store, хранит messageID + unix-время отправки как JSON-объект в массиве message_ids
- **AddMessageID**: принимает `messageID int, sentAt time.Time`, конвертирует в `SentAt.Unix()`, аппендит через `json_set`
- **SetStatus("done")**: автоматически проставляет `done_at = time.Now()` — отдельный вызов `SetDoneAt` не нужен
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
- **Notifier**: симплексный (SendMessage → (messageID, sentAt)). Domain не знает о Telegram. Реализован в bot/notifier.go.
- **BotAPI interface**: ровно один метод `Send` — больше не добавлять
- **`/add` с immediate instance**: первый Instance создаётся сразу. Если время прошло — scheduler пришлёт уведомление при следующем тике. Предупреждение — Фаза 5.
- **`done_at`**: всегда `time.Now()` в момент `SetStatus("done")`. Ручное указание времени (`done 09:15`) — Фаза 5.
- **`done` без reply**: берётся последний `pending` instance пользователя по `scheduled_at DESC`
- **Неизвестные команды**: ответ "Неизвестная команда"
- **Неизвестный текст**: игнорируется (кроме done/ok/+)
- **done при paused**: работает — paused только для автоматических уведомлений
- **Telegram токен**: через `TELEGRAM_BOT_TOKEN` env var, panic если пустой
- **Путь к БД**: `bot.db` в CWD
- **once-reminder завершение**: DailyReset пропускает once — явной пометки нет. Отложено на Фазу 5.

## Открытые вопросы

- **once-reminder завершение**: достаточно ли что DailyReset пропускает once, или нужно поле `completed` в Reminder? Отложено на Фазу 5.