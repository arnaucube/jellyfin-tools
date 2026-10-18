# jellyfin-year-review

Run these commands from the project directory.

## Translate the webapp

Edit [`web/src/lib/translations.json`](web/src/lib/translations.json). It includes
Catalan (`ca`) and English (`en`). Set the top-level `language` to select the
language used by the webapp. Catalan is selected by default:

```json
{
  "language": "ca",
  "languages": {
    "ca": {
      "locale": "ca",
      "direction": "ltr",
      "strings": {
        "Sign out": "Tanca la sessió"
      }
    },
    "en": {
      "locale": "en",
      "direction": "ltr",
      "strings": {
        "Sign out": "Sign out"
      }
    }
  }
}
```

This example shows only one string; keep the full catalogs in the actual file.
To use English, change only `"language": "ca"` to `"language": "en"`.
An unknown language reports a configuration error.

To add another language, copy a catalog under `languages` with a new language
code. Under its `strings`, keep the English keys and translate the values.
Keep placeholders such as `{year}`, `{name}`, and `{count}` unchanged; you can
move them within the sentence. `\n` adds a line break in the sign-in heading.
Each catalog's `locale` controls numbers, dates, and the page language; it does
not select the catalog. Set `direction` to `rtl` for right-to-left languages,
or keep `ltr`.

The file covers interface text, accessibility labels, months and weekdays,
viewing profiles, and known server errors and review warnings. Usernames, media
titles, and other source data stay as supplied. To translate custom source or
coverage descriptions and genre labels, add their exact original text as a key.
Missing entries fall back to the original text. Values are plain text, not HTML.

Run `npm --prefix web run test:unit` to check translations and placeholders,
then `npm --prefix web run build` and restart the serving process. For Docker,
rebuild the image using the Compose command below. Language selection and
translation edits apply to everyone using that build; no review data needs to
be regenerated.

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
