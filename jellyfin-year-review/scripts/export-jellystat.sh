#!/bin/sh
# PGHOST/PGDATABASE/PGUSER and PGPASSWORD configure read-only access.
set -eu
if [ "$#" -ne 1 ]; then echo 'usage: scripts/export-jellystat.sh input/history.jsonl' >&2; exit 1; fi
: "${PGPASSWORD:?Set PGPASSWORD in .env or export it before running this script}"
export PGPASSWORD
umask 077
reel_export_tmp=$(mktemp "${1}.tmp.XXXXXX")
trap 'rm -f "$reel_export_tmp"' EXIT HUP INT TERM
# Disable interactive prompts; psql still reads the password from PGPASSWORD.
psql --no-password -XqAt -v ON_ERROR_STOP=1 -f "$(dirname "$0")/jellystat-export.sql" > "$reel_export_tmp"
mv "$reel_export_tmp" "$1"
