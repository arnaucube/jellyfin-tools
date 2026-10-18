<script>
  import { t } from './i18n.js';
  let { item } = $props();
  let failed = $state(false);
</script>
<div class="poster-block">
  <div class="poster">
    {#if item.poster && !failed}
      <img src={item.poster} alt={t("Poster for {title}", { title: item.title })} onerror={() => failed = true} loading="lazy" />
    {:else}
      <div class="poster-fallback" aria-label={t("Poster unavailable")}><span>▶</span><small>{item.title}</small></div>
    {/if}
  </div>
  {#if item.posterSource}<a class="poster-credit" href={item.posterSource} target="_blank" rel="noreferrer">{t("TVmaze")}</a>{/if}
</div>

<style>
  .poster-block { flex: none; }
  .poster-credit { display: block; width: fit-content; font-size: 9px; line-height: 1.5; margin-top: 4px; }
</style>
