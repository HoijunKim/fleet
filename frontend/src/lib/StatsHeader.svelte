<script lang="ts">
  // Slim summary strip shown above the project table (Projects view only).
  // Counts are computed once in App.svelte and passed down, so the same
  // numbers are never recomputed in more than one place.
  export let stats: {
    total: number;
    clean: number;
    dirty: number;
    behind: number;
  } = { total: 0, clean: 0, dirty: 0, behind: 0 };
</script>

<section class="stats">
  <div class="stat-tiles">
    <div class="stat-tile">
      <span class="stat-dot all"></span>
      <span class="stat-num">{stats.total}</span>
      <span class="stat-label">total</span>
    </div>
    <div class="stat-tile">
      <span class="stat-dot ok"></span>
      <span class="stat-num">{stats.clean}</span>
      <span class="stat-label">clean</span>
    </div>
    <div class="stat-tile">
      <span class="stat-dot dirty"></span>
      <span class="stat-num">{stats.dirty}</span>
      <span class="stat-label">dirty</span>
    </div>
    <div class="stat-tile">
      <span class="stat-dot err"></span>
      <span class="stat-num">{stats.behind}</span>
      <span class="stat-label">behind</span>
    </div>
  </div>
</section>
