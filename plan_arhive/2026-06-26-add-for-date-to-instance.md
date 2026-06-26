# Добавить поле ForDate в ReminderInstance

> Решить проблему «Вальхаллы» — инстансы, у которых ScheduledAt переезжает на другой день (через reschedule/snooze), выпадают из /schedule и статистики. Добавить явное поле ForDate — дата, к которой привязан инстанс.

Без задачи (архитектурное улучшение)

## Контекст

ReminderInstance создаётся «на сегодня» (DailyReset, NextInstance), но ScheduledAt может быть сдвинут на другой день через reschedule (MinGap) или /snooze. При этом GetInstancesByUserAndDay фильтрует по scheduled_at в диапазоне [startOfDay, endOfDay) — инстанс с ScheduledAt = завтра 01:00 выпадает из расписания «на сегодня» и не попадает в «на завтра» (DailyReset создаёт новый). Получается сирота — pending-инстанс, видимый scheduler'ом, но невидимый в /schedule.

БД можно пересоздать (миграция не нужна).

## Цель

Каждый инстанс имеет явное поле ForDate — дату (только yyyy-mm-dd), к которой он привязан. ForDate устанавливается при создании и не меняется при reschedule/snooze. Запросы по «инстансам на день» фильтруют по ForDate, а не по ScheduledAt.

## Фаза 1: Добавить ForDate в модель, БД и запросы

- [ ] Добавить `ForDate time.Time` в структуру `ReminderInstance` (internal/store/instance.go)
- [ ] Добавить колонку `for_date INTEGER NOT NULL` в схему БД (internal/store/db.go)
- [ ] Обновить `CreateInstance` — INSERT включает for_date, ForDate устанавливается вызывающим кодом
- [ ] Обновить `scanReminderInstance` — сканирует for_date
- [ ] Обновить все SELECT-запросы в instance.go — добавить for_date в список колонок
- [ ] Переписать `GetInstancesByUserAndDay` — фильтр по `for_date >= startOfDay AND for_date < endOfDay` вместо scheduled_at
- [ ] Установить ForDate при создании инстанса в DailyReset (internal/domain/dailyreset.go) — ForDate = todayStart
- [ ] Установить ForDate при создании инстанса в NextInstance (internal/domain/nextinstance.go) — ForDate = текущий день (из inst.ForDate или now)
- [ ] Установить ForDate при создании инстанса в commands.go (/add) — ForDate = today в timezone пользователя
- [ ] Убедиться что SetInstanceScheduledAt (snooze/reschedule) НЕ меняет ForDate

Добавляем поле ForDate (тип time.Time, хранится как unix timestamp начала дня) в модель инстанса. Это якорь «день, к которому принадлежит инстанс» — не зависит от сдвигов ScheduledAt. Все запросы «инстансы на день» переключаются с фильтра по scheduled_at на фильтр по for_date.

## Фаза 2: Обновить тесты

- [ ] Обновить все тесты, создающие ReminderInstance — добавить ForDate
- [ ] Добавить тест: инстанс с ScheduledAt на другой день, но ForDate = сегодня — попадает в GetInstancesByUserAndDay на сегодня
- [ ] Добавить тест: после snooze ForDate не меняется
- [ ] Добавить тест: после reschedule ForDate не меняется

## Фаза 3: Проверить зависимый код и UX

- [ ] Проверить /schedule — работает через GetInstancesByUserAndDay, теперь по ForDate
- [ ] Проверить /schedule tomorrow — инстансы с ForDate = завтра
- [ ] Проверить что scheduler (GetPending) работает по-прежнему — фильтр по scheduled_at, ForDate не участвует
- [ ] Проверить статистику — теперь можно группировать по for_date

## Решения и договорённости

- **ForDate вместо Date**: название ForDate понятнее — «инстанс на эту дату», а не абстрактное «дата»
- **ForDate не меняется**: при snooze/reschedule меняется только ScheduledAt, ForDate остаётся = дню создания
- **Хранение for_date**: как unix timestamp начала дня (00:00:00) в UTC — для удобства фильтрации
- **ScheduledAt и TimeIndex остаются**: ScheduledAt нужен scheduler'у для фактического срабатывания, TimeIndex нужен для вычисления следующего инстанса в цепочке
- **БД пересоздаётся**: миграция не нужна, данных в проде нет
- **Отложено**: рефакторинг с удалением TimeIndex — пока не актуально, усложнит логику NextInstance