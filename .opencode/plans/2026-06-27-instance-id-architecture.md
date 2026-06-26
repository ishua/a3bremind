# Архитектура обновления инстансов по ID

> Переработать механизмы поиска инстансов для модификации: убрать json_each-поиск по message_ids и fallback на последний pending инстанс, заменить на явный ID через reply-таблицу и inline-кнопки.

Без задачи

## Контекст

Сейчас в боте три способа найти инстанс для модификации (done/snooze/skip):
1. **По reply_to_message_id** — `GetInstanceByMessageID` парсит JSON-массив `message_ids` через `json_each`. Хрупко, привязано к формату хранения.
2. **Fallback на последний pending** — `GetActiveByUser` берёт все pending инстансы пользователя и берёт последний по `scheduled_at`. Неоднозначно при нескольких активных инстансах.
3. **По UUID** — `/done <uuid>` — единственный чистый путь, уже работает правильно.

Зависимость направления: `store ← domain ← bot`. Domain-слой не знает про Telegram-разметку. Уведомления отправляются через интерфейс `Notifier` в `pending.go:75`. Миграции — `CREATE TABLE IF NOT EXISTS` в `store/db.go:migrate()`. Callback queries сейчас полностью игнорируются (`handler.go:46` — `if update.Message == nil { return }`).

Telegram callback_data лимит — 64 байта. UUID инстанса — 36 символов, с префиксом `done:UUID` = 41 байт — вписывается.

## Цель

Все модификации инстансов (done, snooze, skip) происходят только по явному instance ID:
- При reply на уведомление — через новую таблицу `instance_replies` (reply_message_id → instance_id)
- Через inline-кнопки на уведомлениях (✅ Done, ⏰ Snooze, ⏭ Skip) — callback_data содержит `action:UUID`
- `/done <uuid>` остаётся как есть
- `done`/`ok`/`+` текстом работают только при reply на сообщение бота

Удалить `GetInstanceByMessageID` (json_each) и `GetActiveByUser` (fallback). Удалить поиск по последнему pending из snooze/skip.

## Фаза 1: Таблица instance_replies и переделка reply-логики

- [ ] Добавить таблицу `instance_replies` в `store/db.go:migrate()`
  - Поля: `reply_message_id INTEGER, instance_id TEXT, created_at DATETIME`
  - Индекс: `idx_instance_replies_message_id` на `reply_message_id`
  - База инициализируется с нуля (продакшен-данных нет, старая БД удаляется), миграции не нужны — просто `CREATE TABLE IF NOT EXISTS`
- [ ] Добавить store-методы для `instance_replies`
  - `InsertInstanceReply(db, replyMessageID, instanceID) error` — INSERT
  - `GetInstanceIDByReply(db, replyMessageID) (string, error)` — SELECT by reply_message_id
- [ ] Записывать в `instance_replies` при отправке уведомления
  - В `domain/pending.go:processInstance` — после `SendMessage` (строка 75) добавить запись в `instance_replies`
  - Запись для каждого отправленного сообщения (первое + повторные)
- [ ] Переделать `handleDone` — поиск по `instance_replies` вместо json_each
  - Заменить `store.GetInstanceByMessageID(h.db, replyMsgID)` на поиск через `instance_replies` → `store.GetInstanceByID`
- [ ] Переделать `handleDoneWithTime` — аналогично
  - Заменить `store.GetInstanceByMessageID` на поиск через `instance_replies`
- [ ] Убрать fallback на последний pending инстанс
  - В `handleDone` — если нет reply, вернуть ошибку/подсказку
  - В `handleDoneWithTime` — аналогично
- [ ] Обновить тесты

Создаём прямую маппинг-таблицу `instance_replies` вместо json_each-поиска. Это первый шаг — отвязываемся от хрупкого парсинга JSON-массива в SQL. После этой фазы reply-поиск работает через чистый индексный SELECT. Fallback на «последний pending» убираем — done/ok/+ работают только при reply.

## Фаза 2: Inline-кнопки на уведомлениях

