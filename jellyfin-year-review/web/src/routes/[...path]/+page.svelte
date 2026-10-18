<script>
  import { onMount } from 'svelte';
  import { t, message, locale } from '../../lib/i18n.js';
  import Poster from '../../lib/Poster.svelte';
  import PlatformBreakdown from '../../lib/PlatformBreakdown.svelte';
  import Chart from '../../lib/Chart.svelte';
  import RadarChart from '../../lib/RadarChart.svelte';
  import ViewingTimeline from '../../lib/ViewingTimeline.svelte';
  import RankingPosition from '../../lib/RankingPosition.svelte';
  import Confetti from '../../lib/Confetti.svelte';

  let user = $state(null), years = $state([]), year = $state(''), review = $state(null);
  let loading = $state(true), signingIn = $state(false), error = $state(''), notice = $state('');
  let username = $state(''), password = $state(''), slide = $state(0);
  let swipeDirection = $state(0);
  let touch = null, request = 0;
  let authMode = $state('jellyfin');
  const chapters = [
    { id: 0, name: t("Your viewing recap") },
    { id: 1, name: t("Your watch time"), availability: 'exactWatchTime' },
    { id: 2, name: t("Movies watched") },
    { id: 3, name: t("TV shows watched") },
    { id: 4, name: t("Unfinished movies"), availability: 'completion' },
    { id: 14, name: t("Shows you've watched are from platforms...") },
    { id: 5, name: t("Your top genres") },
    { id: 6, name: t("Viewing by hour") },
    { id: 7, name: t("Viewing by weekday") },
    { id: 8, name: t("Viewing by month") },
    { id: 9, name: t("Your devices and apps") },
    { id: 10, name: t("Your longest viewing session"), availability: 'marathon' },
    { id: 11, name: t("Your viewing profile"), availability: 'personality' },
    { id: 12, name: t("It's not a competition... but this is your watch-time ranking"), availability: 'ranking' },
    { id: 13, name: t("Your year in summary") }
  ];
  // Navigation and chapter numbering follow only the available cards for this year.
  let cards = $derived(chapters.filter(chapter => !chapter.availability || review?.availability?.[chapter.availability]?.available !== false));
  let card = $derived(cards[slide]?.id);
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'].map(value => t(value));
  const weekdays = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'].map(value => t(value));
  const count = (n) => new Intl.NumberFormat(locale).format(n);
  const hours = (n) => new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(n / 3600);
  const duration = (n) => t("{hours} h", { hours: hours(n) });
  const interactive = (target) => target instanceof Element && !!target.closest('button, input, select, a, textarea');

  async function api(path, options = {}) {
    let res, body;
    try {
      res = await fetch(path, { credentials: 'same-origin', ...options });
      body = await res.json();
    } catch {
      throw new Error(t("The request could not be completed."));
    }
    if (!res.ok) {
      if (res.status === 401 && path !== '/api/login') { user = null; review = null; }
      throw Object.assign(new Error(message(body.error) || t("The request could not be completed.")), { status: res.status });
    }
    return body;
  }
  async function loadReview() {
    if (!year) return;
    const current = ++request;
    loading = true; error = ''; review = null; slide = 0; swipeDirection = 0;
    try {
      const data = await api(`/api/review?year=${encodeURIComponent(year)}`);
      if (current !== request) return;
      if (data.schemaVersion !== 1) throw new Error(t("This review format is not supported. Ask the operator to update the app."));
      review = data;
      history.replaceState(null, '', `/review/${year}`);
    } catch (e) { if (current === request) error = e.message; }
    finally { if (current === request) loading = false; }
  }
  async function bootstrap() {
    loading = true; error = '';
    try {
      const config = await api('/api/config'); authMode = config.authMode;
      const session = await api('/api/session'); user = session.user; years = session.years;
      const pathYear = location.pathname.split('/').filter(Boolean)[1];
      year = String(years.includes(Number(pathYear)) ? Number(pathYear) : years.at(-1) || '');
      if (year) await loadReview();
    } catch (e) { if (user || e.status !== 401) error = e.message; }
    finally { loading = false; }
  }
  async function login(event) {
    event.preventDefault(); signingIn = true; error = ''; notice = '';
    try {
      await api('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username, password }) });
      password = ''; await bootstrap();
    } catch (e) { error = e.message; }
    finally { password = ''; signingIn = false; }
  }
  async function logout() {
    notice = ''; error = '';
    try { await api('/api/logout', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }); }
    catch (e) { notice = e.message; }
    finally { request++; user = null; review = null; years = []; password = ''; history.replaceState(null, '', '/'); }
  }
  // Route every navigation method through the same direction and boundary rules.
  function goTo(index) {
    const next = Math.max(0, Math.min(cards.length - 1, index));
    if (next === slide) return;
    swipeDirection = next > slide ? 1 : -1;
    slide = next;
  }
  function navigate(delta) { goTo(slide + delta); }
  function keydown(event) {
    if (!review || interactive(event.target) || event.altKey || event.ctrlKey || event.metaKey) return;
    if (event.key === 'ArrowRight') { event.preventDefault(); navigate(1); }
    if (event.key === 'ArrowLeft') { event.preventDefault(); navigate(-1); }
    if (event.key === 'Home') { event.preventDefault(); goTo(0); }
    if (event.key === 'End') { event.preventDefault(); goTo(cards.length - 1); }
  }
  function pointerdown(event) { if (event.pointerType === 'touch' && !interactive(event.target)) touch = { x: event.clientX, y: event.clientY }; }
  function pointerup(event) {
    if (!touch) return;
    const dx = event.clientX - touch.x, dy = event.clientY - touch.y; touch = null;
    if (Math.abs(dx) > 65 && Math.abs(dx) > Math.abs(dy) * 1.5) navigate(dx < 0 ? 1 : -1);
  }
  onMount(bootstrap);
