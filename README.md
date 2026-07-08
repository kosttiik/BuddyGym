# BuddyGym

Telegram Mini App: собираетесь в комнаты и держите друг друга в тонусе походами в зал. Тренировка подтверждается фоткой и голосами участников комнаты (или геометкой как быстрый вариант). За регулярность — ачивки, статусы и темы профиля.

## Архитектура

```
Telegram Mini App (фронт)
        │ HTTPS
        ▼
  core-service (Go)  ◄── gRPC ──►  checkin-service (Python)
   │          │                     │            │
   ▼          ▼                     ▼            ▼
core-db     Redis               checkin-db   MinIO/S3
(Postgres)  (кэш, локи)         (Postgres)   (фото)
```

- **core-service** (Go) — API-гейтвей для фронта: авторизация через Telegram `initData`, пользователи, комнаты, членство, награды. Реализует `CoreInternalService` (gRPC) для колбэков от checkin.
- **checkin-service** (Python) — жизненный цикл чек-ина: фото, гео, голоса участников, таймауты. Реализует `CheckinService` (gRPC), по результату дёргает core.
- Контракты между сервисами — в [proto/buddygym/v1](proto/buddygym/v1), генерятся для обоих языков (`make proto`, `make proto-py`).
- Один контейнер Postgres, две базы: `core_db` и `checkin_db`.

## Быстрый старт

```bash
cp .env.example .env   # вписать BOT_TOKEN
make up                # postgres + redis + minio + core
curl localhost:8080/api/v1/health
```

Swagger UI: `http://localhost:8080/api/v1/docs`

## Разработка (core-service)

```bash
docker compose up -d postgres redis
cd core-service
go test ./...
go run ./cmd/core
```

Кодоген: `make proto` (Go-стабы, коммитятся), `make swagger` (OpenAPI-спека).

Для checkin-service см. [docs/checkin-service.md](docs/checkin-service.md).
