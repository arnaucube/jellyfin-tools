<script>
  const colors = ['#c5f76b', '#a78bfa', '#ff7869', '#ffd166', '#65d6ed', '#ff86c8'];
  // Spread a short shower across the viewport, with varied timing and tumbling.
  const pieces = Array.from({ length: 90 }, (_, i) => ({
    left: (i * 37) % 100,
    delay: ((i * 13) % 120) / 100,
    duration: 3 + ((i * 17) % 140) / 100,
    drift: ((i * 29) % 240) - 120,
    rotation: (i % 2 ? 1 : -1) * (540 + (i * 31) % 720),
    color: colors[i % colors.length]
  }));
</script>

<div class="confetti" aria-hidden="true">
  {#each pieces as piece, i}
    <span style="left: {piece.left}%; background: {piece.color}; --delay: {piece.delay}s; --duration: {piece.duration}s; --drift: {piece.drift}px; --rotation: {piece.rotation}deg; border-radius: {i % 3 === 0 ? '50%' : '1px'};"></span>
  {/each}
</div>

<style>
  .confetti { position: fixed; inset: 0; z-index: 100; overflow: hidden; pointer-events: none; }
  span { position: absolute; top: -24px; width: 8px; height: 14px; animation: fall var(--duration) var(--delay) linear both; }
  @keyframes fall {
    from { transform: translate3d(0, 0, 0) rotate(0deg); }
    to { transform: translate3d(var(--drift), calc(100dvh + 48px), 0) rotateZ(var(--rotation)) rotateY(720deg); }
  }
  @media (prefers-reduced-motion: reduce) {
    .confetti { display: none; }
  }
</style>
