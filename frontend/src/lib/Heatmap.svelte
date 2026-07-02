<script lang="ts">
  // Presentational GitHub-style commit-activity heatmap.
  // Takes a sparse, ascending list of {date:"YYYY-MM-DD", count} and renders a
  // dense grid of the last `weeks` weeks (columns = weeks, rows = weekdays).
  // The Go side never sends "today"; the browser's current date aligns the grid.
  export let days: Array<{ date: string; count: number }> = [];
  export let weeks: number = 16;

  const MONTHS = [
    "Jan", "Feb", "Mar", "Apr", "May", "Jun",
    "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
  ];
  // Row labels (Sun..Sat); only Mon/Wed/Fri are shown to stay uncluttered.
  const WEEKDAYS = ["", "Mon", "", "Wed", "", "Fri", ""];

  // Local-date formatter matching git's --date=short (avoids UTC day shifts).
  function fmt(d: Date): string {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    return y + "-" + m + "-" + day;
  }

  // Five buckets: 0 = empty, then increasing intensity.
  function level(count: number): number {
    if (count <= 0) return 0;
    if (count <= 2) return 1;
    if (count <= 4) return 2;
    if (count <= 7) return 3;
    return 4;
  }

  type Cell = { date: string; count: number; level: number; future: boolean };
  type Col = { month: number; cells: Cell[] };

  function buildGrid(input: Array<{ date: string; count: number }>, w: number): Col[] {
    const n = w > 0 ? Math.floor(w) : 16;
    const map = new Map<string, number>();
    for (const d of input || []) {
      if (d && d.date) map.set(d.date, (map.get(d.date) || 0) + (d.count || 0));
    }
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const todayTs = today.getTime();
    // Sunday of the current week is the top of the last column.
    const lastSunday = new Date(today);
    lastSunday.setDate(today.getDate() - today.getDay());
    const firstSunday = new Date(lastSunday);
    firstSunday.setDate(lastSunday.getDate() - (n - 1) * 7);

    const cols: Col[] = [];
    for (let c = 0; c < n; c++) {
      const colStart = new Date(firstSunday);
      colStart.setDate(firstSunday.getDate() + c * 7);
      const cells: Cell[] = [];
      for (let r = 0; r < 7; r++) {
        const cur = new Date(colStart);
        cur.setDate(colStart.getDate() + r);
        const date = fmt(cur);
        const future = cur.getTime() > todayTs;
        const count = future ? 0 : map.get(date) || 0;
        cells.push({ date, count, level: level(count), future });
      }
      cols.push({ month: colStart.getMonth(), cells });
    }
    return cols;
  }

  $: cols = buildGrid(days, weeks);
  // Label a column only when its month differs from the previous column's.
  $: monthLabels = cols.map((c, i) =>
    i === 0 || cols[i - 1].month !== c.month ? MONTHS[c.month] : ""
  );

  function tip(cell: Cell): string {
    if (cell.future) return "";
    return cell.date + ": " + cell.count + (cell.count === 1 ? " commit" : " commits");
  }
</script>

<div class="hm">
  <div class="hm-months">
    {#each monthLabels as label}
      <span class="hm-month">{label}</span>
    {/each}
  </div>

  <div class="hm-body">
    <div class="hm-days">
      {#each WEEKDAYS as label}
        <span class="hm-day">{label}</span>
      {/each}
    </div>

    <div class="hm-grid">
      {#each cols as col}
        {#each col.cells as cell}
          <span
            class="hm-cell l{cell.level}"
            class:future={cell.future}
            title={tip(cell)}
          ></span>
        {/each}
      {/each}
    </div>
  </div>

  <div class="hm-legend">
    <span>Less</span>
    <span class="hm-key">
      <span class="hm-cell l0"></span>
      <span class="hm-cell l1"></span>
      <span class="hm-cell l2"></span>
      <span class="hm-cell l3"></span>
      <span class="hm-cell l4"></span>
    </span>
    <span>More</span>
  </div>
</div>
