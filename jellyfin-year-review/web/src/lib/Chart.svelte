<script>
  let { values, labels, metric = 'minutes' } = $props();
  const fmt = (v) => new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(v);
  let max = $derived(Math.max(1, ...values));
</script>
<ol class="chart" aria-label={metric}>
  {#each values as value, index}
    <li><span class="chart-label">{labels[index]}</span><span class="track"><span class="fill" style:width="{Math.max(0, value / max * 100)}%"></span></span><span class="chart-value">{fmt(value)} <small>{metric}</small></span></li>
  {/each}
</ol>
