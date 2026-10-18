<script>
  import Poster from './Poster.svelte';

  let { items, year, kind } = $props();
  const monthNames = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
  let titles = $derived([...items].sort((a, b) => a.title.localeCompare(b.title) || a.id.localeCompare(b.id)));
  let groups = $derived(monthNames.map((name, i) => ({
    month: i + 1, name, items: titles.filter(item => item.months?.includes(i + 1))
  })).filter(group => group.items.length));
  // Keep older exports usable without inventing viewing dates for their titles.
  let undated = $derived(titles.filter(item => !item.months?.length));
</script>

{#if items.length}
  <div class="viewing-timeline" aria-label="{kind} watched by month">
    {#each groups as group (group.month)}
      <section class="timeline-month" aria-label="{group.name} {year}">
        <h3><time datetime="{year}-{String(group.month).padStart(2, '0')}">{group.name} {year}</time></h3>
        <ul class="timeline-titles">
          {#each group.items as item (item.id)}
            <li><Poster {item} /><h4>{item.title}</h4></li>
          {/each}
        </ul>
      </section>
    {/each}
    {#if undated.length}
      <section class="timeline-month" aria-label="Viewing dates unavailable">
        <h3>Viewing dates unavailable</h3>
        <ul class="timeline-titles">
          {#each undated as item (item.id)}
            <li><Poster {item} /><h4>{item.title}</h4></li>
          {/each}
        </ul>
      </section>
    {/if}
  </div>
{:else}
  <p class="story-lede">No {kind.toLowerCase()} watched in {year}.</p>
{/if}

<style>
  .viewing-timeline { margin-top: 32px; padding-left: 24px; border-left: 2px solid currentColor; }
  .timeline-month { position: relative; padding-bottom: 28px; }
  .timeline-month:last-child { padding-bottom: 0; }
  .timeline-month::before { content: ''; position: absolute; width: 10px; height: 10px; border-radius: 50%; background: currentColor; left: -30px; top: 6px; }
  h3 { font-size: 1.2rem; margin-bottom: 16px; }
  .timeline-titles { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 240px), 1fr)); gap: 16px 24px; }
  li { display: flex; align-items: center; gap: 14px; min-width: 0; }
  h4 { margin: 0; font-size: 1rem; line-height: 1.5; overflow-wrap: anywhere; }
  @media (max-width: 760px) { .viewing-timeline { padding-left: 18px; } .timeline-month::before { left: -24px; } }
</style>
