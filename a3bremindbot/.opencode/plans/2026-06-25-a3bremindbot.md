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

# a3bRemindBot · Фаза 4: серии и рескедулер · Фаза 5: полный бот

> Фаза 4: `/add` с несколькими временами и `gap`, Reschedule в domain, предупреждение о выходе за полночь и уведомление о рескедуле. Фаза 5: `/schedule`, `/list`, `/skip`, `/snooze`, `/pause`, `/delete`, `done HH:MM`, оставшиеся corner cases.

Без задачи — продолжение плана разработки.

## Контекст

Фазы 1–3 полностью реализованы и протестированы:
- **store**: SQLite-схема, CRUD для User/Reminder/ReminderInstance, MessageIDEntry, ~45 тестов
- **domain**: Scheduler (ticker 1/сек), ProcessPending (первые уведомления, повторы, missed), DailyReset, NextInstance (цепочка без рескедула), ~12 тестов
- **bot**: `/start`, `/settings timezone`, `/add` (одно время), `done`/`ok`/`+` с reply и fallback, ~15 тестов

Текущие ограничения, которые снимают Фаза 4 и 5:
- `/add` принимает только одно время — нет серий
- `ParseAddCommand` возвращает `(label, repeat, timeStr string, err error)` — одиночное время
- `Reminder.Times`, `MinGap` уже есть в модели и store, но не используются в domain
- `NextInstance` всегда ставит исходное время из `times[]` — нет рескедула при `min_gap`
- Нет `/schedule`, `/list`, `/skip`, `/snooze`, `/pause`, `/delete`
- `done` всегда записывает `time.Now()` — нет `done HH:MM`
- `once` reminder при `missed` не удаляется
- Нет уведомления о рескедуле и предупреждения о выходе за полночь

## Цель

Полностью рабочий бот согласно спецификации v0.2:
- Поддержка серий напоминаний с автоматическим пересчётом (`Reschedule`) при `min_gap`
- Все команды управления: просмотр, пропуск, отсрочка, пауза, удаление
- Корректная обработка `done HH:MM` с подтверждением времени в прошлом
- Все corner cases из спецификации

---

## Фаза 4: серии и рескедулер

### 4.1 store — новые методы

- [ ] `GetInstancesByUserAndDay(db *sql.DB, userID string, date time.Time, loc *time.Location) ([]ReminderInstance, error)`:
  - Принимает `date` и `loc` (timezone пользователя)
  - Вычисляет начало и конец дня в timezone пользователя, конвертирует в UTC для SQL-запроса
  - `scheduled_at BETWEEN startOfDayUTC AND endOfDayUTC`
  - Использовать timezone пользователя, не UTC-день — иначе для UTC+3 "сегодня" начинается в 21:00 UTC предыдущего дня
- [ ] `SetInstanceScheduledAt(db *sql.DB, id string, t time.Time) error` — обновляет `scheduled_at` и `updated_at` у Instance. Реализуется в Фазе 4, используется также в Фазе 5 (`/snooze`).

### 4.2 Расширить `parseAddCommand` для серии с gap

- [ ] Новая сигнатура: `parseAddCommand(text string) (label, repeat string, times []string, minGap *int, err error)`
  - Заменяет старую `(label, repeat, timeStr string, err error)`
  - `times` — один или несколько `HH:MM`, `minGap` — указатель на int (минуты) или nil

- [ ] Новый формат команды:
  ```
  /add "Label" daily gap:3h 07:00 11:00 15:00 18:00 21:00   — серия с gap
  /add "Label" daily 07:00 11:00 15:00                        — серия без gap
  /add "Label" once 09:00                                     — одиночное (как сейчас)
  ```
- [ ] Парсинг `gap:3h` / `gap:30m`: извлечь число + единицу (h → ×60, m → ×1) → `*int` минуты. `gap:` опциональный, если отсутствует → `minGap = nil`
- [ ] Все токены `HH:MM` валидируются через `time.Parse("15:04", v)`, ошибка если ни одного
- [ ] В `handleAdd`: создать `Reminder` с `Times = times`, `MinGap = minGap`; первый Instance для `Times[0]` — как сейчас

