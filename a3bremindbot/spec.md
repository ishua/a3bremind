# a3bRemindBot — Specification v0.2

## Концепция

Телеграм-бот для напоминаний. Не привязан к конкретной предметной области — универсальный инструмент для любых повторяющихся задач с подтверждением выполнения.

---

## Модель данных

### User
| Поле | Тип | Описание |
|---|---|---|
| `id` | uuid | Уникальный идентификатор |
| `telegram_id` | int | Telegram user ID |
| `timezone` | string | Часовой пояс, например `Europe/Berlin` |
| `paused` | bool | Все напоминания приостановлены |
| `last_reset_at` | datetime (UTC) / null | Время последнего сброса дня. Защита от двойного срабатывания |

Timezone задаётся один раз при `/start`, меняется через `/settings timezone`.
Telegram не передаёт timezone пользователя — спрашиваем явно.
Все времена хранятся в UTC, отображаются в timezone пользователя.

---

### Reminder (шаблон)
Описывает что и когда напоминать. Никогда не мутирует после создания.

| Поле | Тип | Описание |
|---|---|---|
| `id` | uuid | Уникальный идентификатор |
| `user_id` | uuid | Владелец |
| `label` | string | Текст напоминания |
| `times[]` | HH:MM[] | Одно или несколько времён. Один элемент = одиночный, несколько = серия |
| `min_gap` | minutes / null | Минимальный интервал между приёмами. Актуален только если `times.length > 1` |
| `repeat` | `daily` / `once` | Режим повторения |
| `created_at` | datetime (UTC) | Дата создания. Для `once` — Instance создаётся только в этот день |

**Примеры:**
```
{ label: "Капли",     times: [07:00, 11:00, 15:00, 18:00, 21:00], min_gap: 180, repeat: daily }
{ label: "Таблетка",  times: [07:00],                              min_gap: null, repeat: daily }
{ label: "Отжимания", times: [09:00],                              min_gap: null, repeat: once  }
```

---

### ReminderInstance (указатель)
Создаётся в момент когда пора напомнить. Хранит факт исполнения и все отправленные сообщения.

| Поле | Тип | Описание |
|---|---|---|
| `id` | uuid | Уникальный идентификатор |
| `reminder_id` | uuid | Ссылка на Reminder |
| `time_index` | int | Индекс в `times[]` — какое по счёту время в серии |
| `scheduled_at` | datetime (UTC) | Плановое время (после рескедулера может отличаться от исходного) |
| `done_at` | datetime / null | Фактическое время выполнения |
| `status` | `pending` / `done` / `missed` / `skipped` | Текущий статус |
| `message_ids[]` | int[] | Telegram message_id всех отправленных сообщений (первое + повторы) |

**Жизненный цикл:**

1. Утром (или при создании `once`) бот создаёт Instance только для первого времени каждого Reminder
2. Отправляет сообщение → добавляет `message_id` в `message_ids[]`
3. Нет ответа → повторяет через N минут → добавляет новый `message_id`
4. После K повторов без ответа → статус `missed`
5. Получен `done` → статус `done`, записывается `done_at`
6. Instance закрыт → проверяет есть ли следующий индекс в `times[]`
   - Есть → создаёт следующий Instance (с рескедулером если нужно)
   - Нет → цепочка завершена

**Сброс в полночь:**
Все `daily` Reminder создают новую цепочку Instances с исходными временами из `times[]`.

---

## Подтверждение выполнения (`done`)

```
done           — выполнено сейчас
done 09:15     — выполнено в указанное время
ok             — синоним done
+              — синоним done
```

**Привязка к Instance:**
- Reply на сообщение бота → берём `reply_to_message_id` → находим Instance по `message_ids[]` → точный контекст
- Без reply → fallback: последний активный (`pending`) Instance пользователя

---

## Рескедулер

**Условие:** `done_at` отличается от `scheduled_at` следующего Instance, и у Reminder задан `min_gap`.

**Алгоритм:**
1. Взять `done_at` закрытого Instance как точку отсчёта
2. Для каждого следующего времени в серии: `new_scheduled_at = предыдущий done_at (или scheduled_at) + min_gap`
3. Если получившееся время ≤ исходному из `times[]` — оставить исходное (не двигаем назад)
4. Если последний Instance уходит за 23:59 — предупредить, предложить `skip`

