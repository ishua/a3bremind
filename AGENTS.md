# a3bremind — AGENTS.md

## Quick start

```sh
# run all tests
make test                        # go test ./...
make test-v                      # verbose, no cache
make test-race                   # with race detector
make test-all                    # verbose + race
make test-store                  # only store layer: go test ./internal/store/... -v -count=1

# build
go build ./cmd/a3bremindbot

# lint (must pass after every code change)
make lint                        # golangci-lint run ./...

# run locally (env required)
TELEGRAM_BOT_TOKEN=... DB_PATH=bot.db go run ./cmd/a3bremindbot
```

## Project layout

```
a3bremindbot/
  cmd/a3bremindbot/main.go   — entrypoint, wires store/domain/bot
  internal/
    store/                   — SQLite data layer (pure SQL, no ORM)
    domain/                  — business logic, knows nothing about Telegram
    bot/                     — Telegram integration via go-telegram-bot-api/v5
```

Dependency direction: `store ← domain ← bot`. Each package only imports packages below it.

## Architecture notes

- **No ORM** — raw SQL via `database/sql` + `modernc.org/sqlite` (pure Go, no CGO).
- **Scheduler** (`domain/scheduler.go`) runs a `time.Ticker` every 1 second in a goroutine. It handles pending notifications, repeats, marking as missed, and daily reset at 03:00 in user's timezone.
- **DB concurrency**: `db.SetMaxOpenConns(1)` — required because modernc.org/sqlite `:memory:` databases are per-connection. In production, WAL mode is enabled via PRAGMA.
- **Store test helper**: `newTestDB(t)` returns an in-memory `*sql.DB` with migration applied.
- **Domain test helper**: `setup(t)` creates `(*sql.DB, *mockNotifier, *Scheduler)`. The `mockNotifier` captures sent messages.
- **Domain exports**: `domain/export_test.go` exposes `Tick = (*Scheduler).tick` for testing without starting the goroutine loop.
- **Bot test helper**: `setup(t)` creates `(*sql.DB, *mockBot, *Handler)`. Bot tests use `package bot_test` (external), can't access unexported symbols.
- **Domain config** (`domain/config.go`): `RepeatInterval` (15m), `RepeatCount` (3), `ResetHour` (3). These are mutable `var`s so tests can override them.
- **Handler routing** (`bot/handler.go`): uses `strings.HasPrefix` in a switch, not Telegram bot command menus. All commands including `/done` with UUID are handled here.
- **done/ok/+**: parsed in `HandleUpdate`. `done HH:MM` triggers a confirmation flow backed by `sync.Map` on `Handler.pendingConfirm`.
- **Reply linking**: `reply_to_message_id` → find Instance by `message_ids[]`. Fallback: last active pending instance.

## Template files structure

Each new bot command follows this pattern:
1. Create `internal/bot/<command>.go` with `func (h *Handler) handleXxx(update)`.
2. Register in `handleCommand` in `handler.go` with a `case strings.HasPrefix(text, "/xxx")`.

## Linting

- **Must pass after every code change**: `make lint` (runs `golangci-lint run ./...`).
- Linter config: `.golangci.yml` in `a3bremindbot/`.
- Before committing, verify: `make test && make lint`.
- Do not suppress lint warnings with `//nolint` in production code without a good reason (the comment explaining why is required).

## Key conventions

- **Times stored as HH:MM strings** in Reminder, scheduled times as `time.Time` in instances.
- **All times in UTC in DB**, converted to user's timezone for display.
- **Timezone must be set** before any commands other than `/start` and `/settings` work.
- **MessageIDs** stored as JSON array of `{"message_id": int, "sent_at": unix}`.
- **Once reminders**: on miss, instance AND reminder are atomically deleted (see `store.AddMessageIDAndMarkMissedDeleteOnce`).
- **ForDate vs ScheduledAt**: `ForDate` is the calendar date this instance belongs to in user's timezone. `ScheduledAt` can shift due to reschedule/snooze but `ForDate` stays fixed.
- **Race safety**: `SetStatus` uses `WHERE status = 'pending'` guard to prevent done-from-chat overwriting missed-from-scheduler.

## CI

- GitHub Actions (`./github/workflows/docker-push.yml`): builds and pushes multi-arch Docker image to `ghcr.io` on `v*` tags. context: `./a3bremindbot`.
- Docker image: static binary from `golang:alpine`, runs as `/app`, expects `TELEGRAM_BOT_TOKEN` and `DB_PATH` env vars. Writes to `/data/` volume.

## Repo root

The `plan_arhive/` directory at repo root contains historical implementation plan documents. The actual project lives entirely in `a3bremindbot/`. `readme.md` at root is minimal — the authoritative spec is `a3bremindbot/spec.md`.
