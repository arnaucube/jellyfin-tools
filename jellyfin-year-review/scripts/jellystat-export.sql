-- Run with psql -XqAt -v ON_ERROR_STOP=1 -f scripts/jellystat-export.sql
-- The role needs SELECT only; the transaction takes one consistent PostgreSQL snapshot.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT json_build_object(
  'Id', "Id", 'UserId', "UserId", 'NowPlayingItemId', "NowPlayingItemId",
  'EpisodeId', "EpisodeId", 'PlaybackDuration', "PlaybackDuration",
  'ActivityDateInserted', to_char("ActivityDateInserted" AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),
  'DeviceName', "DeviceName", 'Client', "Client", 'Imported', false
)::text
FROM public.jf_playback_activity
WHERE COALESCE(imported, false) = false
ORDER BY "Id";
COMMIT;
