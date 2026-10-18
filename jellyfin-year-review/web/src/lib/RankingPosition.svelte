<script>
  let { ranking } = $props();
  let above = $derived(ranking.rank - 1);
  // The percentile counts strictly lower totals, excluding the current viewer.
  let below = $derived(Math.round(ranking.percentile * (ranking.cohort - 1) / 100));
  let tied = $derived(ranking.cohort - above - below - 1);
  let viewers = $derived([
    ...Array.from({ length: above }, () => 'above'),
    'you',
    ...Array.from({ length: tied }, () => 'tied'),
    ...Array.from({ length: below }, () => 'below')
  ]);
  const labels = { above: 'Viewer with more watch time', tied: 'Viewer with the same watch time', below: 'Viewer with less watch time' };
</script>

<figure class="ranking-position">
  <figcaption>Your position among {ranking.cohort} {ranking.cohort === 1 ? 'viewer' : 'viewers'}</figcaption>
  {#if ranking.cohort > 1}<div class="ranking-direction" aria-hidden="true"><span>Most watch time</span><span>Least watch time →</span></div>{/if}
  <ol aria-label="Viewers ordered from most to least watch time">
    {#each viewers as kind, i (i)}
      <li class={kind} aria-label={kind === 'you' ? `You: rank ${ranking.rank} of ${ranking.cohort}` : labels[kind]} aria-current={kind === 'you' ? 'true' : undefined}>
        <span class="viewer-icon">
          <svg viewBox="0 0 32 40" aria-hidden="true"><circle cx="16" cy="10" r="7" /><path d="M3 36v-6a13 13 0 0 1 26 0v6Z" /></svg>
        </span>
        {#if kind === 'you'}<strong>You</strong>{/if}
      </li>
    {/each}
  </ol>
  {#if ranking.cohort === 1}
    <p>You are the only eligible viewer.</p>
  {:else}
    <p>{above} above you · {tied} tied with you · {below} below you</p>
  {/if}
</figure>

<style>
  .ranking-position { margin: 28px 0; max-width: 850px; }
  figcaption { font-weight: 700; font-size: 1rem; margin-bottom: 16px; }
  .ranking-direction { display: flex; justify-content: space-between; gap: 12px; font-size: 11px; margin-bottom: 10px; }
  ol { display: grid; grid-template-columns: repeat(auto-fit, minmax(42px, 1fr)); gap: 12px 8px; list-style: none; margin: 0; padding: 0; }
  li { display: flex; flex-direction: column; align-items: center; gap: 5px; min-height: 82px; font-size: 12px; }
  .viewer-icon { display: grid; place-items: center; width: 42px; height: 56px; border: 2px solid transparent; border-radius: 12px; }
  svg { width: 28px; height: 36px; fill: currentColor; }
  .above .viewer-icon { opacity: .65; }
  .below .viewer-icon { opacity: .3; }
  .tied .viewer-icon { border-color: currentColor; border-style: dashed; }
  .you .viewer-icon { background: #202436; color: white; box-shadow: 0 0 0 3px #fff9; }
  strong { font-weight: 800; }
  p { font-size: 12px; margin: 12px 0 0; }
</style>
