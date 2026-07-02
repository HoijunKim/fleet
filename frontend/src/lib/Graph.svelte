<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { RepoGraph, AddEdge, RemoveEdge, ListEdges } from "../../wailsjs/go/main/App";
  import { tagColor } from "./pm";
  import { toastError } from "./toasts";

  // Select a repo and switch to the Projects view (opens its detail). For code
  // projects the backend uses id == repo path, and a GraphNode.id is that same
  // repo path, so this id maps straight through to App's selection.
  export let onOpen: (id: string) => void;

  // ---- simulation model ----------------------------------------------------
  // A node is a point with position/velocity; edges are from -> to ("from
  // depends on to"). Everything is plain objects mutated in place; a
  // `nodes = nodes` reassignment each frame drives Svelte re-render.
  type SimNode = {
    id: string;
    name: string;
    tags: string[];
    r: number;
    color: string;
    x: number;
    y: number;
    vx: number;
    vy: number;
    fx: number; // force accumulator (x)
    fy: number; // force accumulator (y)
    pinned: boolean; // held still while dragged
  };
  type SimEdge = { from: string; to: string; manual: boolean; kind: string; id: string };
  type Seg = {
    x1: number;
    y1: number;
    x2: number;
    y2: number;
    from: string;
    to: string;
    manual: boolean;
    kind: string;
    id: string;
  };

  let nodes: SimNode[] = [];
  let edges: SimEdge[] = [];
  let byId = new Map<string, SimNode>();
  let loading = true;

  // Fixed colors for manual-edge kinds (falls back to var(--border) for an
  // unrecognized/blank kind).
  const KIND_COLORS: Record<string, string> = {
    http: "#6ea8fe",
    db: "#f5a623",
    "deploy-after": "#7ee787",
    related: "#d2a8ff",
  };
  const EDGE_KINDS = ["http", "db", "deploy-after", "related"];

  // ---- connect mode (draw/delete manual edges) ------------------------------
  let connectMode = false;
  let pendingFrom: string | null = null;
  let pendingTarget: string | null = null;

  // ---- force-simulation tuning ---------------------------------------------
  // O(n^2) pairwise repulsion is fine for the tens-to-low-hundreds of repos
  // this view targets. The loop is capped so it can never run forever.
  const MAX_ITER = 320; // hard cap on frames per settle run
  const REPULSION = 9000; // Coulomb-like node separation
  const SPRING = 0.03; // edge spring stiffness
  const REST = 96; // edge rest length (world units)
  const CENTER = 0.015; // gentle pull toward origin so it can't drift away
  const DAMPING = 0.82; // velocity retained per step
  const MIN_ALPHA = 0.02; // stop once the layout has cooled this far
  const MAX_STEP = 40; // clamp per-frame displacement (anti-explosion)
  const SETTLE = 0.06; // stop once the busiest node moves less than this

  let alpha = 1; // simulation "temperature"; decays each frame
  let iter = 0; // frames elapsed in the current settle run
  let rafId = 0; // pending animation frame, 0 when idle
  let didFit = false; // one-time auto-fit after the first layout settles
  let destroyed = false;

  // ---- view transform (pan + zoom) -----------------------------------------
  let tx = 0;
  let ty = 0;
  let scale = 1;

  let svgEl: SVGSVGElement | undefined;
  let containerEl: HTMLDivElement | undefined;

  // ---- interaction state ---------------------------------------------------
  let mode: "" | "pan" | "drag" = "";
  let dragNode: SimNode | null = null;
  let last = { x: 0, y: 0 }; // last pointer client position
  let down = { x: 0, y: 0 }; // pointer-down client position (click vs drag)
  let moved = false;
  let hoveredId = "";

  // -------------------------------------------------------------------------
  function inDegrees(ns: SimNode[], es: SimEdge[]): Map<string, number> {
    const deg = new Map<string, number>();
    for (const n of ns) deg.set(n.id, 0);
    for (const e of es) {
      if (deg.has(e.to)) deg.set(e.to, (deg.get(e.to) || 0) + 1);
    }
    return deg;
  }

  // Build the simulation model from a GraphView. Crash-safe on missing/empty
  // fields and drops edges that reference an unknown node. `edgeList` (from
  // ListEdges()) is used only to resolve manual edges back to their id, since
  // RepoGraph()'s edges carry manual/kind but not id.
  function build(view: any, edgeList?: any[]) {
    const rawNodes: any[] = (view && Array.isArray(view.nodes)) ? view.nodes : [];
    const rawEdges: any[] = (view && Array.isArray(view.edges)) ? view.edges : [];
    const rawManual: any[] = Array.isArray(edgeList) ? edgeList : [];

    const ids = new Set<string>();
    const tmp: SimNode[] = [];
    for (const n of rawNodes) {
      if (!n || !n.id || ids.has(n.id)) continue;
      ids.add(n.id);
      tmp.push({
        id: n.id,
        name: n.name || n.id,
        tags: Array.isArray(n.tags) ? n.tags : [],
        r: 12,
        color: "var(--nogit)",
        x: 0,
        y: 0,
        vx: 0,
        vy: 0,
        fx: 0,
        fy: 0,
        pinned: false,
      });
    }

    // Manual edges (from RepoGraph) don't carry their own id, so match them
    // back to ListEdges() by from/to/kind to find the id used for delete. The
    // key is a JSON tuple so a repo path containing spaces (common on Windows)
    // can never make two distinct edges collide into one key.
    const edgeKey = (from: string, to: string, kind: string) =>
      JSON.stringify([from, to, kind]);
    const idMap = new Map<string, string>();
    for (const me of rawManual) {
      if (!me || !me.from || !me.to) continue;
      idMap.set(edgeKey(me.from, me.to, me.kind || ""), me.id || "");
    }

    const es: SimEdge[] = [];
    for (const e of rawEdges) {
      if (!e || !e.from || !e.to) continue;
      if (e.from === e.to) continue; // no self-loops
      if (!ids.has(e.from) || !ids.has(e.to)) continue;
      const manual = !!e.manual;
      const kind = e.kind || "";
      const id = manual ? (idMap.get(edgeKey(e.from, e.to, kind)) || "") : "";
      es.push({ from: e.from, to: e.to, manual, kind, id });
    }

    // Radius scales with in-degree (how many repos depend on this one), so
    // heavily-depended-on repos read as hubs. Color comes from the first tag.
    const deg = inDegrees(tmp, es);
    for (const n of tmp) {
      const d = deg.get(n.id) || 0;
      n.r = 12 + Math.min(Math.sqrt(d) * 5, 22);
      n.color = n.tags.length > 0 ? tagColor(n.tags[0]) : "var(--nogit)";
    }

    // Seed positions deterministically on a circle (index-based angle, never
    // Math.random). A single node sits at the origin. On a reload (add/remove
    // edge) keep the already-settled/dragged position of any node that still
    // exists, so the layout doesn't snap back to the circle on every action;
    // only genuinely-new node ids get the circle seed.
    const prev = byId;
    const count = tmp.length;
    const spread = 60 + count * 12;
    tmp.forEach((n, i) => {
      const old = prev.get(n.id);
      if (old) {
        n.x = old.x;
        n.y = old.y;
        n.vx = old.vx;
        n.vy = old.vy;
        return;
      }
      if (count === 1) {
        n.x = 0;
        n.y = 0;
        return;
      }
      const ang = (i / count) * Math.PI * 2;
      n.x = Math.cos(ang) * spread;
      n.y = Math.sin(ang) * spread;
    });

    nodes = tmp;
    edges = es;
    byId = new Map(tmp.map((n) => [n.id, n]));
  }

  // One integration step. Returns the largest single-node displacement so the
  // loop can stop once the layout has settled.
  function step(): number {
    const n = nodes.length;
    for (const nd of nodes) {
      nd.fx = 0;
      nd.fy = 0;
    }

    // Pairwise repulsion (O(n^2), fine at this scale).
    for (let i = 0; i < n; i++) {
      const a = nodes[i];
      for (let j = i + 1; j < n; j++) {
        const b = nodes[j];
        let dx = a.x - b.x;
        let dy = a.y - b.y;
        let d2 = dx * dx + dy * dy;
        if (d2 < 0.01) {
          // Deterministic nudge for coincident points (no randomness).
          dx = (i - j) * 0.1 + 0.1;
          dy = (i + j) * 0.1 + 0.1;
          d2 = dx * dx + dy * dy;
        }
        const d = Math.sqrt(d2);
        const f = REPULSION / Math.max(d2, 100);
        const ux = dx / d;
        const uy = dy / d;
        a.fx += f * ux;
        a.fy += f * uy;
        b.fx -= f * ux;
        b.fy -= f * uy;
      }
    }

    // Spring attraction along edges (pull toward the rest length).
    for (const e of edges) {
      const a = byId.get(e.from);
      const b = byId.get(e.to);
      if (!a || !b) continue;
      const dx = b.x - a.x;
      const dy = b.y - a.y;
      const d = Math.sqrt(dx * dx + dy * dy) || 0.01;
      const f = SPRING * (d - REST);
      const ux = dx / d;
      const uy = dy / d;
      a.fx += f * ux;
      a.fy += f * uy;
      b.fx -= f * ux;
      b.fy -= f * uy;
    }

    // Centering + integration.
    let maxMove = 0;
    for (const nd of nodes) {
      nd.fx += -nd.x * CENTER;
      nd.fy += -nd.y * CENTER;
      if (nd.pinned) {
        nd.vx = 0;
        nd.vy = 0;
        continue;
      }
      nd.vx = (nd.vx + nd.fx) * DAMPING;
      nd.vy = (nd.vy + nd.fy) * DAMPING;
      let dxs = nd.vx * alpha;
      let dys = nd.vy * alpha;
      if (dxs > MAX_STEP) dxs = MAX_STEP;
      else if (dxs < -MAX_STEP) dxs = -MAX_STEP;
      if (dys > MAX_STEP) dys = MAX_STEP;
      else if (dys < -MAX_STEP) dys = -MAX_STEP;
      nd.x += dxs;
      nd.y += dys;
      const m = Math.abs(dxs) + Math.abs(dys);
      if (m > maxMove) maxMove = m;
    }
    alpha *= 0.985;
    return maxMove;
  }

  function frame() {
    rafId = 0;
    if (destroyed) return;
    const m = step();
    nodes = nodes; // trigger reactive re-render of circles + segments
    iter++;
    if (iter < MAX_ITER && alpha > MIN_ALPHA && m > SETTLE) {
      rafId = requestAnimationFrame(frame);
    } else if (!didFit) {
      fitView();
      didFit = true;
    }
  }

  // Re-warm the layout (e.g. after a drag) so neighbours react, but always
  // within a fresh, capped iteration budget - never a runaway loop.
  function reheat(a = 0.4) {
    if (destroyed) return;
    alpha = Math.max(alpha, a);
    iter = 0;
    if (!rafId) rafId = requestAnimationFrame(frame);
  }

  // ---- pan / zoom / fit ----------------------------------------------------
  function viewSize(): { w: number; h: number } {
    const rect = svgEl ? svgEl.getBoundingClientRect() : null;
    return { w: (rect && rect.width) || 800, h: (rect && rect.height) || 600 };
  }

  function fitView() {
    if (!svgEl || nodes.length === 0) return;
    let minX = Infinity;
    let minY = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    for (const nd of nodes) {
      if (nd.x - nd.r < minX) minX = nd.x - nd.r;
      if (nd.y - nd.r < minY) minY = nd.y - nd.r;
      if (nd.x + nd.r > maxX) maxX = nd.x + nd.r;
      if (nd.y + nd.r > maxY) maxY = nd.y + nd.r;
    }
    const { w: vw, h: vh } = viewSize();
    const pad = 72;
    const w = Math.max(maxX - minX, 1);
    const h = Math.max(maxY - minY, 1);
    let s = Math.min((vw - pad * 2) / w, (vh - pad * 2) / h);
    if (!isFinite(s) || s <= 0) s = 1;
    s = Math.max(0.2, Math.min(s, 2.2));
    scale = s;
    const cx = (minX + maxX) / 2;
    const cy = (minY + maxY) / 2;
    tx = vw / 2 - cx * scale;
    ty = vh / 2 - cy * scale;
  }

  function onWheel(e: WheelEvent) {
    if (!svgEl) return;
    const rect = svgEl.getBoundingClientRect();
    const mx = e.clientX - rect.left;
    const my = e.clientY - rect.top;
    const wx = (mx - tx) / scale;
    const wy = (my - ty) / scale;
    const factor = e.deltaY < 0 ? 1.12 : 1 / 1.12;
    let ns = scale * factor;
    ns = Math.max(0.15, Math.min(ns, 3));
    tx = mx - wx * ns;
    ty = my - wy * ns;
    scale = ns;
  }

  // ---- pointer interactions ------------------------------------------------
  function onBgDown(e: PointerEvent) {
    mode = "pan";
    down = { x: e.clientX, y: e.clientY };
    last = { x: e.clientX, y: e.clientY };
    moved = false;
    if (svgEl) svgEl.setPointerCapture(e.pointerId);
  }

  function onNodeDown(e: PointerEvent, nd: SimNode) {
    e.stopPropagation();
    mode = "drag";
    dragNode = nd;
    nd.pinned = true;
    down = { x: e.clientX, y: e.clientY };
    last = { x: e.clientX, y: e.clientY };
    moved = false;
    if (svgEl) svgEl.setPointerCapture(e.pointerId);
  }

  function onMove(e: PointerEvent) {
    if (!mode) return;
    const dx = e.clientX - last.x;
    const dy = e.clientY - last.y;
    if (Math.abs(e.clientX - down.x) + Math.abs(e.clientY - down.y) > 4) moved = true;
    last = { x: e.clientX, y: e.clientY };
    if (mode === "pan") {
      tx += dx;
      ty += dy;
    } else if (mode === "drag" && dragNode) {
      dragNode.x += dx / scale;
      dragNode.y += dy / scale;
      dragNode.vx = 0;
      dragNode.vy = 0;
      nodes = nodes;
      reheat(0.3);
    }
  }

  function onUp(e: PointerEvent) {
    if (mode === "drag" && dragNode) {
      dragNode.pinned = false;
      if (!moved) {
        if (!connectMode) {
          onOpen(dragNode.id); // a click (no drag) opens the repo
        } else {
          onNodeClickConnect(dragNode.id);
        }
      }
    }
    mode = "";
    dragNode = null;
    if (svgEl) {
      try {
        svgEl.releasePointerCapture(e.pointerId);
      } catch (_) {
        /* pointer already released */
      }
    }
  }

  // ---- connect mode ----------------------------------------------------
  // Node click routing while connectMode is on: first click picks the source
  // (highlighted), second click on a different node opens the kind picker,
  // clicking the source again cancels.
  function onNodeClickConnect(id: string) {
    if (pendingFrom === null) {
      pendingFrom = id;
      pendingTarget = null;
    } else if (id === pendingFrom) {
      pendingFrom = null;
      pendingTarget = null;
    } else {
      pendingTarget = id;
    }
  }

  function toggleConnect() {
    connectMode = !connectMode;
    if (!connectMode) {
      pendingFrom = null;
      pendingTarget = null;
    }
  }

  function cancelPending() {
    pendingFrom = null;
    pendingTarget = null;
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape" && (pendingFrom !== null || pendingTarget !== null)) {
      cancelPending();
    }
  }

  async function chooseKind(kind: string) {
    const from = pendingFrom;
    const to = pendingTarget;
    try {
      if (from && to) {
        const msg = await AddEdge(from, to, kind, "");
        if (msg) toastError(msg);
        else await reload();
      }
    } catch (err) {
      toastError("Add edge: " + (err && (err as any).message ? (err as any).message : String(err)));
    } finally {
      pendingFrom = null;
      pendingTarget = null;
    }
  }

  async function onEdgeClick(s: Seg) {
    if (!connectMode || !s.manual || !s.id) return;
    if (!confirm("Remove this manual edge?")) return;
    try {
      const msg = await RemoveEdge(s.id);
      if (msg) toastError(msg);
      else await reload();
    } catch (err) {
      toastError("Remove edge: " + (err && (err as any).message ? (err as any).message : String(err)));
    }
  }

  // ---- rendered edge geometry ----------------------------------------------
  // Recomputes each frame because it references `nodes` (reassigned per frame).
  // Each segment is trimmed to the node rims so the arrowhead sits at the edge
  // of the target circle rather than under it.
  function computeSegments(_ns: SimNode[], es: SimEdge[]): Seg[] {
    const out: Seg[] = [];
    for (const e of es) {
      const a = byId.get(e.from);
      const b = byId.get(e.to);
      if (!a || !b) continue;
      const dx = b.x - a.x;
      const dy = b.y - a.y;
      const d = Math.sqrt(dx * dx + dy * dy);
      if (d < 1) continue;
      const ux = dx / d;
      const uy = dy / d;
      out.push({
        x1: a.x + ux * a.r,
        y1: a.y + uy * a.r,
        x2: b.x - ux * (b.r + 5),
        y2: b.y - uy * (b.r + 5),
        from: e.from,
        to: e.to,
        manual: e.manual,
        kind: e.kind,
        id: e.id,
      });
    }
    return out;
  }

  $: segments = computeSegments(nodes, edges);

  // -------------------------------------------------------------------------
  // (Re)loads the graph + manual-edge id map and restarts the settle. Used
  // for the initial mount and after every successful AddEdge/RemoveEdge, so
  // it never touches the pan/zoom transform (only onMount centers the view).
  async function reload() {
    let view: any = null;
    try {
      view = await RepoGraph();
    } catch (err) {
      toastError("Load graph: " + (err && (err as any).message ? (err as any).message : String(err)));
    }
    let el: any[] = [];
    try {
      el = (await ListEdges()) || [];
    } catch (_) {
      el = [];
    }
    build(view, el);
    // Restart the settle (mirrors onMount's post-build sim start) so
    // new/removed edges pull the layout without a full remount.
    iter = 0;
    alpha = 1;
    if (!destroyed && nodes.length > 0 && !rafId) {
      rafId = requestAnimationFrame(frame);
    }
  }

  onMount(async () => {
    await reload();
    loading = false;
    // Center the origin on first load; the simulation (already started by
    // reload()) settles and auto-fits itself.
    const { w, h } = viewSize();
    tx = w / 2;
    ty = h / 2;
    scale = 1;
  });

  onDestroy(() => {
    destroyed = true;
    if (rafId) cancelAnimationFrame(rafId);
    rafId = 0;
  });
