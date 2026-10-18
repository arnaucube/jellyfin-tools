# Metric definitions (v1)

All calculations happen in `generate`. The serving process never recalculates metrics. Sources must contain finalized records, not repeated progress updates. Native Jellystat rows are aggregates and may combine resumes. Canonical `exactInterval: true` rows represent uninterrupted watched wall-clock intervals, with duration exactly matching end minus start. If a collector has pauses, it must emit separate watched intervals, not spread watched time over the entire paused session.

## Normalization and eligibility

- Normalize user UUIDs to lowercase, hyphen-free 32-hex strings. Reject malformed or duplicate user identities. Media IDs are case-folded with hyphens removed for consistent Jellyfin matching.
- Duplicate `(userId, recordId)` records are removed only when all canonical fields agree. Conflicting duplicates fail generation. Different record IDs are distinct; no heuristic joining or double summing of cumulative progress updates is attempted. An upstream collector must finalize cumulative updates first.
- Invalid/nonpositive/nonfinite durations, durations longer than 366 days, invalid position bounds, and invalid exact intervals fail generation. Default minimum original-record watch time is **60 seconds**, adjustable with `--minimum-seconds`. Apply this before clipping the local year, so short year-boundary portions of valid sessions survive.
- Only `Movie` and `Episode` contribute. Series are metadata/grouping records. Audio, music videos, trailers, unsupported types, and records with deleted/missing media metadata are excluded. A missing media record produces a warning rather than invented title/type. A trailer incorrectly identified by Jellyfin as a movie cannot be distinguished; correct its metadata upstream.
- Missing device/client names form an explicit `Unknown` group. Names have whitespace collapsed and case-insensitive grouping; the lexicographically smaller display spelling wins. No IP addresses appear in output.
- Missing runtimes never affect measured watch duration. They make media completion unavailable.

## Year, timezone, and temporal precision

The effective IANA timezone is the configured default (UTC unless overridden) or a per-user ID mapping. Each local year's boundary is January 1 00:00 inclusive to next January 1 exclusive, converted by Go's timezone rules to actual instants. Exact intervals are clipped to this range. DST and the server's process timezone do not change attribution.

Exact interval distributions allocate **watched minutes** over actual UTC minute segments and label each segment by local hour, weekday (Sunday index 0), and month (January index 0). Fractional minutes are retained. Repeated DST hours accumulate into the same local-hour bucket; skipped hours contain no fabricated time. Midnight/year crossings split correctly. Simultaneous records contribute their own measured activity seconds and minutes to personal totals: these are summed device activity, not deduplicated human attention. Only marathon watch time unions overlap.

Jellystat aggregate rows retain their measured duration but assign the entire record to the **local year of its observed end**. Pauses/resumes and the updated stop timestamp mean exact selected-year attribution is unavailable. Distributions then show **activity record ends (counts)**, not watched minutes or session starts. If even one eligible record has aggregate precision, all distributions for that review switch to this consistent unit; exact records in the mixed set use their clipped end. Exact yearly watch-time availability is false. The generator never pretends these rows reconstruct continuous sessions.

`activeDays` counts dates containing exact watched intervals for exact reviews; for mixed/aggregate reviews it counts attributed end-record dates. It is not a streak. `records` is an activity-record count. `uniqueTitles` counts unique movie and episode IDs, not series. Repeated activity records, completed plays, and unique titles are deliberately distinct.

## Content, completion, genres, and artwork

Movie totals group by movie ID. Show totals group by series ID when known; otherwise the episode ID/title remains a standalone entry. Sort by watched seconds descending, then record count descending, then normalized media ID ascending. Equal scores therefore remain deterministic. The frontend presents the top five, while the document retains all entries.

Completion requires movie runtime **and actual media position ranges for every contributing record**, not duration/runtime arithmetic. Union the recorded position ranges across all the selected year's resumed records, clamp to runtime, then compute `100 * coveredPositions / runtime` (capped at 100). Repeated ranges never inflate coverage. Different resumes covering different parts of a movie combine. Any record without position coverage, without runtime, or clipped at the year's boundary makes that movie unavailable. Jellystat's final `PlayState.PositionTicks` alone does not establish the watched range, so it is never used to invent coverage.