- [ ] Тесты:
  - `TestParseAddCommand_Series` — `/add "Капли" daily 07:00 11:00 15:00` → `times=["07:00","11:00","15:00"]`, minGap=nil
  - `TestParseAddCommand_WithGap` — `/add "Капли" daily gap:3h 07:00 11:00 15:00` → minGap=180
  - `TestParseAddCommand_GapMinutes` — `gap:30m` → minGap=30
  - `TestParseAddCommand_Single` — одиночное время — старый формат не сломан
  - `TestParseAddCommand_InvalidGap` — `gap:xyz` → ошибка
  - `TestParseAddCommand_InvalidTime` — `25:00` → ошибка
  - `TestParseAddCommand_NoTimes` — нет времён → ошибка
  - `TestHandleAdd_Series` — интеграционный: `/add` с серией → Reminder с Times, первый Instance создан

### 4.3 Reschedule в domain

- [ ] Новая функция `Reschedule(reminder store.Reminder, doneAt time.Time, fromIndex int, loc *time.Location) (adjustedTimes []time.Time, warning string)`:
  - Если `reminder.MinGap == nil` → вернуть исходные времена из `reminder.Times[fromIndex+1:]` без изменений (конвертировать в time.Time для текущего дня)
  - Начинать с `doneAt` как точки отсчёта
  - Для каждого `reminder.Times[i]` где `i > fromIndex`:
    - `originalTime = сегодня + Times[i]` в `loc`
    - `earliestNext = предыдущее_adjusted + MinGap`
    - `adjustedTimes[i] = max(originalTime, earliestNext)`
  - Если последнее `adjustedTime` выходит за 23:59 в `loc` → `warning` непустой

- [ ] Модифицировать `NextInstance`:
  - Новая сигнатура: `func NextInstance(db *sql.DB, inst store.ReminderInstance) (warning string, err error)`
  - После создания нового Instance:
    - Загрузить User для получения timezone (`store.GetUserByID`)
    - Если `reminder.MinGap != nil` и `inst.DoneAt != nil` → вызвать `Reschedule(reminder, *inst.DoneAt, inst.TimeIndex, loc)`
    - Обновить `ScheduledAt` созданного Instance через `store.SetInstanceScheduledAt`
    - Если Reschedule вернул warning → вернуть его из NextInstance
  - Обновить все существующие вызовы NextInstance в bot и тестах под новую сигнатуру (warning пока можно игнорировать через `_`)

- [ ] **Новый store-метод** `SetInstanceScheduledAt` — уже описан в 4.1

- [ ] Тесты:
  - `TestReschedule_ShiftsForward` — done в 09:00, min_gap=3h, исходные 07:00/11:00/15:00 → [12:00, 15:00] (11:00 сдвинуто до 12:00, 12+3=15 совпадает)
  - `TestReschedule_NoShiftNeeded` — done в 06:00, min_gap=2h, исходные 09:00/12:00 → [09:00, 12:00] (не сдвигаем: 6+2=8 < 9)
  - `TestReschedule_NilMinGap` — без MinGap → возвращает исходные времена
  - `TestReschedule_LastPastMidnight` — последнее время после рескеда > 23:59 → warning непустой
  - `TestNextInstance_WithReschedule` — после done с MinGap → новый Instance имеет скорректированное `scheduled_at`
  - `TestNextInstance_RescheduleWarning` — warning возвращается при выходе за полночь
  - `TestNextInstance_SignatureUpdate` — убедиться что старые тесты обновлены под новую сигнатуру

### 4.4 Предупреждение и уведомление в bot

- [ ] Обновить вызов `domain.NextInstance` в `bot/done.go`:
  - Обрабатывать `warning` — если непустой, отправить пользователю: `"⚠️ Последний приём выходит за полночь — пропустить?"`

- [ ] Уведомление о рескедуле:
  - После NextInstance проверить: если `inst.DoneAt != nil` и у Reminder есть `MinGap` → рескедул был применён
  - Перечитать все pending Instance цепочки (через `GetInstancesByUserAndDay`) и сформировать список актуальных времён
  - Отправить: `"📅 Новое расписание: 12:00 · 15:00 · 18:00 · 21:00"` — только если хотя бы одно время сдвинулось относительно исходного `times[]`

- [ ] Тесты:
  - `TestHandleDone_RescheduleNotification` — done с MinGap → бот отправил `📅` сообщение
  - `TestHandleDone_RescheduleWarning` — warning при выходе за полночь → бот отправил `⚠️`
  - `TestHandleDone_NoRescheduleNotification` — без MinGap → `📅` не отправляется

---

## Фаза 5: полный бот

### 5.1 store — новые методы

