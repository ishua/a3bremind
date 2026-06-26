# Команды `/list instances` и `/done <uuid>`

> Расширение бота командами для просмотра Instance по UUID и отметки выполнения любого Instance (включая `missed`) с пересчётом цепочки.

Без задачи (обсуждение в чате).

## Контекст

Пользователь хочет иметь возможность отметить Instance как выполненный постфактум — даже если Instance уже в статусе `missed` (пропущен). Для этого нужна точная адресация по UUID.

Текущее состояние:
- `/list` показывает шаблоны Reminder (только название, времена, gap). UUID выводится в строке `🆔 <uuid>`, но команды с ним не предлагаются.
- `/done` работает только через reply (по `message_id`) или fallback на последний `pending` Instance. Если все Instance `missed` — `done` не принимается.
- `NextInstance` создаёт новый Instance только если следующего ещё нет. Если он уже существует (missed/pending) — возникнет дубликат.

## Цель

Добавить три изменения:
1. Изменить формат `/list` — вместо строки `🆔 <uuid>` показывать готовые к копированию команды `/delete <uuid>` и `/list instances <uuid>`.
2. Добавить команду `/list instances <reminder_id>` — показывает все Instance указанного Reminder за сегодня с UUID, статусом, временем и готовой командой `/done <uuid> <time>`.
3. Добавить команду `/done <uuid> [HH:MM]` — отмечает Instance (любого статуса) как выполненный, с пересчётом последующих Instance в цепочке.

## Фаза 1: Store — метод удаления последующих Instance

- [x] Добавить `DeleteInstancesAfterIndex(db Querier, reminderID string, fromIndex int) error` в `internal/store/instance.go`
  - SQL: `DELETE FROM reminder_instances WHERE reminder_id = ? AND time_index > ?`
  - Нужен для пересоздания цепочки в `/done`

Зачем отдельная фаза: новый store-метод — независимый кирпичик, можно сразу протестировать.

**Заметка:** Добавлен метод + 4 теста. Заодно исправлен предсуществующий баг в `db.go`: индекс `idx_instances_user_for_date` ссылался на несуществующую колонку `user_id` в `reminder_instances` — переименован в `idx_instances_reminder_for_date` с колонками `(reminder_id, for_date)`.

**Результат:** новый метод в store, покрытый тестами.

## Фаза 2: Изменение `/list` и новый `/list instances`

- [x] В `internal/bot/list.go`:
  - Заменить строку `  🆔 %s\n` на две строки с готовыми командами в backtick-блоке:
    ```
      `/delete e034ce50-7954-4e79-92b4-fab8e0cdc993`
      `/list instances e034ce50-7954-4e79-92b4-fab8e0cdc993`
    ```
  - ID показывается только внутри команд, отдельной строки с 🆔 больше нет.

- [x] Создать `internal/bot/list_instances.go` с хендлером `handleListInstances(update)`:
  - Парсит `<reminder_id>` из аргументов команды `/list instances <id>`
  - Загружает Reminder (с проверкой принадлежности пользователю)
  - Загружает все Instance для этого Reminder через `GetReminderInstancesByReminder`
  - Группирует/фильтрует по сегодняшнему дню (в timezone пользователя)
  - Форматирует вывод в стиле:
    ```
    💊 Капли
    ✅ 07:00
      - `/done e034ce50... 07:00`
    ❌ 11:00 — f123ce50...
      - `/done f123ce50... 11:00`
    ⏳ 14:00 — a456ce50...
      - `/done a456ce50... 14:00`
    ```

- [x] В `internal/bot/handler.go` зарегистрировать `/list instances`:
  - Строка: `case strings.HasPrefix(text, "/list instances"):` — **до** общего `/list`
  - Приоритет: более длинный префикс раньше

Почему вместе: оба изменения касаются отображения данных пользователю, логически связаны.

**Результат:** пользователь видит команды в `/list` и может зайти в детали Reminder с готовыми `/done` командами для каждого Instance.