**Пример:**
```
Исходное:  07:00 → 11:00 → 15:00 → 18:00 → 21:00  (min_gap: 3h)
done в 09:00:
Новое:     [09:00] → 12:00 → 15:00 → 18:00 → 21:00
                       ↑       ↑ не сдвигаем: 12+3=15, совпадает с исходным
```

Рескедулер работает только внутри одного Reminder. Другие Reminder не затрагивает.

---

## Команды

### Онбординг
```
/start             — приветствие, запрос timezone
/settings timezone Europe/Berlin  — изменить timezone
```

### Создание расписания
```
/add "Капли" daily gap:3h  07:00 11:00 15:00 18:00 21:00
/add "Таблетка" daily  07:00
/add "Отжимания" once  09:00
```

### Просмотр
```
/schedule          — расписание на сегодня с текущим статусом
/list              — все Reminder (шаблоны)
```

### Управление
```
/skip              — пропустить текущий активный Instance
/snooze 30         — напомнить через 30 минут
/pause             — приостановить все напоминания (user.paused = true)
/resume            — возобновить (user.paused = false)
/delete <id>       — удалить Reminder
```

---

## Corner Cases

### 1. done в прошлом
`done 06:30` в 11:00 → бот запрашивает подтверждение:
> "Записать выполнение в 06:30?"

### 2. done без reply и без активного Instance
Пользователь написал `done` вне контекста → бот сообщает что нет активных напоминаний.

### 3. Несколько пропущенных Instance
Накопилось 2+ missed → бот сообщает суммарно, не заваливает отдельными сообщениями.

### 4. Одинаковое время у разных Reminder
`07:00 Таблетка` и `07:00 Капли` → бот отправляет два отдельных сообщения с небольшой задержкой (5 сек). Каждое подтверждается независимо.

### 5. `once` выполнен
После `done` → Reminder помечается как завершённый, больше не создаёт Instances.

### 6. `once` не выполнен (missed)
Instance остаётся в истории как `missed`. Reminder удаляется — на следующий день не восстанавливается.

### 7. Сброс дня
В 03:00 (по timezone пользователя) — смещение от полуночи чтобы не мешать поздним сессиям. Все `daily` Reminder запускают новую цепочку Instances с исходными временами. `last_reset_at` на User обновляется чтобы не сработать дважды.

### 8. Пауза во время активного Instance
`/pause` пока Instance в статусе `pending` → бот перестаёт слать повторы. Instance остаётся `pending`. После `/resume` — повторы возобновляются с того же Instance.

---

## Уведомления

| Событие | Сообщение |
|---|---|
| Напоминание | `⏰ 07:00 · Капли` |
| Повтор | `🔔 Напоминаю: Капли (попытка 2/3)` |
| Пропущено | `❌ Капли 07:00 — не подтверждено` |
| Подтверждено | `✅ Капли — записано в 07:00` |
| Рескедул | `📅 Новое расписание: 12:00 · 15:00 · 18:00 · 21:00` |
| Предупреждение | `⚠️ Последний приём выходит за полночь — пропустить?` |

---

## Настройки

| Параметр | По умолчанию | Описание |
|---|---|---|
| `repeat_interval` | 15 мин | Интервал повтора напоминания |
| `repeat_count` | 3 | Количество повторов до `missed` |
| `timezone` | спрашивается при /start | Часовой пояс пользователя |

---

## Технологии

| Роль | Пакет |
|---|---|
| Язык | Go |
| Telegram API | `github.com/go-telegram-bot-api/telegram-bot-api` |
| База данных | SQLite via `modernc.org/sqlite` (pure Go, без cgo) |
| SQL | `database/sql` (стандартная библиотека) |
| Планировщик | `time.Ticker` (стандартная библиотека) |

Никаких ORM — чистый SQL. Никаких внешних планировщиков — один ticker раз в секунду.

---

## Архитектура

### Структура проекта

```
/cmd
  main.go                 — точка входа, wire-up зависимостей

/internal
  /store                  — слой данных, только SQL, без бизнес-логики
    db.go                 — инициализация БД, миграции
    user.go
    reminder.go
    instance.go

  /domain                 — бизнес-логика, не знает о Telegram
    scheduler.go          — ticker 1/сек: проверка instances + сброс дня
    reminder.go           — создание цепочки instances, рескедулер
    user.go               — pause/resume, смена timezone

  /bot                    — интеграция с Telegram
    handler.go            — роутинг входящих команд и сообщений
    commands.go           — /start /add /schedule /pause /delete etc
    messages.go           — форматирование исходящих сообщений
```