The default completion threshold is **90%**, configurable with `--completion-percent`. Movies with known coverage below it are classified as abandoned/unfinished **within the selected year**. A later complete resume in the same year prevents an earlier partial record being listed. Completion in a later year is deliberately outside this year's snapshot. Unknown coverage is omitted from abandonment; availability explains that the resulting list may be incomplete. The UI rounds the displayed percentage; classification uses the unrounded value.

Genre watch seconds are split equally over distinct genres for each media item. Episodes inherit series genres when absent. A missing genre becomes Unknown. Sorting is seconds descending then display label ascending. Device/client charts use the same watch-second weighting, even for aggregate rows (with the same year-attribution limitation).

Metadata comes from Jellyfin. During generation, posters are downloaded from Jellyfin’s Primary image endpoint using the existing operator API key and exact movie/series IDs; there is no external metadata matching or separate artwork credential. Only titles in the selected year’s generated reviews are downloaded, with repeated titles shared across users and chapters. Show posters use their parent series. Missing, deleted or invalid images keep explicit placeholders. Shared local JPEG names are SHA-256 content hashes, not user identities. Successful downloads are cached by server URL, item ID and Primary image tag; changing a tag fetches new artwork. If no image tag is available, generation refreshes the image each time. Serving reads only the published local copies.

## Marathons

Exact intervals only. Sort by start, then end. Intervals belong to one run while the next start is at most **45 minutes** after the current maximum end; configure with `--marathon-gap` (0 through 24h). The watched duration is the union of the run's intervals, so overlapping playback counts once. The elapsed span is last end minus first start, including gaps. The run with most watched seconds wins; earliest start breaks a tie. A single interval is a valid run. Aggregate history yields unavailable, not an estimate of a marathon.

## Cohort comparisons

The cohort includes enabled users in the source identity snapshot with at least **3,600 contributed watch seconds** in their effective local year. Users without history and those below one hour are ineligible. Rank is `1 + number of strictly greater totals`, so ties use competition ranks (1, 1, 3). Percentile is `100 * strictlyLowerTotals / (cohortSize - 1)`; a singleton has percentile 0. Equal peers are not treated as lower. The comparison includes rank, cohort size, percentile and cohort average seconds only. It exposes no other identities or histories. For Jellystat history comparisons use end-attributed aggregate totals and carry the same limitation as personal totals.

## Exactly 14 personalities

These are playful deterministic viewing labels, never psychological assessments. Every label requires **at least 10 eligible exact records and 5 watched hours**. Aggregate/mixed history or insufficient data is unavailable, not a fifteenth type. Evaluate these rules **in this order**; the first true rule wins. This makes ties explicit and stable.

| Priority | Label | Evidence |
|---|---|---|
| 1 | Night Owl | At least 40% of watch minutes in local 23:00–05:00 |
| 2 | Early Bird | At least 40% in 05:00–10:00 |
| 3 | Weekend Warrior | At least 60% on Saturday/Sunday |
| 4 | Marathoner | Longest run has at least 6 watched hours |
| 5 | Binge Watcher | At least 70% TV, longest run ≥3 records and ≥3 watched hours |
| 6 | Movie Buff | Movies account for at least 80% of watch time |
| 7 | Series Devotee | TV accounts for at least 80% of watch time |
| 8 | Comfort Rewatcher | `(records - unique movie/episode IDs) / records ≥ 0.5` |
| 9 | Genre Explorer | At least 6 known genres each weighted at ≥3% of watch time |
| 10 | Prime-Time Regular | At least 50% of minutes in local 19:00–23:00 |
| 11 | Daily Viewer | At least 180 distinct watched dates |
| 12 | Seasonal Viewer | At least 40% of minutes in one month |
| 13 | Short-Session Viewer | Mean exact uninterrupted record duration ≤25 minutes |
| 14 | Balanced Viewer | Eligible data with none of the preceding dominant patterns |

The result includes the matching evidence sentence. Rewatching and short-session labels are record-based proxies: even exact intervals do not establish separate completed plays or intentions. Multiple pause-separated intervals for the same title may affect these proxies. Each supplied exact record's meaning must be documented by its collector in source coverage.
