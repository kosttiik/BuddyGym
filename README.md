# BuddyGym

Telegram Mini App and installable PWA: gather in rooms and keep each other accountable for gym visits. A workout is confirmed by a photo and peer votes from room members (or by a geo tag as the fast path). Regularity earns achievements, statuses and profile themes.

## Architecture

```mermaid
flowchart TB
    clients["Telegram Mini App + PWA<br>(frontend)"]
    nginx["nginx<br>static + reverse proxy"]
    core["core-service · Go<br>auth, users, rooms, rewards"]
    checkin["checkin-service · Python<br>photos, geo, peer votes"]
    coredb[("core-db<br>PostgreSQL")]
    redis[("Redis<br>rate limit, locks")]
    checkindb[("checkin-db<br>PostgreSQL")]
    s3[("MinIO<br>private photo bucket")]

    clients -- HTTPS --> nginx
    nginx --> core
    core <-- gRPC --> checkin
    core --> coredb
    core --> redis
    checkin --> checkindb
    checkin --> s3
```

- **core-service** (Go) is the API gateway for the frontend: Telegram and password auth, linked identities, users, rooms, membership, rewards and push subscriptions. It also implements `CoreInternalService` (gRPC) for callbacks from checkin.
- **checkin-service** (Python) owns the checkin lifecycle: photos, geo, peer votes, timeouts, photo retention. It implements `CheckinService` (gRPC) and calls core when a checkin reaches a final status.
- Service contracts live in [proto/buddygym/v1](proto/buddygym/v1) and are generated for both languages (`make proto`, `make proto-py`).
- One Postgres container, two databases: `core_db` and `checkin_db`.

## Workspace layout

The check-in service and [frontend](https://github.com/kosttiik/BuddyGym-Frontend) are cloned
into this workspace (both are gitignored). Compose and proto generation rely on these paths.

```text
BuddyGym/
├── core-service/     Go (this repository)
├── checkin-service/  Python (separate repository)
├── frontend/         Telegram Mini App and PWA (separate repository)
├── proto/            shared gRPC contracts
└── docker-compose.yml
```

## Check-ins and photos

One proof can be submitted to several rooms at once. The photo is uploaded **once** and every resulting checkin references the same object, so posting to five rooms does not store five copies. Each room votes on its own checkin, because the quorum is a per-room setting.

Photos are private end to end:

- the MinIO bucket allows no anonymous access, and no photo URL is ever handed out;
- `GET /api/v1/checkins/{id}/photo` streams the bytes only to members of the room the checkin belongs to;
- uploads are validated by magic bytes, so an SVG or HTML payload cannot be stored under an image name;
- the frontend re-encodes photos before upload, which also strips EXIF (camera GPS tags never leave the device);
- photos are purged 14 days after the checkin, by a background job. A shared photo survives until the newest checkin using it has aged out.

## Quick start

```bash
cp .env.example .env   # set bot, JWT, VAPID and Geoapify credentials
docker compose up -d --build
curl localhost:8080/api/v1/health
```

Swagger UI: `http://localhost:8080/api/v1/docs`

Auth: `POST /api/v1/auth/telegram` exchanges Telegram `initData`; `/auth/register` and `/auth/login` support the browser. Access JWTs stay in memory and browser sessions rotate an HttpOnly refresh cookie. Redis backs rate limiting: 10/min per IP on auth, 120/min per user on the API, 20/hour per user on checkin creation.

A linked Telegram identity can replace forgotten web credentials during the short-lived access session issued by a fresh Telegram launch. Password and refresh-session tokens still require the current password.

## Development (core-service)

```bash
docker compose up -d postgres redis
cd core-service
go test ./...
go run ./cmd/core
```

Codegen: `make proto` (Go stubs, committed), `make swagger` (OpenAPI spec, freshness is checked in CI).

## Notification bot

[BuddyGym-Bot](https://github.com/kosttiik/BuddyGym-Bot) reads the `events` outbox in `core_db`
and delivers Telegram cards and Web Push notifications: comments, vote requests, verdicts,
achievements and reminders. It ships in `dry-run` mode, which renders every card to disk and
sends nothing; flip `BOT_MODE` to `live` with a `BOT_NOTIFY_TOKEN` once the cards look right.

## Deploy

[docker-compose.yml](docker-compose.yml) runs the whole stack for local work and exposes every service on the host for convenience.

For a real host, layer [docker-compose.prod.yml](docker-compose.prod.yml) on top. It publishes **one** port, `127.0.0.1:18080`, serving the built frontend plus the API behind nginx. Postgres, Redis, MinIO, core and checkin get no host ports at all.

```bash
cp deploy/.env.prod.example .env   # set BOT_TOKEN, JWT_SECRET, and every password
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
curl -s -o /dev/null -w '%{http_code}\n' localhost:18080/api/v1/health
```

Terminate TLS in front of it and reverse-proxy the domain to `127.0.0.1:18080`. On a Synology NAS, DSM already owns 80/443, so its built-in reverse proxy (Control Panel, Login Portal, Advanced) forwards `buddygym.ru` to that port while DSM manages the Let's Encrypt certificate. Nothing else on the box is touched.

Update: `git pull && docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build`.
