#!/bin/sh
# Generate a selected year using the operator's .env and native Jellystat history.
set -eu

if [ "$#" -ne 1 ]; then
  echo 'usage: scripts/generate-review.sh YEAR' >&2
  exit 1
fi
case "$1" in
  [0-9][0-9][0-9][0-9]) ;;
  *) echo 'Year must be between 1970 and 9998.' >&2; exit 1 ;;
esac
if [ "$1" -lt 1970 ] || [ "$1" -gt 9998 ]; then
  echo 'Year must be between 1970 and 9998.' >&2
  exit 1
fi

cd "$(dirname "$0")/.."
umask 077
printf 'review: Preparing recap generation for %s...\n' "$1" >&2
if [ -f .env ]; then
  set -a
  . ./.env
  set +a
fi
: "${JELLYFIN_URL:?Set JELLYFIN_URL in .env}"
: "${JELLYFIN_API_KEY:?Set JELLYFIN_API_KEY in .env}"
for reel_dependency in go psql; do
  if ! command -v "$reel_dependency" >/dev/null 2>&1; then
    printf 'review: Missing required command: %s. Install Go 1.27.2 and the PostgreSQL client (psql), then retry.\n' "$reel_dependency" >&2
    exit 127
  fi
done

mkdir -p input data cache
reel_captured_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
printf 'review: Exporting Jellystat history...\n' >&2
if scripts/export-jellystat.sh input/jellystat.jsonl; then
  :
else
  reel_export_status=$?
  printf 'review: History export failed. Check PostgreSQL access and PGPASSWORD; review generation was not started.\n' >&2
  exit "$reel_export_status"
fi
printf 'review: Generating the recap and downloading Jellyfin posters...\n' >&2
exec go run ./cmd/review generate --year "$1" \
  --history input/jellystat.jsonl --captured-at "$reel_captured_at" \
  --coverage "${REVIEW_COVERAGE:-Native Jellystat export; collection coverage not verified}" \
  --output data --cache cache
