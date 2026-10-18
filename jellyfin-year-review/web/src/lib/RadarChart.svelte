<script>
  import { t, locale } from './i18n.js';
  import { radarGeometry } from './radar.js';

  let { values, labels, metric = t('h') } = $props();
  const fmt = value => new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(value);
  let geometry = $derived(radarGeometry(values));
  let points = $derived(geometry.axes.slice(0, values.length).map(axis => `${280 + axis.valueX * 150},${240 + axis.valueY * 150}`).join(' '));
  let description = $derived(labels.map((label, index) => `${label}: ${fmt(values[index])} ${metric}`).join('; '));
</script>

<figure class="radar-chart">
  <figcaption>{t('Genre radar chart')}</figcaption>
  <div class="radar-plot">
  <svg viewBox="0 0 560 480" role="img" aria-label={description}>
    <title>{t('Genre radar chart')}</title>
    {#each [0.25, 0.5, 0.75, 1] as level}
      <polygon class="grid-ring" points={geometry.axes.map(axis => `${280 + axis.x * 150 * level},${240 + axis.y * 150 * level}`).join(' ')} />
      <text class="scale" x="286" y={240 - 150 * level + 12}>{fmt(geometry.max * level)} {metric}</text>
    {/each}
    {#each geometry.axes as axis}
      <line class="axis" x1="280" y1="240" x2={280 + axis.x * 150} y2={240 + axis.y * 150} />
    {/each}
    {#if values.length >= 3}
      <polygon class="radar-area" {points} />
    {:else}
      <polyline class="radar-line" {points} />
    {/if}
    {#each values as value, index}
      {@const axis = geometry.axes[index]}
      <circle class="radar-point" cx={280 + axis.valueX * 150} cy={240 + axis.valueY * 150} r="4">
        <title>{labels[index]}: {fmt(value)} {metric}</title>
      </circle>
    {/each}
  </svg>
  {#each values as value, index}
    {@const axis = geometry.axes[index]}
    <div class="axis-label" style:left="{50 + axis.x * 194 / 560 * 100}%" style:top="{50 + axis.y * 194 / 480 * 100}%" aria-hidden="true" title={labels[index]}>
      <b>{labels[index]}</b><span>{fmt(value)} {metric}</span>
    </div>
  {/each}
  </div>
</figure>

<style>
  .radar-chart { width: 100%; max-width: 850px; margin: 36px 0 0; }
  figcaption { color: var(--accent); font-size: 1rem; font-weight: 700; }
  .radar-plot { position: relative; max-width: 650px; margin: 0 auto; }
  svg { display: block; width: 100%; height: auto; }
  .grid-ring { fill: none; stroke: currentColor; stroke-opacity: .18; }
  .axis { stroke: currentColor; stroke-opacity: .15; }
  .radar-area { fill: var(--accent); fill-opacity: .16; stroke: var(--accent); filter: drop-shadow(0 0 8px rgba(var(--accent-rgb), .3)); stroke-width: 2; stroke-linejoin: round; }
  .radar-line { fill: none; stroke: var(--accent); stroke-width: 2; }
  .radar-point { fill: var(--accent); filter: drop-shadow(0 0 5px var(--accent)); }
  .scale { fill: currentColor; font-size: 10px; opacity: .65; }
  .axis-label { position: absolute; transform: translate(-50%, -50%); width: 23%; display: flex; flex-direction: column; gap: 3px; text-align: center; font-size: clamp(10px, 1.3vw, 14px); line-height: 1.2; overflow-wrap: anywhere; }
  .axis-label b { display: -webkit-box; line-clamp: 2; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
  .axis-label span { font-size: .85em; }
  @media (max-width: 760px) { .scale { font-size: 18px; } }
</style>