`store` не знает о `domain`. `domain` использует `store`. `bot` вызывает `domain`. Зависимости строго в одну сторону.

### Слой данных (store)

**user.go**
- `Create(telegramID int) (User, error)`
- `GetByTelegramID(telegramID int) (User, error)`
- `SetTimezone(userID uuid, tz string) error`
- `SetPaused(userID uuid, paused bool) error`
- `SetLastResetAt(userID uuid, t time.Time) error`

**reminder.go**
- `Create(r Reminder) (Reminder, error)`
- `GetAll(userID uuid) ([]Reminder, error)`
- `GetByID(id uuid) (Reminder, error)`
- `Update(r Reminder) error`
- `Delete(id uuid) error`

**instance.go**
- `Create(i ReminderInstance) (ReminderInstance, error)`
- `GetPending(now time.Time) ([]ReminderInstance, error)` — `scheduled_at <= now AND status = pending`
- `GetActiveByUser(userID uuid) ([]ReminderInstance, error)` — fallback для done без reply
- `GetByMessageID(messageID int) (ReminderInstance, error)` — привязка reply
- `GetLastByReminder(reminderID uuid, timeIndex int) (ReminderInstance, error)` — для рескедулера
- `SetStatus(id uuid, status string) error`
- `SetDoneAt(id uuid, t time.Time) error`
- `AddMessageID(id uuid, messageID int) error`

### Бизнес-логика (domain)

**scheduler.go** — главный loop, `time.Ticker` каждую секунду:
1. Загрузить все pending instances где `scheduled_at <= now`
2. Для каждого — отправить напоминание (или повтор), добавить `message_id`
3. Проверить нужен ли сброс дня: для каждого юзера если текущее время = 03:00 по его timezone и `last_reset_at` был вчера → запустить `DailyReset`

**reminder.go**
- `DailyReset(userID uuid)` — создать первый Instance для каждого `daily` Reminder
- `NextInstance(instance ReminderInstance)` — после закрытия Instance: взять следующий `time_index`, применить рескедулер, создать новый Instance
- `Reschedule(reminder Reminder, doneAt time.Time, fromIndex int) []time.Time` — пересчёт времён серии

### Интеграция с Telegram (bot)

**handler.go** — роутинг:
- Команды (`/start`, `/add`, ...) → `commands.go`
- Текстовые сообщения (`done`, `ok`, `+`, `done HH:MM`) → парсинг + `domain`
- Reply → извлечь `reply_to_message_id` → найти Instance → подтвердить

---

## План разработки

Реализация снизу вверх: `store → domain → bot`. Каждая фаза даёт что-то реально работающее.

### Фаза 1 · store
- Схема БД, миграции
- CRUD: User, Reminder, ReminderInstance
- Юнит тесты с реальным SQLite in-memory

**Результат:** данные сохраняются и читаются корректно.

### Фаза 2 · domain · базовый сценарий
- `DailyReset` — создаёт первый Instance для каждого `daily` Reminder
- scheduler loop (`time.Ticker` 1/сек) — находит pending instances
- `NextInstance` — после закрытия создаёт следующий в цепочке
- Рескедулер пока не реализуем

**Результат:** цепочка Instance живёт и переключается сама по себе.

### Фаза 3 · bot · минимальный живой бот
- `/start` + запрос timezone
- `/add` простой (одно время, без серии)
- `done` — reply и fallback на последний активный Instance

**Результат:** полностью рабочий бот для одного напоминания. Можно реально пользоваться.

### Фаза 4 · серии и рескедулер
- `/add` с несколькими временами и `gap`
- `Reschedule` логика в domain
- Предупреждение если последний Instance выходит за день

**Результат:** полный сценарий с серией напоминаний и автоматическим пересчётом.

### Фаза 5 · полный бот
- `/schedule`, `/list`, `/skip`, `/snooze`, `/pause`, `/delete`
- `done HH:MM` с подтверждением если время в прошлом
- Оставшиеся corner cases

**Результат:** полный бот согласно спецификации.
