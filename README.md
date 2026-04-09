# Print Farm Dashboard

A small print-farm management dashboard built with React, Vite, PostgreSQL, and a Python printer poller.

## Features

- dashboard for printer status, webcam previews, and live job activity
- queue sync from a Google Sheet into PostgreSQL
- queue history with local printed status tracking
- analytics backed by PostgreSQL
- optional public viewer mode that hides sensitive printer details

## Stack

- `web`: Vite app plus lightweight Node API middleware
- `db`: PostgreSQL
- `poller`: Python background service for printer status refresh
- `nginx`: reverse proxy in front of the app

## Quick Start

1. Copy env defaults:

```bash
cp .env.example .env
```

2. Review the values in `.env`.

3. Start the stack:

```bash
docker compose up --build
```

4. Open:

```text
http://localhost:5173
```

## Environment

Key settings in `.env.example`:

- `POSTGRES_DB`
- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `POSTGRES_PORT`
- `VITE_PUBLIC_VIEWER_MODE`

The app container and poller derive their `DATABASE_URL` from those values in `docker-compose.yml`.

## Notes

- `.env` is intentionally ignored by git and should not be committed.
- The built-in browser auth is suitable for local/demo use, not hardened production auth.
- If you enable `VITE_PUBLIC_VIEWER_MODE="true"`, the UI auto-enters viewer mode and printer list responses redact sensitive connection fields.

## License

This project is released under the MIT License. See [LICENSE](LICENSE).