- [ ] Расширить интерфейс `Notifier` для поддержки inline-кнопок
  - Добавить параметр или новый метод, позволяющий передать `ReplyMarkup` при отправке
  - Или: изменить `SendMessage` чтобы возвращал messageID и принимал опции (кнопки)
- [ ] Сформировать inline-кнопки в domain/pending.go
  - Каждое уведомление (⏰ и 🔔) получает клавиатуру: ✅ Done | ⏰ Snooze | ⏭ Skip
  - Callback data: `done:UUID`, `snooze:UUID`, `skip:UUID`
  - Domain не должен знать про Telegram-типы — нужна абстракция (например, `Button{Text, Data}`)
- [ ] Добавить обработку callback queries в `bot/handler.go`
  - Проверка `update.CallbackQuery != nil` перед `update.Message == nil`
  - Парсинг callback data: `action:instanceID`
  - Роутинг по action: done / snooze / skip
- [ ] Реализовать callback-обработчики
  - `handleCallbackDone(callback)` — аналог handleDoneByUUID для pending инстансов
  - `handleCallbackSnooze(callback)` — snooze на дефолтный интервал (RepeatInterval)
  - `handleCallbackSkip(callback)` — пропуск инстанса
  - Проверка владельца (user ID из callback == reminder.UserID)
  - Ответ на callback: `NewCallback(callbackID, "✅ Done!")` и т.п.
- [ ] Редактирование клавиатуры после действия
  - После done/snooze/skip — убрать кнопки или заменить на статус-текст через `NewEditMessageReplyMarkup`
- [ ] Записывать в `instance_replies` messageID сообщения с inline-кнопками (уже делается в фазе 1, убедиться что работает)
- [ ] Обновить тесты

Добавляем inline-кнопки к уведомлениям. Ключевая сложность — абстракция: domain-слой не должен зависеть от Telegram-типов. Нужен интерфейс кнопок, который domain формирует, а bot-нотифаер превращает в `InlineKeyboardMarkup`. Callback-обработчики живут в bot-слое, вызывают store напрямую (как и текущие обработчики).

## Фаза 3: Очистка и унификация

- [ ] Удалить `store.GetInstanceByMessageID` и связанный SQL с `json_each`
- [ ] Удалить `store.GetActiveByUser`
- [ ] Переделать `/snooze N` — требовать instance ID (reply или inline-кнопка)
  - Без reply/ID — ошибка с подсказкой
- [ ] Переделать `/skip` — аналогично, требовать instance ID
- [ ] Удалить мёртвый код в bot-слоях (обработка fallback на GetActiveByUser)
- [ ] Проверить что `message_ids` в `reminder_instances` всё ещё нужен для scheduler-логики (подсчёт повторов) — если нужен, оставить колонку, но убрать поиск по ней
- [ ] Обновить все тесты
- [ ] Проверить что `/list instances` корректно показывает команды с UUID

Финальная очистка. Убираем старые store-методы и fallback-логику. Snooze/skip теперь тоже требуют ID. Колонка `message_ids` остаётся — она нужна scheduler'у для подсчёта количества отправленных уведомлений и определения повтора/мисс.

## Решения и договорённости

- **Таблица instance_replies**: прямая маппинг-таблица `reply_message_id → instance_id` вместо json_each-поиска. Чистый индексный доступ, без хрупкого парсинга JSON в SQL.
- **Inline-кнопки**: ✅ Done, ⏰ Snooze (дефолтный интервал), ⏭ Skip. Callback data = `action:UUID` (41 байт, лимит 64).
- **Полный UUID в callback**: не нужен обратный маппинг, 41 байт вписывается в лимит.
- **Domain-абстракция кнопок**: domain формирует `[]Button{Text, Data}`, Notifier-реализация в bot-слое превращает в `InlineKeyboardMarkup`.
- **done/ok/+ без reply**: ошибка/подсказка как правильно использовать.
- **message_ids остаётся**: колонка нужна для подсчёта повторов в scheduler'е, но поиск по ней убирается.
- **Отложено**: несколько интервалов snooze (5m/15m/30m) — решено сделать один дефолтный. Можно добавить позже.