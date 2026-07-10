<script lang="ts" context="module">
  let counter = 0;
</script>

<script lang="ts">
  // Fleet brand mark — "Quiet orbit": bold F on a blue gradient squircle with a
  // satellite tracing a short arc. Fixed brand colors (NOT currentColor): a logo
  // keeps its identity in both themes. Unique gradient id per instance.
  export let size: number = 20;
  const gid = "fleetLogo" + ++counter;
</script>

<svg width={size} height={size} viewBox="0 0 96 96" role="img" aria-label="Fleet">
  <defs>
    <linearGradient id={gid} x1="0" y1="0" x2="0.35" y2="1">
      <stop offset="0" stop-color="#7fb2ff" />
      <stop offset="1" stop-color="#3f5fd6" />
    </linearGradient>
  </defs>
  <rect width="96" height="96" rx="24" fill="url(#{gid})" />
  <rect width="96" height="52" rx="24" fill="#ffffff" opacity="0.09" />
  <path d="M40 24 A 30 30 0 0 1 72 50" fill="none" stroke="#ffffff" stroke-width="2.4" opacity="0.38" stroke-linecap="round" />
  <circle cx="72" cy="50" r="5" fill="#ffffff" />
  <g fill="#ffffff">
    <rect x="28" y="30" width="12" height="43" rx="6" />
    <rect x="28" y="30" width="31" height="12" rx="6" />
    <rect x="28" y="47" width="23" height="11" rx="5.5" />
  </g>
</svg>
