<script>
  import { onMount } from 'svelte';
  import { locale, direction } from '../lib/i18n.js';
  import '../app.css';
  let { children } = $props();
  onMount(() => {
    document.documentElement.lang = locale;
    document.documentElement.dir = direction;
  });
</script>
{@render children()}
