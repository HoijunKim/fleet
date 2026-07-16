<script lang="ts">
  // Inline SVG glyph set (no dependency). Lucide-style geometry, hand-authored.
  // Colored via currentColor so it inherits the surrounding text/accent color.
  export let name: string;
  export let size: number = 16;

  // Each entry is the inner markup of a 24x24 viewBox, fill="none",
  // stroke="currentColor". Unknown name -> "" (empty svg, no throw).
  const P: Record<string, string> = {
    search: '<circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/>',
    settings:
      '<circle cx="12" cy="12" r="3.2"/><path d="M12 3v2.5M12 18.5V21M4.2 7l2.2 1.3M17.6 15.7l2.2 1.3M4.2 17l2.2-1.3M17.6 8.3l2.2-1.3"/>',
    external: '<path d="M14 5h5v5"/><path d="M19 5l-9 9"/><path d="M18 13v6H5V6h6"/>',
    "chevron-down": '<path d="M6 9l6 6 6-6"/>',
    activity: '<path d="M4 12h4l2.5-6 3 12 2.5-6H20"/>',
    check: '<path d="M5 12.5l4.5 4.5L19 7"/>',
    x: '<path d="M6 6l12 12M18 6L6 18"/>',
    stop: '<rect x="6" y="6" width="12" height="12" rx="2"/>',
    sparkle: '<path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/>',
    file: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/>',
    terminal: '<path d="M6 8l3.5 3.5L6 15"/><path d="M12.5 16H18"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
    moon: '<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>',
  };
  $: inner = P[name] ?? "";
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.5"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>{@html inner}</svg>