- [ ] `GetInstancesByUserAndDay` — реализован в Фазе 4, здесь только используется
- [ ] `SetInstanceScheduledAt` — реализован в Фазе 4, здесь только используется
- [ ] `GetReminderInstancesByReminder(db *sql.DB, reminderID string) ([]ReminderInstance, error)` — все Instance для Reminder, для каскадного удаления
- [ ] `DeleteReminderInstances(db *sql.DB, reminderID string) error` — удалить все Instance для Reminder
- [ ] `SetStatusWithDoneAt(db *sql.DB, id string, status string, doneAt time.Time) error` — как `SetStatus("done")` но с конкретным `doneAt`. Только для статуса `"done"`.

### 5.2 Команда `/schedule` и `/schedule tomorrow`

- [ ] В `handler.go` добавить роутинг `/schedule` → `handleSchedule`
- [ ] `handleSchedule`:
  - Получить пользователя, проверить timezone
  - Если аргумент `tomorrow` → дата = завтра в timezone пользователя, иначе сегодня
  - `GetInstancesByUserAndDay(db, user.ID, date, loc)`
  - Сгруппировать по `ReminderID`, для каждого показать label + времена со статусами
  - Формат:
    ```
    📅 Расписание на сегодня:

    Таблетка
    ✅ 07:00
    ⏳ 12:00

    Капли
    ⏳ 09:00
    ⏳ 13:00
    ⏳ 17:00
    ```
  - Иконки: `⏳` pending, `✅` done, `❌` missed, `⏭️` skipped

- [ ] Тесты:
  - `TestHandleSchedule_Today` — `/schedule` → Instance за сегодня
  - `TestHandleSchedule_Tomorrow` — `/schedule tomorrow` → Instance за завтра
  - `TestHandleSchedule_Empty` — нет Instance → "Нет напоминаний на сегодня"

### 5.3 Команда `/list`

- [ ] В `handler.go` добавить роутинг `/list` → `handleList`
- [ ] `handleList`:
  - `store.GetAll(userID)` → все Reminder шаблоны
  - Формат:
    ```
    📋 Все напоминания:

    Капли · daily
    🆔 a1b2c3d4-e5f6-...
    ⏰ 07:00 11:00 15:00 18:00 21:00 (gap: 3ч)

    Отжимания · once
    🆔 b2c3d4e5-...
    ⏰ 09:00
    ```

- [ ] Тесты:
  - `TestHandleList_WithReminders` — показывает все Reminder с форматированием
  - `TestHandleList_Empty` — нет Reminder → "Нет настроенных напоминаний"

### 5.4 Команда `/skip`

- [ ] В `handler.go` добавить роутинг `/skip` → `handleSkip`
- [ ] `handleSkip`:
  - Получить пользователя
  - `GetActiveByUser` → последний pending Instance
  - Нет активных → "Нет активных напоминаний"
  - `store.SetStatus(db, instance.ID, "skipped")` — не проставляет `done_at` (SetStatus проверяет `status == "done"`)
  - `domain.NextInstance(db, instance)` — создаёт следующий, обрабатывает warning
  - **Важно:** для NextInstance нужен актуальный Instance — перечитать из store после SetStatus, так как `DoneAt` используется в Reschedule. При `skipped` `DoneAt` будет nil → рескедул не применяется (это правильно)
  - Ответ: `"⏭️ Label — пропущено"`

- [ ] Тесты:
  - `TestHandleSkip_Active` — skip → статус skipped, nextInstance создан с исходным временем (без рескедула)
  - `TestHandleSkip_NoActive` — нет активных → сообщение
  - `TestHandleSkip_LastIndex` — последний в цепочке → skipped, без nextInstance

### 5.5 Команда `/snooze N`

- [ ] В `handler.go` добавить роутинг `/snooze` → `handleSnooze`
- [ ] `handleSnooze`:
  - Парсинг: после `/snooze` целое число N (минуты). Валидация: N > 0 и N <= 1440 (24 часа) — ошибка если нет
  - Получить пользователя
  - `GetActiveByUser` → последний pending Instance
  - Нет активных → "Нет активных напоминаний"
  - `store.SetInstanceScheduledAt(db, instance.ID, now.Add(time.Duration(N)*time.Minute))`
  - Instance остаётся `pending` — scheduler не обработает до нового времени
  - Ответ: `"🔇 Label — напомню через N минут"`

- [ ] Тесты:
  - `TestHandleSnooze` — `/snooze 30` → `scheduled_at` сдвинут на 30 минут
  - `TestHandleSnooze_Invalid` — `/snooze abc` → ошибка парсинга
  - `TestHandleSnooze_OutOfRange` — `/snooze 0` и `/snooze 1441` → ошибка валидации
  - `TestHandleSnooze_NoActive` — нет активных → сообщение