## Фаза 3: Команда `/done <uuid> [HH:MM]`

- [x] Создать `internal/bot/donebyuuid.go` с хендлером `handleDoneByUUID(update)`:
  - Парсинг: `/done <uuid>` или `/done <uuid> HH:MM`
  - Если UUID невалидный (не 36 символов) — сообщить об ошибке
  - Если времени нет — `done_at = now` в timezone пользователя
  - Если время есть — `done_at = today + HH:MM` (как в `donewithtime.go`)
  - **Без подтверждения** (даже если время в прошлом) — пользователь подтвердил

  - **Алгоритм отметки (в транзакции):**
    1. Загрузить Instance по UUID (с проверкой, что Instance принадлежит пользователю через Reminder)
    2. Загрузить Reminder
    3. Удалить все Instance с `time_index > current` для этого Reminder (`DeleteInstancesAfterIndex`)
    4. Установить Instance: `status = done`, `done_at = указанное время` (через `SetStatusWithDoneAt`)
    5. Перезагрузить Instance (чтобы получить `done_at`)
    6. Вызвать `NextInstance` — он создаст следующий Instance с нуля, применив рескедулер если нужно
    7. Commit

  - Форматирование ответа:
    - `✅ Капли — записано в 08:00`
    - Если был рескедул — `📅 Новое расписание: 11:00 · 14:00 ...`
    - Если последний выходит за полночь — `⚠️ ...`

  - Если Instance уже `done` — сообщить: `Это напоминание уже выполнено`

- [x] В `internal/bot/handler.go` зарегистрировать `/done` как команду:
  - Нужно аккуратно: `/done` сейчас не команда (нет `IsCommand()` — это plain text). Но `/done <uuid>` будет командой (начинается с `/`).
  - Вариант: добавить в `handleCommand` перед дефолтом:
    ```go
    case strings.HasPrefix(text, "/done"):
        h.handleDoneByUUID(update)
    ```
  - Это не сломает существующий `done` (plain text, без `/`) — он обрабатывается в `HandleUpdate` через `lower == "done"`.

Почему отдельная фаза: самая сложная часть, меняет логику цепочки, требует осторожности с транзакциями.

**Результат:** пользователь может отметить любой Instance (даже `missed`) по UUID, цепочка пересоздаётся с учётом рескедулера.

## Фаза 4: Тесты

- [x] В `internal/bot/bot_test.go`:
  - Тест на `/list` — проверить что в выводе есть `/delete` и `/list instances`
  - Тест на `/list instances <id>` — проверить форматирование
  - Тест на `/done <uuid> 08:00` — missed Instance → done + пересоздание цепочки
  - Тест на `/done <uuid>` (без времени) — done at now
  - Тест на `/done <uuid>` с уже done Instance — сообщение об ошибке
  - Тест на невалидный UUID

## Решения и договорённости

- **Формат `/list`**: строка `🆔 <uuid>` заменяется на две строки с backtick-командами `/delete <id>` и `/list instances <id>`. UUID не дублируется отдельно.
- **Формат `/list instances`**: заголовок с label, затем по одной строке на Instance: `иконка время — частичный UUID`, затем строка с готовой `/done` командой в backtick.
- **`/done <uuid>` без пересчёта**: при отметке Instance удаляются все последующие Instance для этого Reminder, затем `NextInstance` создаёт их заново с учётом рескедулера.
- **Без подтверждения времени**: `/done <uuid> 07:00` записывается сразу без двухшагового подтверждения, даже если время в прошлом.
- **`/done <uuid>` без времени**: `done_at = now` в timezone пользователя.
- **`/done` (без `/`, plain text)**: сохраняет существующее поведение (reply/fallback).
- **Полный UUID**: в командах используется полный 36-символьный UUID — копируется тапом на телефоне.

## Открытые вопросы

> Вопросов нет — все решения согласованы с пользователем.