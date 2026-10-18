<script>
  import { t, locale } from './i18n.js';

  let { platforms = [], records = 0 } = $props();
  // Normalize existing exports too, keeping their fallback row after named services.
  let rows = $derived(platforms.map(row => ({
    ...row, name: row.name === 'Unknown platform' ? 'Other' : row.name
  })).sort((a, b) => Number(a.name === 'Other') - Number(b.name === 'Other')));
  const count = value => new Intl.NumberFormat(locale).format(value);
  let maximum = $derived(Math.max(1, ...platforms.map(row => row.shows)));
</script>

{#if platforms.length}
  <div class="platform-list">
    <table>
      <caption class="sr-only">{t('TV shows watched by platform')}</caption>
      <thead><tr><th scope="col">{t('Platform')}</th><th scope="col">{t('TV shows')}</th></tr></thead>
      <tbody>
        {#each rows as platform}
          <tr>
            <th scope="row">
              <span>{platform.name === 'Other' ? t('Other') : platform.name}</span>
              <div class="platform-bar" aria-hidden="true" style:width="{100 * platform.shows / maximum}%">
                <span class="shows"></span>
              </div>
            </th>
            <td>{count(platform.shows)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{:else}
  <p class="empty-note">{records ? t('Platform data is unavailable for this review. Regenerate the year to include it.') : t('No TV shows watched this year.')}</p>
{/if}

<style>
  .platform-list { max-height: 26rem; overflow: auto; margin-top: 1.5rem; }
  table { width: 100%; border-collapse: collapse; table-layout: fixed; }
  th, td { padding: .8rem .4rem; border-bottom: 1px solid #ffffff1f; }
  th { text-align: left; }
  thead th { font-size: .8rem; color: var(--muted, #b9bac9); }
  th:first-child { width: 54%; }
  thead th:not(:first-child), td { text-align: right; }
  tbody th { font-weight: 600; overflow-wrap: anywhere; }
  td { color: var(--accent); font-size: 1.25rem; font-weight: 700; font-variant-numeric: tabular-nums; }
  .platform-bar { display: flex; height: .35rem; margin-top: .6rem; border-radius: 1rem; box-shadow: 0 0 12px rgba(var(--accent-rgb), .22); overflow: hidden; }
  .shows { width: 100%; background: linear-gradient(90deg, rgba(var(--accent-rgb), .45), var(--accent)); }
  .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
</style>