</script>

<svelte:window on:keydown={onKeyDown} />

<div class="graph" bind:this={containerEl}>
  {#if loading}
    <div class="graph-empty">
      <span class="spinner"></span>
      <span>Loading graph</span>
    </div>
  {:else if nodes.length === 0}
    <div class="graph-empty">
      <span class="graph-empty-title">No repositories</span>
      <span class="graph-empty-sub">Add a git repository root in Settings to see the dependency graph</span>
    </div>
  {:else}
    <svg
      class="graph-svg"
      class:connect={connectMode}
      bind:this={svgEl}
      on:pointerdown={onBgDown}
      on:pointermove={onMove}
      on:pointerup={onUp}
      on:pointercancel={onUp}
      on:wheel|preventDefault={onWheel}
      role="presentation"
    >
      <defs>
        <marker
          id="graph-arrow"
          viewBox="0 0 10 10"
          refX="9"
          refY="5"
          markerWidth="7"
          markerHeight="7"
          orient="auto-start-reverse"
        >
          <path d="M0,0 L10,5 L0,10 z" />
        </marker>
      </defs>

      <g transform="translate({tx} {ty}) scale({scale})">
        {#each segments as s}
          <line
            class="edge"
            class:manual={s.manual}
            class:hot={s.from === hoveredId || s.to === hoveredId}
            x1={s.x1}
            y1={s.y1}
            x2={s.x2}
            y2={s.y2}
            marker-end="url(#graph-arrow)"
            style={s.manual ? `stroke: ${KIND_COLORS[s.kind] || "var(--border)"}` : undefined}
          />
          {#if connectMode && s.manual && s.id}
            <line
              class="edge-hit"
              x1={s.x1}
              y1={s.y1}
              x2={s.x2}
              y2={s.y2}
              stroke="transparent"
              stroke-width="12"
              style="cursor:pointer"
              on:click={() => onEdgeClick(s)}
            />
          {/if}
        {/each}

        {#each nodes as nd (nd.id)}
          <g class="node" class:hot={nd.id === hoveredId} class:pending={nd.id === pendingFrom}>
            <circle
              class="node-dot"
              cx={nd.x}
              cy={nd.y}
              r={nd.r}
              style="fill: {nd.color};"
              on:pointerdown={(e) => onNodeDown(e, nd)}
              on:pointerenter={() => (hoveredId = nd.id)}
              on:pointerleave={() => { if (hoveredId === nd.id) hoveredId = ""; }}
              role="button"
              tabindex="-1"
            />
            <text class="node-label" x={nd.x} y={nd.y + nd.r + 13} text-anchor="middle">
              {nd.name}
            </text>
          </g>
        {/each}
      </g>
    </svg>

    <div class="graph-hud">
      <div class="graph-legend">
        <span class="graph-count">{nodes.length} repos</span>
        <span class="graph-sep"></span>
        <span class="graph-hint-text">
          {#if connectMode}
            {#if pendingFrom}
              pick a target repo (Escape to cancel)
            {:else}
              click a repo to start a connection
            {/if}
          {:else}
            arrows point to dependencies
          {/if}
        </span>
      </div>
      <div class="graph-controls">
        <button class="btn btn-secondary btn-sm" class:active={connectMode} on:click={toggleConnect}>Connect</button>
        <button class="btn btn-secondary btn-sm" on:click={fitView}>Fit</button>
      </div>
    </div>

    {#if connectMode && pendingFrom && pendingTarget}
      <div class="graph-kindpick">
        <span class="graph-kindpick-text">
          connect {byId.get(pendingFrom)?.name || pendingFrom} -> {byId.get(pendingTarget)?.name || pendingTarget}
        </span>
        <div class="graph-kindpick-btns">
          {#each EDGE_KINDS as k}
            <button class="btn btn-secondary btn-sm" on:click={() => chooseKind(k)}>{k}</button>
          {/each}
          <button class="btn btn-secondary btn-sm" on:click={cancelPending}>Cancel</button>
        </div>
      </div>
    {/if}

    {#if edges.length === 0}
      <div class="graph-note">No dependencies detected between repos</div>
    {/if}
  {/if}
</div>

<style>
  .graph {
    flex: 1;
    min-height: 0;
    position: relative;
    overflow: hidden;
    background:
      radial-gradient(1200px 600px at 50% 40%, rgba(110, 168, 254, 0.05), transparent 70%),
      var(--bg);
  }

  .graph-svg {
    width: 100%;
    height: 100%;
    display: block;
    cursor: grab;
    touch-action: none;
  }
  .graph-svg:active {
    cursor: grabbing;
  }
  .graph-svg.connect .node-dot {
    cursor: crosshair;
  }

  /* Edges: subtle by default, brighten when touching the hovered node. Manual
     edges are dashed and colored by kind (see KIND_COLORS / inline style). */
  .edge {
    stroke: var(--border);
    stroke-width: 1.4;
    opacity: 0.85;
    pointer-events: none;
    transition: stroke 120ms ease, opacity 120ms ease;
  }
  .edge.hot {
    stroke: var(--accent);
    opacity: 1;
  }
  .edge.manual {
    stroke-width: 1.8;
    stroke-dasharray: 6 4;
    opacity: 0.9;
  }
  .edge.manual.hot {
    opacity: 1;
  }
  .edge-hit {
    pointer-events: auto;
  }

  :global(#graph-arrow path) {
    fill: var(--faint);
  }

  .node-dot {
    stroke: rgba(13, 15, 19, 0.85);
    stroke-width: 2;
    cursor: pointer;
    transition: stroke 120ms ease, stroke-width 120ms ease;
  }
  .node.hot .node-dot {
    stroke: var(--accent);
    stroke-width: 3;
  }
  .node.pending .node-dot {
    stroke: #e3b341;
    stroke-width: 4;
  }
  .node.pending .node-label {
    fill: #e3b341;
  }

  .node-label {
    fill: var(--text);
    font-family: var(--font-sans);
    font-size: 11px;
    font-weight: 500;
    paint-order: stroke;
    stroke: var(--bg);
    stroke-width: 3px;
    stroke-linejoin: round;
    pointer-events: none;
    user-select: none;
  }
  .node.hot .node-label {
    fill: var(--accent-hover);
  }

  /* Empty / loading states */
  .graph-empty {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    color: var(--faint);
    text-align: center;
    padding: 24px;
  }
  .graph-empty-title {
    font-size: 15px;
    color: var(--muted);
  }
  .graph-empty-sub {
    font-size: 13px;
    color: var(--faint);
    max-width: 380px;
  }

  /* Heads-up display: count + legend + fit control */
  .graph-hud {
    position: absolute;
    top: 14px;
    left: 14px;
    right: 14px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    pointer-events: none;
  }
  .graph-legend {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    padding: 6px 12px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-pill);
    box-shadow: var(--shadow);
    pointer-events: auto;
  }
  .graph-count {
    font-family: var(--font-mono);
    font-size: 12px;
    font-weight: 600;
    color: var(--text);
  }
  .graph-sep {
    width: 1px;
    height: 12px;
    background: var(--border);
  }
  .graph-hint-text {
    font-size: 12px;
    color: var(--muted);
  }
  .graph-controls {
    display: inline-flex;
    gap: 8px;
    pointer-events: auto;
  }
  .graph-controls .btn.active {
    background: var(--accent-soft);
    color: var(--accent);
    border-color: rgba(110, 168, 254, 0.35);
  }

  .graph-note {
    position: absolute;
    bottom: 16px;
    left: 50%;
    transform: translateX(-50%);
    padding: 6px 14px;
    font-size: 12px;
    color: var(--muted);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-pill);
    box-shadow: var(--shadow);
    pointer-events: none;
    white-space: nowrap;
  }

  /* Kind picker: shown once a connect-mode source and target are both picked. */
  .graph-kindpick {
    position: absolute;
    top: 58px;
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 14px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-pill);
    box-shadow: var(--shadow);
    pointer-events: auto;
    z-index: 5;
  }
  .graph-kindpick-text {
    font-size: 12px;
    color: var(--muted);
    white-space: nowrap;
  }
  .graph-kindpick-btns {
    display: inline-flex;
    gap: 6px;
  }
</style>
