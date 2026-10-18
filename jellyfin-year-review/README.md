# jellyfin-year-review

Run these commands from the project directory.

## 1. Generate review data

Requires Go 1.27.2, `psql`, and native Jellystat history. Copy `.env.example` to
`.env` and configure:

- `JELLYFIN_URL` (reachable from this machine) and `JELLYFIN_API_KEY`.
- `PGHOST`, `PGDATABASE`, `PGUSER`, and `PGPASSWORD` for read-only Jellystat access.
- `REVIEW_TIMEZONE`. Posters come from Jellyfin, with TVmaze as a fallback for
  TV shows (no additional API key).

Set `PGPASSWORD='your-postgresql-password'` in `.env` (keep the quotes).
Keep `.env` private (`chmod 600 .env`), then run:

```sh
./scripts/generate-review.sh 2025
```

The script reports its progress, loads `.env`, exports history, and generates
`data/`. If it reports a missing `psql`, install the PostgreSQL client on this
machine (for Debian/Ubuntu: `sudo apt install postgresql-client`). Change the year
to generate another recap. Optionally set `REVIEW_COVERAGE` to describe your
collection window and known gaps.

## 2. Export Jellyfin users

With Python 3.9+ on the Jellyfin 12.2 host:

```sh
python3 scripts/export-jellyfin-auth.py --database /path/to/jellyfin.db --output auth/users.json
```

Use the actual database path with read access to its WAL/SHM files. The export
contains private password verifiers, never plaintext passwords. Rerun after
password or account changes. Only supported built-in accounts can log in offline.

## 3. Serve the webapp

On the serving host, place the project, generated `data/`, and private
`auth/users.json` together. **Upload `data/` and `auth/` separately:** the normal
code-upload rsync excludes them. Do not upload the Jellyfin database or API keys.

Run either option as the non-root user who owns `data/` and `auth/`.
First set permissions and the public origin:

```sh
chmod 700 data auth
chmod 600 auth/users.json

export APP_ORIGIN=https://review.example.com
```

With Docker Compose:

```sh
export REVIEW_UID="$(id -u)" REVIEW_GID="$(id -g)"
docker compose -f compose.offline.yaml up -d --build
```

Without Docker (requires Go 1.27.2 and Node.js 24+ to build):

```sh
npm --prefix web ci
npm --prefix web run build
go build -buildvcs=false -o ./review ./cmd/review

./review serve --listen 127.0.0.1:8080 \
  --data ./data --web ./web/build \
  --auth-mode offline --auth-file ./auth/users.json
```

The server runs in the foreground; Node.js is only needed for the frontend build.

Point your HTTPS reverse proxy at `127.0.0.1:8080`; `APP_ORIGIN` must match the
public origin exactly, without a trailing slash. Compose reads settings from
`.env`; without Docker, export `APP_ORIGIN` each time you start the server.

Users log in with their exported Jellyfin credentials. Serving requires no
Jellyfin connection. See [offline setup](docs/offline-auth.md) for secure transfer
and atomic credential-refresh instructions.