### 5.6 Команды `/pause` и `/resume`

- [ ] В `handler.go` добавить роутинг `/pause` и `/resume`
- [ ] `handlePause`: `store.SetPaused(db, user.ID, true)` → `"⏸ Все напоминания приостановлены"`
- [ ] `handleResume`: `store.SetPaused(db, user.ID, false)` → `"▶️ Напоминания возобновлены"`

- [ ] Тесты:
  - `TestHandlePause` — paused=true в store
  - `TestHandleResume` — paused=false в store
  - `TestHandleDone_WhilePaused` — done работает при paused=true (paused не блокирует команды)

### 5.7 Команда `/delete <id>`

- [ ] В `handler.go` добавить роутинг `/delete` → `handleDelete`
- [ ] `handleDelete`:
  - Парсинг: после `/delete` — полный UUID
  - `store.GetByID(db, id)` — проверить что Reminder существует и `reminder.UserID == user.ID`
  - Reminder не найден или чужой → "Напоминание не найдено"
  - Каскадное удаление: `store.DeleteReminderInstances(db, id)` + `store.Delete(db, id)`
  - **Безопасность race condition:** SQLite работает в режиме serial access для одного соединения — удаление и ticker не пересекутся. Приемлемо для однопользовательского бота.
  - Ответ: `"🗑 Напоминание «Label» удалено"`

- [ ] Тесты:
  - `TestHandleDelete` — Reminder + все Instance удалены из store
  - `TestHandleDelete_NotFound` — неверный UUID → ошибка
  - `TestHandleDelete_WrongUser` — чужой Reminder → ошибка

### 5.8 `done HH:MM` с подтверждением

- [ ] В `handler.go` расширить роутинг: если текст начинается с `done `/`ok `/`+ ` и содержит `HH:MM` → `handleDoneWithTime`. Проверять до `handleDone` чтобы не перехватить.

- [ ] `handleDoneWithTime`:
  - Парсинг `HH:MM` из текста
  - Получить пользователя, timezone
  - `doneAt = сегодня + HH:MM` в timezone пользователя
  - Если `doneAt` в будущем → "Указанное время в будущем. Используй `done` без времени."
  - Если `doneAt` в прошлом:
    - Сохранить в `pendingConfirm[chatID] = {InstanceID, DoneAt}` (in-memory `sync.Map`)
    - Отправить: `"Записать выполнение в HH:MM? Отправь + для подтверждения."`

- [ ] Обработка подтверждения в `HandleUpdate`:
  - Если текст `+`/`yes`/`y` (после TrimSpace/ToLower) И в `pendingConfirm` есть запись для chatID:
    - Загрузить Instance, проверить статус
    - `store.SetStatusWithDoneAt(db, instanceID, "done", doneAt)`
    - Перечитать Instance из store → `domain.NextInstance(db, updatedInst)` (DoneAt уже проставлен)
    - Удалить из `pendingConfirm[chatID]`
    - Ответ: `"✅ Label — записано в HH:MM"`
  - Очистка: pending confirm удаляется через 5 минут горутиной-таймером или при следующем `done HH:MM`

- [ ] **Важно — перечитывать Instance перед NextInstance:** после `SetStatusWithDoneAt` обязательно перечитать Instance из store, иначе `inst.DoneAt` будет nil и рескедул не применится.

- [ ] **Новый store-метод** `SetStatusWithDoneAt` — отдельный SQL:
  ```sql
  UPDATE reminder_instances SET status='done', done_at=?, updated_at=? WHERE id=?
  ```

- [ ] Тесты:
  - `TestHandleDone_WithTime_Past` — `done 06:30` в 11:00 → запрос подтверждения, pending confirm создан
  - `TestHandleDone_TimeConfirm_Yes` — `+` после confirm → `done_at = 06:30`, NextInstance вызван
  - `TestHandleDone_WithTime_Future` — `done 14:00` в 11:00 → ошибка
  - `TestHandleDone_WithTime_NoConfirm` — `+` без pending confirm → обычный `done` (не handleDoneWithTime)

### 5.9 Corner cases

- [ ] **`once` при `missed`** (corner case 6):
  - В `processPending` после `SetStatus("missed")`: загрузить Reminder, если `repeat == "once"`:
    - Для `once` всегда `time_index == 0` и это последний — удалить безусловно
    - `store.DeleteReminderInstances(db, reminder.ID)` + `store.Delete(db, reminder.ID)`
  - Тихое удаление — пользователю сообщение не отправляется