</script>

<svelte:head><title>{t("Jellyfin Year Review")}</title><meta name="description" content={t("Your private Jellyfin year in movies and television.")} /></svelte:head>
<svelte:window onkeydown={keydown} onpointerup={pointerup} onpointercancel={() => touch = null} />

<div class="app-shell" class:login-only={!user}>
  {#if user}
  <header class="topbar">
    <a class="brand" href="/" aria-label={t("Jellyfin Year Review home")}><span class="brand-icon">{t("J")}</span><span>{t("Jellyfin")} <span class="brand-light">{t("Year Review")}</span><small>{t("A YEAR WORTH REPLAYING")}</small></span></a>
    {#if user}
      <div class="account">
        {#if years.length > 1}<label class="year-picker">{t("Year")}<select aria-label={t("Review year")} bind:value={year} onchange={loadReview}>{#each years as y}<option value={String(y)}>{y}</option>{/each}</select></label>{/if}
        <button class="quiet" onclick={logout}>{t("Sign out")}</button>
      </div>
    {/if}
  </header>
  {/if}

  <main>
    {#if user}
      {#if notice}<p class="message" role="status">{notice}</p>{/if}
      {#if error}<div class="message error" role="alert">{error}<button class="quiet" onclick={bootstrap}>{t("Try again")}</button></div>{/if}
    {/if}
    {#if !user}
      <section class="login-layout" aria-label={t("Sign in")}>
        <form class="login-panel" onsubmit={login}>
          <h1 style="white-space: pre-line">{t("Your year,\nreplayed.")}</h1><p>{t("Sign in with your existing Jellyfin account.")}</p>
          {#if notice}<p class="message" role="status">{notice}</p>{/if}
          {#if error}<div class="message error" role="alert">{error}</div>{/if}
          <label for="username">{t("Jellyfin username")}</label><input id="username" name="username" autocomplete="username" required maxlength="256" bind:value={username} />
          <label for="password">{t("Password")}</label><input id="password" name="password" type="password" autocomplete="current-password" bind:value={password} />
          <button class="primary" type="submit" disabled={loading || signingIn}>{loading ? t("Loading…") : signingIn ? t("Signing in…") : t("Roll the credits")} <span aria-hidden="true">↗</span></button>
          <small class="login-note">{authMode === 'offline' ? t("Your credentials are checked locally against a private Jellyfin account snapshot. Password changes take effect after the operator refreshes it.") : t("Your credentials are validated by your Jellyfin server. Your review stays private.")}</small>
        </form>
      </section>
    {:else if loading}
      <section class="state-screen"><span class="loading-orbit" aria-hidden="true"></span><h1>{t("Setting the scene…")}</h1><p role="status">{t("Loading your private review")}</p></section>
    {:else if !years.length}
      <section class="state-screen"><p class="eyebrow">{t("NOT QUITE SHOWTIME")}</p><h1>{t("Your story is on its way.")}</h1><p>{t("No review has been generated for your account. Ask your server operator to generate a year.")}</p><button class="primary" onclick={bootstrap}>{t("Check again")}</button></section>
    {:else if review}
      <div class="review-meta"><span>{review.year} / {user.name}</span></div>
      <nav class="progress" aria-label={t("Review chapters")}>{#each cards as chapter, i}<button class:active={slide === i} class:visited={slide > i} aria-label="{i + 1}. {chapter.name}" aria-current={slide === i ? 'step' : undefined} onclick={() => goTo(i)}><span></span></button>{/each}</nav>
      <section class="story tone-{slide % 4}" aria-roledescription={t("carousel")} aria-label={t("Your yearly review")} onpointerdown={pointerdown}>
        <div class="story-orbit" aria-hidden="true"></div>
        <div class="story-content" aria-live="polite" aria-atomic="true">
        {#key card}
        <div class="story-body" style:--swipe-from="{swipeDirection * 105}%">
          <p class="eyebrow">{t("CHAPTER {number} / {name}", { number: String(slide + 1).padStart(2, '0'), name: cards[slide].name.toLocaleUpperCase(locale) })}</p>
          {#if card === 0}
            <span class="year-stamp">{review.year}</span><h1>{t("Your viewing recap")}</h1><p class="story-lede">{t(review.records ? "{name}, see the movies, TV shows, and viewing habits that made up your year." : "{name}, no viewing activity was recorded for this year.", { name: user.name })}</p><button class="primary" onclick={() => navigate(1)}>{t("View my recap")} <span aria-hidden="true">→</span></button><span class="giant-play" aria-hidden="true">▶</span>
          {:else if card === 1}
            <h2>{t("Your watch time")}</h2><div class="big-stat">{hours(review.seconds)}<span>{t(review.availability.exactWatchTime.available ? "hours watched" : "hours watched · end-attributed estimate")}</span></div>
            <div class="stat-grid"><div><b>{duration(review.movieSeconds)}</b><span>{t("Movies")}</span></div><div><b>{duration(review.tvSeconds)}</b><span>{t("Television")}</span></div><div><b>{count(review.records)}</b><span>{t("Activity records")}</span></div><div><b>{count(review.uniqueTitles)}</b><span>{t("Unique movies & episodes")}</span></div></div>
          {:else if card === 2 || card === 3}
            {@const items = card === 2 ? review.movies : review.shows}
            <h2>{card === 2 ? t("Movies watched") : t("TV shows watched")}</h2>
            <p>{t("Everything you watched in {year}, grouped by month.", { year: review.year })}</p>
            <ViewingTimeline {items} year={review.year} kind={card === 2 ? 'movies' : 'shows'} />
          {:else if card === 4}
            <h2>{t("Unfinished movies")}</h2><p class="story-lede">{t("Movies where you watched less than {percent}% of the runtime.", { percent: review.rules.completionPercent })}</p>
            {#if review.abandoned.length}<ol class="content-list compact">{#each review.abandoned.slice(0, 5) as item}<li><Poster {item} /><div><h3>{item.title}</h3><p>{t("{percent}% of the movie watched", { percent: Math.round(item.completionPercent) })}</p></div></li>{/each}</ol>{:else}<p class="empty-note">{review.availability.completion.available ? t("No unfinished movies found in supported records.") : t("Completion is unavailable without runtime and actual position ranges.")}</p>{/if}
          {:else if card === 14}
            <h2>{t("Shows you've watched are from platforms...")}</h2>
            <p>{t("Distinct TV shows watched, grouped by platform.")}</p>
            <PlatformBreakdown platforms={review.platforms ?? []} records={review.records} />
          {:else if card === 5}
            <h2>{t("Your top genres")}</h2><p>{t("Genres ranked by watch time.")}</p>
            {#if review.genres.length}
              {@const values = review.genres.slice(0, 7).map(v => v.seconds / 3600)}
              {@const labels = review.genres.slice(0, 7).map(v => message(v.name))}
              <Chart {values} {labels} metric={t("h")} />
              <RadarChart {values} {labels} metric={t("h")} />
            {:else}<p class="empty-note">{t("No genre history is available.")}</p>{/if}
          {:else if card === 6}
            <h2>{t("Viewing by hour")}</h2><p>{t("Your viewing activity across the hours of the day.")}</p><div class="hour-chart"><Chart values={review.hourly} labels={Array.from({ length: 24 }, (_, i) => `${String(i).padStart(2, '0')}:00`)} metric={review.distributionMetric === 'watched minutes' ? t("min") : t("records")} /></div>
          {:else if card === 7}
            <h2>{t("Viewing by weekday")}</h2><p>{t("Your viewing activity for each day of the week.")}</p><Chart values={[...review.weekdays.slice(1), review.weekdays[0]]} labels={weekdays} metric={review.distributionMetric === 'watched minutes' ? t("min") : t("records")} />
          {:else if card === 8}
            <h2>{t("Viewing by month")}</h2><p>{t("Your viewing activity for each month of {year}.", { year: review.year })}</p><Chart values={review.monthly} labels={months} metric={review.distributionMetric === 'watched minutes' ? t("min") : t("records")} />
          {:else if card === 9}
            <h2>{t("Your devices and apps")}</h2><p>{t("Your most-used devices and apps, ranked by watch time.")}</p><div class="two-columns"><div><h3>{t("Devices")}</h3><Chart values={review.devices.slice(0, 5).map(v => v.seconds / 3600)} labels={review.devices.slice(0, 5).map(v => message(v.name))} metric={t("h")} /></div><div><h3>{t("Apps & clients")}</h3><Chart values={review.clients.slice(0, 5).map(v => v.seconds / 3600)} labels={review.clients.slice(0, 5).map(v => message(v.name))} metric={t("h")} /></div></div>
          {:else if card === 10}
            <h2>{t("Your longest viewing session")}</h2>{#if review.marathon}<div class="big-stat">{hours(review.marathon.watchedSeconds)}<span>{t("hours watched in your longest session")}</span></div><p class="story-lede">{t("{duration} elapsed · {records} records", { duration: duration(review.marathon.elapsedSeconds), records: count(review.marathon.records) })}</p><p>{new Date(review.marathon.start).toLocaleString(locale, { timeZone: review.timezone })}</p>{:else}<p class="empty-note">{message(review.availability.marathon.reason)}</p>{/if}
          {:else if card === 11}
            <h2>{t("Your viewing profile")}</h2>{#if review.personality}<span class="personality-symbol" aria-hidden="true">✳</span><h3 class="personality-name">{message(review.personality.name)}</h3><p class="story-lede">{message(review.personality.explanation)}</p>{:else}<p class="empty-note">{message(review.availability.personality.reason)}</p>{/if}
          {:else if card === 12}
            <h2 class="ranking-title"><span>{t("It's not a competition...")}</span>{' '}<span class="ranking-subtitle">{t("but this is your watch-time ranking")}</span></h2>
            {#if review.ranking}
              <div class="ranking-highlights">
                <div class="big-stat"><span class="ranking-value">#{review.ranking.rank}</span><span>{t("of {count} eligible viewers", { count: count(review.ranking.cohort) })}</span></div>
                <div class="big-stat"><span class="ranking-value">{hours(review.seconds)} <small>{t("h")}</small></span><span>{t("Your total hours watched · movies & TV shows")}</span></div>
              </div>
              <RankingPosition ranking={review.ranking} />
              <div class="stat-grid ranking-secondary"><div><b>{Math.round(review.ranking.percentile)}%</b><span>{t("Viewers with less watch time")}</span></div></div>
            {:else}<p class="empty-note">{message(review.availability.ranking.reason)}</p>{/if}
          {:else}
            <p class="year-stamp">{t("SUMMARY / {year}", { year: review.year })}</p><h2>{t("Your year in summary")}</h2><div class="stat-grid">{#if review.availability.exactWatchTime.available !== false}<div><b>{duration(review.seconds)}</b><span>{t("Your watch time")}</span></div>{/if}{#if review.availability.personality.available !== false}<div><b>{message(review.personality?.name) || t("No viewing profile")}</b><span>{t("Your viewing profile")}</span></div>{/if}</div><p class="story-lede">{t("You have reached the end of your viewing recap.")}</p><button class="primary" onclick={() => goTo(0)}>{t("View recap again ↺")}</button>
          {/if}
        </div>
        {/key}
        </div>
        <div class="story-bottom"><span>{String(slide + 1).padStart(2, '0')} <span class="muted">/ {cards.length}</span></span><div class="story-controls"><button aria-label={t("Previous chapter")} onclick={() => navigate(-1)} disabled={slide === 0}>←</button><button aria-label={t("Next chapter")} onclick={() => navigate(1)} disabled={slide === cards.length - 1}>→</button></div></div>
      </section>
      <details class="source-notes"><summary>{t("Behind your review · source & availability")}</summary><p>{message(review.source)}</p><p>{message(review.coverage)}</p><p>{t("Snapshot: {date} · Format v{version}", { date: new Date(review.generatedAt).toISOString(), version: review.schemaVersion })}</p>{#each review.warnings as warning}<p>{message(warning)}</p>{/each}</details>
    {:else}<section class="state-screen"><h1>{t("Let’s try that again.")}</h1><button class="primary" onclick={bootstrap}>{t("Reload review")}</button></section>{/if}
  </main>
  {#if user}<footer><span>{t("MADE FOR THE LOVE OF THE WATCH.")}</span></footer>{/if}
</div>

{#if user && !loading && review?.ranking && card === 12}<Confetti />{/if}
