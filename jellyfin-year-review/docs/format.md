# JSON contracts v1

The machine-readable owner-visible format is [review.schema.json](review.schema.json). The authoritative Go contracts are `internal/model/model.go`. JSON is UTF-8, timestamps are RFC3339 instants with offsets, durations are seconds unless explicitly stated, and all user IDs use lowercase 32-hex UUIDs. Unknown source fields and schema versions are rejected. Generated reviews carry `schemaVersion: 1`; both the server and browser reject unsupported review versions. To evolve the meaning or required shape of fields, increment the version and update both consumers. Optional metadata may be absent; `null` personality/marathon/ranking means unavailable and is accompanied by availability evidence.

## Canonical source snapshot

A private snapshot contains:

| Field | Contract |
|---|---|
| `schemaVersion` | Exactly 1 |
| `capturedAt` | Required timestamp of the fixed snapshot, used as deterministic `generatedAt` |
| `source`, `coverage` | Required honest descriptions of collector/version, collection window, retention and gaps |
| `users` | Array of `{id, name}`; immutable ID and display name, no credentials |
| `items` | Array of unique media metadata records |
| `playback` | Finalized unique activity records, or identical duplicates |

An item has `id`, `title`, `type` (`Movie`, `Episode`, `Series`, or an intentionally excluded type), `genres` (array); optional `seriesId`, `seriesTitle`, `year`, nonnegative `runtimeSeconds`, `imageTag` (the Jellyfin Primary image cache tag). `poster` is generator-owned and any input value is discarded by the CLI's artwork resolver. When `JELLYFIN_URL` and `JELLYFIN_API_KEY` are configured, the generator downloads posters by actual Jellyfin item UUIDs and optional image tags; without them a canonical snapshot can still generate reviews with poster placeholders. Media identifiers need not be user UUIDs and never become filesystem paths.

A playback record has `id`, `userId`, `itemId`, positive `seconds`, boolean `exactInterval`, `device`, and `client`. For exact records, provide `start` and `end`, and ensure `seconds = end - start` within 0.01 seconds. For aggregate records, provide `observedAt`, the actual finalized record timestamp; no start/end inference is performed. Optional `positionStart` and `positionEnd` must be supplied together as nonnegative media-position seconds in ascending order; they mean the range actually watched, not just the furthest seek destination. Emit separate ranges if seeking skips content. A zero-length position range is permitted and contributes no coverage. Source times have offsets, so ambiguous local DST timestamps are rejected by normal RFC3339 parsing.

See `testdata/snapshot.json` for a full, explicitly synthetic two-user/two-year example. Unknown/missing user references are skipped and reported; invalid identities fail. Missing item references are excluded and reported per owner. The file size is bounded at 256 MiB; each raw Jellystat JSONL row is bounded at 1 MiB. Prefer smaller source snapshots for this under-30-user deployment.

## Private published dataset

```text
data/
  CURRENT                          # 64-hex dataset content digest
  datasets/<digest>/
    manifest.json                  # schemaVersion + users: { normalizedId: [years...] }
    <normalized-user-id>/<year>.json
    art/<sha256>.jpg
```

All generated directories use mode 0700, JSON/images 0600. The serving OS account must have read access. A staged dataset is completed and synced before `CURRENT` changes. The private manifest and reviews never sit in the frontend webroot. Old snapshots remain for recovery; no public file server points at `data/`.

## Owner-visible review

Required identity/provenance fields: `schemaVersion`, `user: {id, name}`, `year`, `timezone`, `generatedAt`, `source`, `coverage`, `warnings` (array of plain-text strings), `availability` (map of metric → `{available, reason?}`). Availability keys are `exactWatchTime`, `completion`, `marathon`, `personality`, and `ranking`. Their reasons explain unavailable or partially unavailable metrics; zero and unavailable are distinct states.

`rules` records the effective `minimumSeconds`, `completionPercent`, and `marathonGapSeconds` used for this generation.

Statistics: `seconds`, `movieSeconds`, `tvSeconds`, `records`, `uniqueTitles`, `activeDays`. `movies`, `shows` and `abandoned` contain `{id, title, seconds, records, poster?, completionPercent?}`. `poster` is a local `/art/<sha256>.jpg` reference. TVmaze fallback posters also include a `posterSource` link to their TVmaze show page for attribution. Completion percentages are present only where actual media coverage supports them. Full content arrays are retained. Movie and show entries include `months`, a sorted list of local calendar months (1–12) in which the title was watched. Exact intervals contribute every month they overlap within the selected year; aggregate records use their observed end month. Episodes are grouped under their series, with each title appearing once per month. The UI shows every title grouped by month, alphabetically within each month. Older exports without `months` remain visible in a separate undated list; regenerate the selected year to enable its timeline.

`genres`, `devices` and `clients` contain `{name, seconds, records}`. Genre record count is not a unique-title count and is secondary to the documented weighted seconds. `hourly` is length 24, `weekdays` length 7 (Sunday first), `monthly` length 12 (January first). `distributionMetric` is either `watched minutes` or `activity record ends (counts)`; do not infer units from a chart title.

`marathon` is null or `{start, end, watchedSeconds, elapsedSeconds, records}`. `personality` is null or `{name, explanation}`. `ranking` is null or `{rank, cohort, percentile, averageSeconds}`. No other account identities are included. See [metrics](metrics.md) for exact definitions and tie rules.

## HTTP API

All responses are `Cache-Control: no-store`, no CORS access is enabled. Errors use `{error: string}` without upstream error bodies.

| Method/path | Behavior |
|---|---|
| `GET /api/config` | Public mode-only `{authMode:"jellyfin"}` or `{authMode:"offline"}`; no identities, counts or credentials |
| `POST /api/login` | JSON `{username,password}`; validates through the configured online/offline backend, issues opaque HTTP-only cookie |
| `GET /api/session` | Validates the online token or current offline credential revision, returns `{user:{id,name}, years:[...]}` for that token's actual identity only |
| `GET /api/review?year=2025` | Validates the session and reads only that identity's review; extra query parameters are rejected |
| `POST /api/logout` | Deletes local app session/cookie; online mode also revokes only that app device's Jellyfin token |
| `GET /art/<sha256>.jpg` | Public generated shared JPEG; restricted name, never arbitrary files |

State-changing requests require `Content-Type: application/json` and exact configured `Origin`. Missing/tampered/expired/revoked sessions return 401; Jellyfin outages return 503 with no private data; unknown reviews return 404; invalid years/selector attempts return 400; CSRF origin failures return 403; login throttling returns 429. There is no user-selecting review route, public manifest, admin impersonation, or statistics endpoint. Deep links `/review` and `/review/<year>` return only the static application shell; authenticated API reads supply the private data.

## Separate offline credential format v1

`auth/users.json` has `{schemaVersion:1, sourceVersion:"12.2", exportedAt:<RFC3339>, users:[...]}`. Each user has normalized UUID `id`, `username`, boolean `disabled`, and `passwordHash`. A disabled entry has an empty hash; enabled entries require a supported salted Jellyfin PHC-style hex PBKDF2 verifier. These fields are **secret input**, never part of the owner-visible review schema or a public API. The exporter writes at most 1 MiB and 1,000 accounts, with mode 0600. The server also accepts mode 0400. See [offline-auth.md](offline-auth.md) for exact hash bounds, account exclusions, atomic refresh, and session revocation semantics.