- [ ] **`done` при уже выполненном через reply** — уже обработано в `handleDone` (проверка статуса). Добавить явный тест `TestHandleDone_AlreadyDone_Reply`.

- [ ] **paused не блокирует команды** — уже работает. Тест `TestHandleDone_WhilePaused` покрывает в 5.6.

- [ ] Тесты:
  - `TestProcessPending_OnceMissedDeleted` — once reminder после missed → удалён из store
  - `TestHandleDone_AlreadyDone_Reply` — reply на done instance → "уже выполнено"

### 5.10 Роутинг и финальная сборка

- [ ] Обновить `handler.go`: все новые команды в `handleCommand` switch
- [ ] Порядок проверки текста в `HandleUpdate`:
  1. `IsCommand()` → команды
  2. текст начинается с `done `/`ok `/`+ ` + содержит `HH:MM` → `handleDoneWithTime`
  3. текст `done`/`ok`/`+` → `handleDone`
  4. текст `+`/`yes`/`y` + есть `pendingConfirm[chatID]` → подтверждение `done HH:MM`
  5. остальное → игнор
- [ ] Обновить существующие тесты под новую сигнатуру `NextInstance` если ещё не обновлены
- [ ] `cmd/main.go` — изменений не требует

### 5.11 Тесты — общие

- [ ] Прогнать все тесты фаз 1–3 после изменений Фазы 4–5 — убедиться что ничего не сломано
- [ ] Итоговое покрытие: store ~50 тестов, domain ~20 тестов, bot ~35 тестов

---

## Решения и договорённости

- **Формат gap в /add**: `gap:3h` (часы) или `gap:30m` (минуты)
- **`parseAddCommand` новая сигнатура**: `(label, repeat string, times []string, minGap *int, err error)`
- **`SetInstanceScheduledAt`**: реализуется в Фазе 4, используется в Фазах 4 и 5
- **`GetInstancesByUserAndDay`**: принимает `loc *time.Location`, вычисляет UTC-диапазон по timezone пользователя — не UTC-день
- **snooze валидация**: N > 0 и N <= 1440 (24 часа)
- **snooze механизм**: сдвиг `scheduled_at` текущего pending Instance через `SetInstanceScheduledAt`
- **`/delete` ID**: полный UUID
- **cascade delete**: `DeleteReminderInstances` + `Delete` вручную, без ON DELETE CASCADE. Безопасно при serial SQLite access.
- **`SetStatusWithDoneAt`**: отдельный store-метод только для `"done"` с конкретным `doneAt`
- **перечитывать Instance перед NextInstance**: обязательно после любого `SetStatus*` чтобы `DoneAt` был актуальным для Reschedule
- **`done HH:MM` подтверждение**: in-memory `sync.Map`, ключ — chatID. Не переживает перезапуск. Таймаут 5 минут.
- **`skipped` + NextInstance**: `DoneAt == nil` → рескедул не применяется, исходное время из `times[]`
- **`once` при missed**: `time_index` всегда 0 и всегда последний — удалять безусловно
- **paused не блокирует команды**: done, skip, snooze работают при paused=true
- **Порядок `done` в HandleUpdate**: `done HH:MM` проверяется до `done` без времени
- **`/help`**: не реализуем — краткая сводка в `/start`
- **Reschedule notification**: отправляется только если хотя бы одно время реально сдвинулось
- **NextInstance сигнатура**: `(db, inst) (warning string, err error)` — обновить все вызовы в bot и тестах



## Заметки по реализации

- `handleAdd`: `reminder.Times = times`, `reminder.MinGap = minGap`, первый Instance для `Times[0]`
- `handleSkip`: перечитать Instance после SetStatus("skipped") перед NextInstance — нужен актуальный объект (хотя DoneAt будет nil, это правильное поведение для skip)
- `handleDoneWithTime` → подтверждение: шаг 4 в HandleUpdate должен идти до шага 3 (`done`/`ok`/`+`) чтобы `+` как подтверждение обрабатывался раньше чем `+` как синоним done
- `GetInstancesByUserAndDay` для `/schedule`: группировать по `ReminderID` — загружать Reminder отдельно для получения label, или джойнить в SQL
- Горутина очистки `pendingConfirm`: `time.AfterFunc(5*time.Minute, func() { pendingConfirm.Delete(chatID) })`
