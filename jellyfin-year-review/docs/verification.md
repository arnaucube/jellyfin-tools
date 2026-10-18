# Verification performed

Completed in the isolated `reel-review/` directory on 2026-10-17:

- `gofmt` check: clean.
- `go vet ./...`: passed with Go 1.27.2.
- `go test -race ./...`: passed. Synthetic tests cover duplicate normalization/conflicts, resumed movie coverage, missing metadata/runtime, invalid sources, native Jellystat versus prohibited imports, local year/midnight/DST boundaries, per-user timezone, minimum thresholds, genre weighting, overlapping/gapped marathon runs, cohort ties/singletons, all 14 personality labels and deterministic priority, deterministic datasets, multi-year preservation, publication failure, and writer serialization.
- HTTP tests: login success/failure, actual token identity overriding login-response identity, cookie properties, expiry, tampering, revocation, fail-closed Jellyfin outage, logout and failed-revocation behavior, login rate limiting, CSRF, body limits, owner-only review delivery, unavailable years, traversal/direct private paths, private manifest isolation, deep-link shell and CSP hashes, unsupported review schemas.
- Artwork tests use controlled Jellyfin image services; no real operator credentials or production image service is used. Current coverage is described below.
- `npm --prefix web run check`: zero errors, zero warnings.
- `npm --prefix web run build`: passed with SvelteKit 3.1.0 / Svelte 5.57.2 / Vite 8.3.4 and adapter-static 4.0.0.
- Playwright Chromium smoke: passed against the compiled static frontend served by the actual Go HTTP handler with a controlled synthetic Jellyfin service and real generated two-year reviews. Login, chapter controls, keyboard navigation, year selection, reload/deep link, mobile overflow, logout and post-logout access rejection were exercised. No browser page errors. Mobile and desktop screenshots were produced in ignored `web/test-results/`; the mobile result was visually inspected.
- Local CLI generation from the explicit synthetic fixture with `America/New_York`: passed.
- `docker compose config --quiet` with safe test origin/internal URL: passed.
- Multi-stage Docker build: passed. Final image is approximately 12 MB, user `10001:10001`, with no production Node process or shell. Official base image digests are pinned.
- Disposable Docker generator smoke: passed with read-only root, dropped capabilities, no-new-privileges, a read-only synthetic fixture bind mount, and private tmpfs output/cache owned by UID 10001. Published both synthetic user reviews. CLI help also exits successfully.

This container's default home caches were read-only. Verification used temporary npm/Go/Docker caches under `/tmp`. Chromium's missing OS libraries were downloaded and extracted under `/tmp` and supplied through `LD_LIBRARY_PATH`; no system packages or host configuration were modified. On a normal development host, install Playwright's documented Chromium system prerequisites, then run `make e2e` after installing the browser.

No checks against a live Jellyfin or Jellystat PostgreSQL database were performed. Their documented source contracts were inspected and represented by synthetic fixtures/HTTP services. The PostgreSQL export query should be reviewed against the installed Jellystat schema as described in the README; no real database was queried or modified. No live deployment, commit, or push was performed. Existing parent-repository changes were left untouched.

## Offline authentication addition

Verified after adding disconnected serving for Jellyfin 12.2:

- `make check`: Go formatting, vet and race tests, Python exporter tests, and Svelte checks passed. No new Go or frontend dependencies were added; Python is an operator/test-only standard-library utility.
- Independent Python `hashlib` test vectors authenticate through Go's Jellyfin-compatible SHA-512 and legacy SHA-1 PBKDF2 paths, including a Unicode password. Tests cover unknown/disabled users, wrong passwords, duplicate identities, malformed/bounded hashes, missing account status, private permissions, symlink rejection, cancellation, and refresh-driven revocation.
- SQLite fixtures exercise committed, uncheckpointed WAL state, account permissions, external/passwordless/restricted exclusions, missing schema/status, zero eligible users, atomic export failure preservation and private previous-file backups. All six exporter tests passed.
- Offline HTTP tests cover owner-only delivery, extra-selector rejection, direct credential-file requests, generic credential errors, CSRF, throttling, expiry, local logout, malformed-export fail-closed behavior, changed password/disabled-account revocation, and unsafe credential placement. The mock Jellyfin service records **zero requests** during offline tests.
- Static frontend build passed. Browser login → generated review → year selection/deep-link reload → logout passed in **both** online and offline modes. The offline test server constructs no Jellyfin client or fake Jellyfin service, and its login page correctly labels local snapshot validation.
- Docker image rebuilt successfully. `compose.offline.yaml` validates with `JELLYFIN_URL` unset and has no external media-network dependency.
- A complete disposable runtime smoke created a synthetic 12.2 SQLite database, ran the actual exporter CLI, generated review JSON, then served the built image with **`--network none`**, no Jellyfin configuration, read-only mounts/root, dropped capabilities and a non-root UID. A helper sharing only its loopback namespace verified offline config, unauthorized rejection, successful local login, owner review rendering and logout revocation. No real credentials or production services were used, and test containers were removed afterward.

Offline credentials are separate private runtime inputs. They are intentionally excluded from ordinary code-upload rsync and Docker contexts. Real Jellyfin password/status changes cannot be known by a disconnected server until an operator refreshes the export; see `docs/offline-auth.md` for transfer and atomic replacement instructions.

## Jellyfin-only artwork

Artwork generation now uses the existing Jellyfin operator API key and exact movie/parent-series IDs. No separate metadata/artwork service or credential is used. Only content in the selected recap is downloaded; successful Primary images are cached by server, item and image tag, then published as local SHA-256 JPEG assets.

- Artwork tests pass for scoped headers/base paths, deduplicated downloads across users/chapters, avoiding unwatched library titles, cache reuse while the source is unreachable, image-tag invalidation, untagged refresh, corrupt-cache recovery, missing/deleted/unauthorized images, invalid/truncated/oversized JPEGs, unsafe item IDs, redirect rejection, bounded 429/5xx retry/backoff and cancellation.
- The CLI integration test uses source-shaped Jellyfin metadata/image responses and native Jellystat rows. It verifies correct movie/series posters, local files surviving source shutdown, deterministic repeat generation and cache reuse, and absence of source URLs/API keys in review JSON.
- The browser test downloads synthetic Jellyfin posters during generation, closes that image source before serving begins, and verifies a real decoded `/art/<hash>.jpg` poster in both online and offline authentication modes. It also asserts that the browser makes no external requests.
- Frontend configuration, assets, attribution and the generator schema no longer contain an external artwork dependency. Existing owner-visible review JSON remains format v1. Canonical source snapshots may supply `imageTag`; poster input URLs are discarded in favor of generated local references.

The current Go formatting/vet/race suite, Python exporter tests, Svelte checks/production build, both browser flows, Docker image build and offline Compose configuration checks passed. No live Jellyfin data or services were accessed or modified.

The local CLI binary was rebuilt with `go build -buildvcs=false -trimpath` because this workspace has an unavailable Git metadata mount. Docker builds exclude Git metadata and completed normally.
