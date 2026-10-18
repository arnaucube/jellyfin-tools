# Serve independently of Jellyfin

Offline mode lets users log in with the username and password they had in a private Jellyfin **12.2 built-in account snapshot**. Serving needs only the generated `data/`, static frontend, and `auth/users.json`. It creates no Jellyfin client or token and makes no Jellyfin requests for login, session validation, review delivery, or logout. Jellyfin posters and any TVmaze fallback posters are downloaded during generation and served from local files.

The authentication export is a separate secret artifact, **never part of generated review JSON, its manifest, or the frontend build**. It contains salted password verifiers, not plaintext passwords. Someone who obtains it can attempt offline guessing of the corresponding Jellyfin passwords, so transfer it over SSH and retain private filesystem permissions. The application never returns verifiers to the browser or logs them.

## 1. Export on the Jellyfin host

Requires Python 3.9+ with its standard-library SQLite module; no Python package installation or production Python server is needed. The exporter reads the Jellyfin SQLite database containing `Users` and `Permissions`. For Docker installations this is normally the host file mounted at `/config/data/jellyfin.db`; check your own volume mapping. Do not point it at Jellystat, Playback Reporting, or an unsafe copy of a live `.db` file.

```sh
cd reel-review
umask 077
mkdir -p auth
chmod 700 auth
python3 scripts/export-jellyfin-auth.py \
  --database /path/to/jellyfin/config/data/jellyfin.db \
  --output ./auth/users.json
```

Run as an account allowed to read the database and its WAL/SHM files. The source connection uses SQLite `mode=ro`, `query_only`, and a **single read transaction** spanning schema checks, users and permission reads. This pins one consistent snapshot and includes committed WAL contents without copying the full media database or changing it. Do not use `immutable=1` on a live WAL database. If exporting from another machine or a read-only archival mount, first obtain a safe SQLite online backup or stop Jellyfin cleanly before copying its database; copying only a running `.db` can lose recent credential/status updates. No Jellyfin API access is required for this export.

The script checks the supported 12.2 schema, normalizes UUIDs, and reports counts only:

- Enabled local accounts with a supported salted password verifier can log in offline.
- Disabled accounts are exported as blocked entries **without their verifiers**.
- Accounts without remote-access permission, with an access schedule, or requiring a password update are blocked. Offline mode does not implement their interactive/scheduled policies.
- External/LDAP/custom-provider and passwordless accounts are omitted. There is no fallback to another provider or blank-password access.
- Missing status permissions, malformed/unsupported hashes, duplicate identities, or unsupported schemas fail the export and preserve the last successful file. Sign in to Jellyfin to migrate an obsolete hash before exporting again; this application never changes Jellyfin credentials.

The exporter writes mode 0600 files and atomically replaces the destination after validation and fsync. If replacing an existing private regular file, it first saves `users.json.previous` as a private backup. Symlinks and group/world-readable existing exports are refused. The backup is equally sensitive; restoring it restores the old credential snapshot. The credential format is bounded to 1 MiB and 1,000 accounts, comfortably beyond the intended under-30-user deployment.

## 2. Transfer reviews and credentials privately

The project's normal code-upload rsync command excludes `auth/`, `data/`, input databases and environment files through `.gitignore`. Upload runtime data separately. For the first installation, create private `data/` and `auth/` directories on the remote host and transfer both **before starting** the service:

```sh
ssh user@server 'umask 077; mkdir -p /srv/reel-review/data /srv/reel-review/auth'
rsync -av --chmod=D700,F600 ./data/ user@server:/srv/reel-review/data/
rsync -av --chmod=F600 ./auth/users.json user@server:/srv/reel-review/auth/users.json.new
ssh user@server 'cd /srv/reel-review/auth && if test -f users.json; then cp -p users.json users.json.previous; fi && mv users.json.new users.json'
```

Use your actual destination and ensure the serving UID/GID can read these files. A transfer account that owns private files can also be the non-root Compose runtime UID/GID. Do not upload the Jellyfin database, API keys, or generator cache to the remote serving host.

## 3. Start offline serving

Locally, after building and generating reviews:

```sh
export APP_ORIGIN=https://review.example.com
./review serve --data ./data --web ./web/build \
  --auth-mode offline --auth-file ./auth/users.json
```

Or set `REVIEW_AUTH_MODE=offline` and `REVIEW_AUTH_FILE=/absolute/private/path/users.json`. `JELLYFIN_URL` is unnecessary and is not read by offline serving. Credentials must be outside both the frontend directory and the generated-data directory; the server rejects unsafe placement, symlink files and permissive modes (use 0600 or 0400).

For the separate remote server, use the **standalone** offline Compose file:

```sh
# .env needs only APP_ORIGIN and optional non-root REVIEW_UID/REVIEW_GID.
docker compose -f compose.offline.yaml build
docker compose -f compose.offline.yaml up -d
```

This file has no generator, external Jellyfin network or Jellyfin URL. It mounts `data/` and `auth/` read-only and retains the scratch image, non-root runtime and other hardening. Python runs only on the exporting host. Configure HTTPS and the public origin as in the main README. The login page identifies local snapshot validation rather than claiming to contact Jellyfin.

## Credential changes and session revocation

Changes made in Jellyfin are unknown to the disconnected service until you export and upload a replacement. **An old password continues working offline until that refresh**, and newly disabled/deleted Jellyfin users remain eligible until their changes reach the export. There is no background sync or scheduled refresh.

To refresh, rerun the exporter and repeat the `users.json.new` transfer and remote `mv` above. Mount the **auth directory**, not an individual file, so Docker sees atomic inode replacements. On each private request the server opens and validates the current export; it does not retain a credential cache or require a restart:

- Removed/disabled accounts immediately fail subsequent validation.
- Changing a user's password verifier or username invalidates that user's existing app sessions. They sign in again using the refreshed credentials.
- Updating only the export timestamp or an unrelated account preserves unaffected sessions.
- Missing, corrupt, unsupported or newly permissive exports fail closed with 503, exposing no private reviews and falling back to neither cached credentials nor Jellyfin. Restore a valid private file to recover availability.
- Logout deletes only the local app session. There is no Jellyfin token to revoke. Expiry remains 12 hours and app restarts end sessions.

The server uses per-account credential fingerprints only in server memory; hashes and fingerprints are never sent to JavaScript or cookies. Password checks use constant-time derived-key comparison, bounded PBKDF2 parameters and at most two concurrent derivations. Unknown/disabled accounts also perform a default-cost dummy derivation and receive generic credential errors. Existing CSRF, body limits, login rate limits and owner-only authorization still apply.

Supported verifiers match Jellyfin 12.2: `$PBKDF2-SHA512$iterations=N$HEX_SALT$HEX_HASH` (64-byte output) and legacy `$PBKDF2$iterations=N$HEX_SALT$HEX_HASH` (SHA-1, 32-byte output). Supported salts are 8–128 bytes and iteration counts 1–1,000,000; the current Jellyfin default is SHA-512, 210,000 iterations, a 16-byte salt. These existing verifiers are checked without rehashing or mutating the source. Passwords use UTF-8; ASCII usernames are case-insensitive. For non-ASCII usernames use the exact exported spelling, avoiding differences between .NET and Go Unicode case mappings.

When updating review data on a running remote service, upload the new immutable `datasets/<digest>/` directory completely **before** atomically replacing its `CURRENT` pointer. Do not overwrite files inside the dataset currently being served. Authentication files remain separate throughout generation and transfer.
