<script lang="ts">
  import GraphCanvas from '$lib/components/graph/GraphCanvas.svelte';
  import type { GraphNode, GraphEdge } from '$lib/components/graph/GraphCanvas.svelte';
  import { fetchLenses, fetchLensGraph, createLens } from '$lib/api';
  import type { LensInfo } from '$lib/api';
  import { onMount } from 'svelte';
  import { marked } from 'marked';

  marked.setOptions({ breaks: true, gfm: true });

  // Golden angle color generation in OKLCH space — one color per KIND.
  // φ = (1+√5)/2, golden_angle = 360/φ² ≈ 137.508°
  // Perceptually uniform, no gray/white/black (C≥0.12, L∈[0.55,0.85])
  const GOLDEN_ANGLE = 137.508;

  // Stable kind→index mapping so colors are consistent across sessions.
  // Ordered by frequency/importance — most common kinds get the first (most distinct) hues.
  const KIND_ORDER = [
    'project', 'campaign', 'goal', 'task', 'bug', 'note', 'concept',
    'decision', 'spec', 'need', 'source', 'doc', 'ref', 'context',
    'journal', 'kind-group', 'ghost', 'session', 'turn', 'tool-call',
    'interface', 'test', 'config', 'template', 'rule', 'section',
  ];
  const kindIndexMap = new Map(KIND_ORDER.map((k, i) => [k, i]));
  let nextKindIndex = KIND_ORDER.length;

  function kindColor(kind: string): string {
    const short = kind?.split('.').pop() || kind || 'unknown';
    let idx = kindIndexMap.get(short);
    if (idx === undefined) {
      idx = nextKindIndex++;
      kindIndexMap.set(short, idx);
    }
    const hue = (idx * GOLDEN_ANGLE + 60) % 360; // offset 60° so index 0 isn't red
    return oklchToHex(0.72, 0.14, hue);
  }

  function oklchToHex(L: number, C: number, h: number): string {
    const hRad = h * Math.PI / 180;
    const a = C * Math.cos(hRad);
    const b = C * Math.sin(hRad);
    const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
    const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
    const s_ = L - 0.0894841775 * a - 1.2914855480 * b;
    const l3 = l_ * l_ * l_;
    const m3 = m_ * m_ * m_;
    const s3 = s_ * s_ * s_;
    let r = +4.0767416621 * l3 - 3.3077115913 * m3 + 0.2309699292 * s3;
    let g = -1.2684380046 * l3 + 2.6097574011 * m3 - 0.3413193965 * s3;
    let bl = -0.0041960863 * l3 - 0.7034186147 * m3 + 1.7076147010 * s3;
    const gamma = (x: number) => x <= 0.0031308 ? 12.92 * x : 1.055 * Math.pow(x, 1/2.4) - 0.055;
    r = Math.round(Math.max(0, Math.min(1, gamma(r))) * 255);
    g = Math.round(Math.max(0, Math.min(1, gamma(g))) * 255);
    bl = Math.round(Math.max(0, Math.min(1, gamma(bl))) * 255);
    return '#' + [r, g, bl].map(v => v.toString(16).padStart(2, '0')).join('');
  }

  let nodes: GraphNode[] = $state([]);
  let edges: GraphEdge[] = $state([]);
  let loading = $state(true);
  let expanded = $state(new Set<string>());

  // ── Map mode system ─────────────────────────────────────────────────
  type MapMode = 'scopes' | 'lens';

  interface ModeCategory {
    id: MapMode;
    label: string;
    icon: string;
    color: string;
  }

  const MODE_CATEGORIES: ModeCategory[] = [
    { id: 'scopes', label: 'Scopes', icon: 'S', color: '#6366f1' },
    { id: 'lens',   label: 'Lens',   icon: 'L', color: '#ec4899' },
  ];

  let activeMode: MapMode = $state('scopes');
  let lenses: LensInfo[] = $state([]);
  let activeLens: string | null = $state(null);
  let lensStats: { seeds: number; traversed: number; edges: number } | null = $state(null);
  let focusedNodeId: string | null = $state(null);

  let showLensForm = $state(false);
  let lensForm = $state({ title: '', anchor: '', traverse: '', exclude: '', scoreBy: 'edges' });

  async function submitLensForm() {
    if (!lensForm.title || !lensForm.anchor) return;
    const anchors = lensForm.anchor.split(',').map(s => s.trim()).filter(Boolean);
    const traverseRules = lensForm.traverse.split(',').map(t => {
      const parts = t.trim().split(':');
      return { relation: parts[0] || '', direction: parts[1] || 'both', max_depth: parseInt(parts[2]) || 3 };
    }).filter(r => r.relation);
    const excludes = lensForm.exclude ? lensForm.exclude.split(',').map(s => s.trim()).filter(Boolean) : undefined;

    await createLens({
      title: lensForm.title,
      anchor: anchors,
      traverse: traverseRules.length > 0 ? traverseRules : [{ relation: 'depends_on', direction: 'both', max_depth: 3 }],
      exclude: excludes,
      score_by: lensForm.scoreBy,
    });

    lenses = await fetchLenses();
    showLensForm = false;
    lensForm = { title: '', anchor: '', traverse: '', exclude: '', scoreBy: 'edges' };
  }

  function focusNode(id: string | null) {
    focusedNodeId = id;
    nodes = nodes.map(n => {
      if (!focusedNodeId) return { ...n, color: kindColor(n.kind || 'unknown') };
      if (n.id === focusedNodeId) return { ...n, color: '#ffffff' };
      const isConnected = edges.some(e =>
        (e.source === focusedNodeId && e.target === n.id) ||
        (e.target === focusedNodeId && e.source === n.id)
      );
      return { ...n, color: isConnected ? kindColor(n.kind || 'unknown') : kindColor(n.kind || 'unknown').substring(0, 7) + '25' };
    });
  }

  // Sidebar state — wiki-style artifact inspector with clickable linked references
  interface ArtifactDetail {
    id: string;
    title: string;
    labels: string[];
    sections: Array<{ name: string; text: string }>;
    extra: Record<string, any>;
    created_at: string;
    updated_at: string;
  }
  interface EdgeRef {
    from: string;
    to: string;
    relation: string;
    title: string;
    kind: string;
  }
  let sidebar: { art: ArtifactDetail; edges: EdgeRef[]; history: string[] } | null = $state(null);
  let highlightEdge: { source: string; target: string } | null = $state(null);

  async function openSidebar(id: string) {
    const [artRes, edgesRes] = await Promise.all([
      fetch(`/api/v1/artifacts/${encodeURIComponent(id)}`),
      fetch(`/api/v1/artifacts/${encodeURIComponent(id)}/edges`),
    ]);
    if (!artRes.ok) return;
    const art = await artRes.json();
    const edgeList = edgesRes.ok ? await edgesRes.json() : [];
    const history = sidebar ? [...sidebar.history, sidebar.art.id] : [];
    sidebar = { art, edges: edgeList, history };
  }

  function closeSidebar() { sidebar = null; }

  function sidebarBack() {
    if (!sidebar || sidebar.history.length === 0) return;
    const prev = sidebar.history[sidebar.history.length - 1];
    sidebar.history = sidebar.history.slice(0, -1);
    openSidebar(prev);
  }

  async function fetchScopeGraph() {
    const res = await fetch('/api/v1/graph/scopes');
    const data = await res.json();

    const n = data.nodes.length;
    const vals = data.nodes.map((r: any) => r.val || 1);
    const minCbrt = Math.cbrt(Math.min(...vals));
    const maxCbrt = Math.cbrt(Math.max(...vals));
    const range = maxCbrt - minCbrt || 1;

    const MIN_SIZE = 4;   // smallest node in world units
    const MAX_SIZE = 18;  // largest node in world units

    // Initial positions: golden-angle spiral (better than circle for uneven sizes)
    const goldenAngle = Math.PI * (3 - Math.sqrt(5));
    const spreadRadius = Math.sqrt(n) * 12;

    nodes = data.nodes.map((raw: any, i: number) => {
      const t = (Math.cbrt(raw.val || 1) - minCbrt) / range;
      const size = MIN_SIZE + (MAX_SIZE - MIN_SIZE) * t;
      const angle = i * goldenAngle;
      const r = spreadRadius * Math.sqrt((i + 0.5) / n);
      return {
        id: raw.id,
        label: raw.name,
        x: r * Math.cos(angle),
        y: r * Math.sin(angle),
        size,
        color: kindColor(raw.kind),
        kind: raw.kind,
        depth: 0,
        status: raw.status,
        scope: raw.scope,
      } as GraphNode;
    });

    edges = data.links.map((raw: any) => ({
      source: raw.source,
      target: raw.target,
      relation: raw.relation,
      color: '#5a5a7a',
    }));

    loading = false;
  }

  async function expandScope(scopeName: string) {
    if (expanded.has(scopeName)) return;
    const status = 'work.draft,work.active,work.blocked,work.complete,note.fleeting,note.mature,note.evergreen,decision.proposed,decision.accepted,active';
    const res = await fetch(`/api/v1/graph/kinds?scope=${encodeURIComponent(scopeName)}&status=${encodeURIComponent(status)}`);
    const data = await res.json();

    const parentId = `project:${scopeName}`;
    const parentIdx = nodes.findIndex(n => n.id === parentId);
    if (parentIdx < 0) return;
    const parent = nodes[parentIdx];
    const cx = parent.x;
    const cy = parent.y;
    const parentSize = parent.size;
    const parentDepth = parent.depth || 0;

    const childCount = data.nodes.length || 1;
    const maxChildSize = parentSize * 0.25;
    const orbitRadius = parentSize * 0.7;
    const childGoldenAngle = 137.508 * Math.PI / 180;

    const newNodes: GraphNode[] = data.nodes.map((raw: any, i: number) => {
      const angle = i * childGoldenAngle;
      const r = orbitRadius * Math.sqrt((i + 0.5) / childCount);
      const rawSize = Math.max(1, Math.cbrt(raw.val || 1) * 1.2);
      return {
        id: raw.id,
        label: raw.name,
        x: cx + r * Math.cos(angle),
        y: cy + r * Math.sin(angle),
        size: Math.min(rawSize, maxChildSize),
        color: kindColor(raw.kind),
        kind: raw.kind,
        depth: parentDepth + 1,
      };
    });

    const newEdges: GraphEdge[] = [
      ...data.links.map((raw: any) => ({
        source: raw.source, target: raw.target, color: '#5a5a7a',
      })),
      ...newNodes.map(n => ({
        source: parentId, target: n.id, color: '#4a4a6a',
      })),
    ];

    // Make parent hollow (reduce opacity) — no ghost node needed
    const updatedNodes = nodes.map((n, i) =>
      i === parentIdx ? { ...n, color: n.color.substring(0, 7) + '40' } : n
    );

    nodes = [...updatedNodes, ...newNodes];
    edges = [...edges, ...newEdges];
    expanded = new Set([...expanded, scopeName]);
  }

  async function loadLens(lensId: string) {
    loading = true;
    activeLens = lensId;
    const data = await fetchLensGraph({ context_id: lensId });

    const n = data.nodes.length || 1;
    const vals = data.nodes.map(r => r.val || 1);
    const minCbrt = Math.cbrt(Math.min(...vals));
    const maxCbrt = Math.cbrt(Math.max(...vals));
    const range = maxCbrt - minCbrt || 1;
    const goldenAngle = Math.PI * (3 - Math.sqrt(5));
    const spreadRadius = Math.sqrt(n) * 14;

    nodes = data.nodes.map((raw, i) => {
      const t = (Math.cbrt(raw.val || 1) - minCbrt) / range;
      const size = 3 + 12 * t;
      const angle = i * goldenAngle;
      const r = spreadRadius * Math.sqrt((i + 0.5) / n);
      return {
        id: raw.id,
        label: raw.name,
        x: r * Math.cos(angle),
        y: r * Math.sin(angle),
        size,
        color: kindColor(raw.kind),
        kind: raw.kind,
        depth: 0,
        status: raw.status,
        scope: raw.scope,
      } as GraphNode;
    });

    edges = data.links.map(raw => ({
      source: raw.source,
      target: raw.target,
      relation: raw.relation,
      color: '#5a5a7a',
    }));

    lensStats = { seeds: 0, traversed: data.nodes.length, edges: data.links.length };
    expanded = new Set();
    loading = false;
  }

  function switchMode(mode: MapMode) {
    activeMode = mode;
    focusNode(null);

    if (mode === 'scopes' && activeLens) {
      activeLens = null;
      lensStats = null;
      loading = true;
      fetchScopeGraph();
    }
  }

  function handleNodeClick(node: GraphNode) {
    if (node.kind === 'ghost') return;

    if (activeMode === 'lens') {
      focusNode(focusedNodeId === node.id ? null : node.id);
      if (focusedNodeId) openSidebar(node.id);
      return;
    }
    if (node.kind === 'project') {
      const scope = node.label || node.id.replace('project:', '');
      expandScope(scope);
      return;
    }
    if (node.kind === 'kind-group') {
      expandKindGroup(node);
      return;
    }
    openSidebar(node.id);
  }

  async function expandKindGroup(node: GraphNode) {
    // kind-group IDs are "kind:{scope}:{kindName}" — extract scope and kind
    const parts = node.id.replace('kind:', '').split(':');
    if (parts.length < 2) return;
    const scope = parts[0];
    const kindName = parts.slice(1).join(':');
    const status = 'work.draft,work.active,work.blocked,work.complete,note.fleeting,note.mature,note.evergreen,decision.proposed,decision.accepted';
    const res = await fetch(`/api/v1/graph?scope=${encodeURIComponent(scope)}&status=${encodeURIComponent(status)}&max_nodes=200`);
    const data = await res.json();

    // Filter to artifacts of this kind only
    const kindNodes = data.nodes.filter((n: any) => n.kind === kindName);
    if (kindNodes.length === 0) return;

    const cx = node.x;
    const cy = node.y;
    const parentSize = node.size;
    const parentDepth = (node as any).depth || 0;
    const orbitRadius = Math.max(parentSize * 0.7, 8);
    const maxChildSize = parentSize * 0.3;
    const childGoldenAngle = 137.508 * Math.PI / 180;

    const newNodes: GraphNode[] = kindNodes.map((raw: any, i: number) => {
      const angle = i * childGoldenAngle;
      const r = orbitRadius * Math.sqrt((i + 0.5) / kindNodes.length);
      const rawSize = Math.max(1, Math.cbrt(raw.val || 1) * 1.0);
      return {
        id: raw.id,
        label: raw.name,
        x: cx + r * Math.cos(angle),
        y: cy + r * Math.sin(angle),
        size: Math.min(rawSize, maxChildSize),
        color: kindColor(raw.kind),
        kind: raw.kind,
        depth: parentDepth + 1,
      };
    });

    const kindEdges = data.links
      .filter((l: any) => kindNodes.some((n: any) => n.id === l.source || n.id === l.target))
      .map((l: any) => ({ source: l.source, target: l.target, color: '#5a5a7a' }));

    nodes = [...nodes, ...newNodes];
    edges = [...edges, ...kindEdges];
  }

  onMount(() => {
    fetchScopeGraph();
    fetchLenses().then(l => { lenses = l; }).catch(() => {});
  });
</script>

<div class="graph-page">
  {#if loading}
    <div class="loading">Loading graph...</div>
  {:else}
    <!-- Paradox-style mode bar — bottom-left above minimap area -->
    <div class="mode-bar">
      <div class="mode-icons">
        {#each MODE_CATEGORIES as cat}
          <button
            class="mode-icon"
            class:active={activeMode === cat.id}
            style="--mode-color: {cat.color}"
            title={cat.label}
            onclick={() => switchMode(cat.id)}
          >{cat.icon}</button>
        {/each}
      </div>

      <!-- Mode-specific panel (slides out from the bar) -->
      <div class="mode-panel">
        <div class="mode-panel-header">
          <span class="mode-label">{MODE_CATEGORIES.find(c => c.id === activeMode)?.label}</span>
          <span class="badge">{nodes.length} · {edges.length}</span>
        </div>

        {#if activeMode === 'scopes'}
          {#if expanded.size > 0}
            <div class="mode-detail">Expanded: {[...expanded].join(', ')}</div>
          {:else}
            <div class="mode-detail">Click a scope to drill in</div>
          {/if}

        {:else if activeMode === 'lens'}
          {#if lenses.length > 0}
            <div class="lens-list">
              {#each lenses as lens}
                <button
                  class="lens-btn"
                  class:active={activeLens === lens.id}
                  onclick={() => loadLens(lens.id)}
                >{lens.title}</button>
              {/each}
            </div>
            {#if lensStats}
              <div class="mode-detail">{lensStats.traversed} artifacts · {lensStats.edges} edges</div>
            {/if}
          {:else if !showLensForm}
            <div class="mode-detail">No stored lenses</div>
          {/if}

          {#if showLensForm}
            <div class="lens-form">
              <input class="lens-input" placeholder="Lens name" bind:value={lensForm.title} />
              <input class="lens-input" placeholder="Anchor labels (e.g. project:ptp)" bind:value={lensForm.anchor} />
              <input class="lens-input" placeholder="Traverse (e.g. depends_on:both:3)" bind:value={lensForm.traverse} />
              <input class="lens-input" placeholder="Exclude (e.g. status:archived)" bind:value={lensForm.exclude} />
              <select class="lens-input" bind:value={lensForm.scoreBy}>
                <option value="edges">Score: edges</option>
                <option value="pagerank">Score: pagerank</option>
                <option value="recency">Score: recency</option>
                <option value="weight">Score: weight</option>
              </select>
              <div class="lens-form-actions">
                <button class="lens-form-btn create" onclick={submitLensForm}>Create</button>
                <button class="lens-form-btn" onclick={() => { showLensForm = false; }}>Cancel</button>
              </div>
            </div>
          {:else}
            <button class="lens-btn new-lens" onclick={() => { showLensForm = true; }}>+ New Lens</button>
          {/if}
        {/if}
      </div>
    </div>

    <!-- Focus indicator (Victoria 3-style) -->
    {#if focusedNodeId}
      <button class="focus-indicator" onclick={() => focusNode(null)}>
        Focused · Click to clear
      </button>
    {/if}
    <GraphCanvas {nodes} {edges} {highlightEdge} onNodeClick={handleNodeClick} />

    {#if sidebar}
      <div class="sidebar">
        <div class="sidebar-header">
          <div class="sidebar-nav">
            {#if sidebar.history.length > 0}
              <button class="sidebar-back" onclick={sidebarBack}>←</button>
            {/if}
            <button class="sidebar-close" onclick={closeSidebar}>×</button>
          </div>
          <h3>{sidebar.art.title}</h3>
          <div class="sidebar-meta">
            {#each sidebar.art.labels as label}
              {#if label.startsWith('kind:')}
                <span class="tag tag-kind">{label.replace('kind:', '')}</span>
              {:else if label.startsWith('project:')}
                <span class="tag tag-scope">{label.replace('project:', '')}</span>
              {:else if !label.startsWith('encoded:') && !label.startsWith('compliance:')}
                <span class="tag">{label}</span>
              {/if}
            {/each}
          </div>
        </div>

        <div class="sidebar-body">
          {#if sidebar.art.extra?.description}
            <div class="sidebar-section">
              <div class="section-md">{@html marked.parse(sidebar.art.extra.description)}</div>
            </div>
          {/if}

          {#each sidebar.art.sections || [] as section}
            <div class="sidebar-section">
              <h4>{section.name}</h4>
              <div class="section-md">{@html marked.parse(section.text)}</div>
            </div>
          {/each}

          {#if sidebar.edges.length > 0}
            <div class="sidebar-section">
              <h4>Linked References ({sidebar.edges.length})</h4>
              {#each sidebar.edges as edge}
                <button
                  class="edge-link"
                  onclick={() => openSidebar(edge.from === sidebar?.art.id ? edge.to : edge.from)}
                  onmouseenter={() => { highlightEdge = { source: edge.from, target: edge.to }; }}
                  onmouseleave={() => { highlightEdge = null; }}
                >
                  <span class="edge-relation">{edge.relation}</span>
                  <span class="edge-title">{edge.title || (edge.from === sidebar?.art.id ? edge.to : edge.from)}</span>
                  {#if edge.kind}
                    <span class="edge-kind">{edge.kind.split('.').pop()}</span>
                  {/if}
                </button>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .graph-page {
    width: 100%;
    height: 100%;
    position: relative;
    background: #1a1a2e;
  }
  .loading {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: #8a94a8;
  }
  /* ── Paradox-style mode bar ───────────────────────────────── */
  /* Spacing scale (golden ratio from 8px base): 8, 13, 21, 34 */
  /* Fitts' Law: all interactive targets ≥ 44px, spacing ≥ 8px */
  /* Typography: base 14px (0.875rem), scaled by φ for hierarchy */
  .mode-bar {
    position: fixed;
    bottom: 21px;
    left: 13px;
    z-index: 10;
    display: flex;
    gap: 13px;
    align-items: flex-end;
  }
  .mode-icons {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .mode-icon {
    width: 44px;
    height: 44px;
    border-radius: 8px;
    border: 2px solid rgba(255,255,255,0.12);
    background: rgba(26,26,46,0.92);
    color: #94a3b8;
    font-size: 1rem;
    font-weight: 700;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: all 0.15s;
  }
  .mode-icon:hover {
    border-color: var(--mode-color);
    color: var(--mode-color);
    background: rgba(26,26,46,1);
  }
  .mode-icon.active {
    background: var(--mode-color);
    border-color: var(--mode-color);
    color: #fff;
    box-shadow: 0 0 13px color-mix(in srgb, var(--mode-color) 50%, transparent);
  }
  .mode-panel {
    background: rgba(26,26,46,0.95);
    border: 1px solid rgba(255,255,255,0.1);
    border-radius: 10px;
    padding: 13px;
    color: #E0E0E0;
    font-size: 0.875rem;
    min-width: 200px;
    max-width: 280px;
  }
  .mode-panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
  }
  .mode-label {
    font-weight: 600;
    font-size: 0.875rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .badge {
    font-size: 0.75rem;
    padding: 3px 8px;
    border-radius: 4px;
    background: rgba(99,102,241,0.3);
    border: 1px solid rgba(99,102,241,0.5);
    color: #c7d2fe;
  }
  .mode-detail {
    font-size: 0.8125rem;
    opacity: 0.6;
    line-height: 1.5;
  }

  /* ── Lens list ───────────────────────────────────────────── */
  .lens-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .lens-btn {
    text-align: left;
    font-size: 0.8125rem;
    padding: 8px 13px;
    min-height: 36px;
    border-radius: 6px;
    background: rgba(255,255,255,0.04);
    border: 1px solid rgba(255,255,255,0.08);
    color: #94a3b8;
    cursor: pointer;
  }
  .lens-btn:hover {
    border-color: #ec4899;
    color: #f9a8d4;
    background: rgba(236,72,153,0.08);
  }
  .lens-btn.active {
    background: rgba(236,72,153,0.2);
    border-color: #ec4899;
    color: #f9a8d4;
  }

  /* ── Lens create form ────────────────────────────────────── */
  .lens-form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 8px;
  }
  .lens-input {
    font-size: 0.8125rem;
    padding: 8px 13px;
    min-height: 36px;
    border-radius: 6px;
    background: rgba(255,255,255,0.06);
    border: 1px solid rgba(255,255,255,0.12);
    color: #e2e8f0;
    width: 100%;
    box-sizing: border-box;
  }
  .lens-input::placeholder { color: #64748b; }
  .lens-input:focus {
    outline: none;
    border-color: #ec4899;
    box-shadow: 0 0 0 2px rgba(236,72,153,0.15);
  }
  .lens-form-actions {
    display: flex;
    gap: 8px;
    margin-top: 4px;
  }
  .lens-form-btn {
    flex: 1;
    font-size: 0.8125rem;
    padding: 8px 13px;
    min-height: 36px;
    border-radius: 6px;
    background: rgba(255,255,255,0.05);
    border: 1px solid rgba(255,255,255,0.12);
    color: #94a3b8;
    cursor: pointer;
  }
  .lens-form-btn.create {
    background: rgba(236,72,153,0.2);
    border-color: #ec4899;
    color: #f9a8d4;
  }
  .lens-form-btn:hover { border-color: rgba(255,255,255,0.3); }
  .new-lens {
    margin-top: 8px;
    border-style: dashed;
    text-align: center;
    color: #64748b;
  }
  .new-lens:hover {
    color: #f9a8d4;
    border-color: #ec4899;
  }

  /* ── Focus indicator (top-center) ───────────────────────── */
  .focus-indicator {
    position: fixed;
    top: 13px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 10;
    background: rgba(26,26,46,0.95);
    border: 1px solid rgba(255,255,255,0.15);
    border-radius: 8px;
    padding: 8px 21px;
    min-height: 36px;
    color: #94a3b8;
    font-size: 0.8125rem;
    cursor: pointer;
    display: flex;
    align-items: center;
  }
  .focus-indicator:hover {
    color: #e2e8f0;
    border-color: rgba(255,255,255,0.3);
  }
  .sidebar {
    position: fixed;
    top: 0;
    left: 0;
    width: 360px;
    height: 100vh;
    background: rgba(16, 16, 32, 0.97);
    border-right: 1px solid rgba(255,255,255,0.08);
    z-index: 20;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .sidebar-header {
    padding: 1rem;
    border-bottom: 1px solid rgba(255,255,255,0.06);
    flex-shrink: 0;
  }
  .sidebar-header h3 {
    margin: 0.3rem 0 0.5rem;
    font-size: 0.95em;
    color: #e2e8f0;
    line-height: 1.3;
  }
  .sidebar-nav {
    display: flex;
    justify-content: space-between;
  }
  .sidebar-back, .sidebar-close {
    background: none;
    border: 1px solid rgba(255,255,255,0.12);
    color: #94a3b8;
    border-radius: 6px;
    padding: 8px 13px;
    min-width: 36px;
    min-height: 36px;
    cursor: pointer;
    font-size: 1rem;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .sidebar-back:hover, .sidebar-close:hover {
    color: #e2e8f0;
    border-color: rgba(255,255,255,0.3);
    background: rgba(255,255,255,0.05);
  }
  .sidebar-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 0.3rem;
  }
  .tag {
    font-size: 0.75rem;
    padding: 4px 8px;
    border-radius: 4px;
    background: rgba(255,255,255,0.06);
    color: #94a3b8;
    border: 1px solid rgba(255,255,255,0.08);
    line-height: 1.4;
  }
  .tag-kind {
    background: rgba(99,102,241,0.2);
    border-color: rgba(99,102,241,0.4);
    color: #a5b4fc;
  }
  .tag-scope {
    background: rgba(34,197,94,0.15);
    border-color: rgba(34,197,94,0.3);
    color: #86efac;
  }
  .sidebar-body {
    flex: 1;
    overflow-y: auto;
    padding: 0.8rem 1rem;
  }
  .sidebar-section {
    margin-bottom: 1rem;
  }
  .sidebar-section h4 {
    margin: 0 0 0.4rem;
    font-size: 0.78em;
    color: #64748b;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .section-md {
    font-size: 0.82em;
    color: #cbd5e1;
    line-height: 1.5;
    word-break: break-word;
  }
  .section-md :global(h1), .section-md :global(h2), .section-md :global(h3) {
    font-size: 0.95em;
    color: #e2e8f0;
    margin: 0.6rem 0 0.3rem;
  }
  .section-md :global(p) { margin: 0.3rem 0; }
  .section-md :global(ul), .section-md :global(ol) {
    padding-left: 1.2rem;
    margin: 0.3rem 0;
  }
  .section-md :global(li) { margin: 0.15rem 0; }
  .section-md :global(input[type="checkbox"]) {
    margin-right: 0.4rem;
    accent-color: #6366f1;
  }
  .section-md :global(code) {
    background: rgba(255,255,255,0.08);
    padding: 0.1rem 0.3rem;
    border-radius: 3px;
    font-size: 0.9em;
  }
  .section-md :global(pre) {
    background: rgba(0,0,0,0.3);
    padding: 0.5rem;
    border-radius: 4px;
    overflow-x: auto;
    margin: 0.4rem 0;
  }
  .section-md :global(a) {
    color: #818cf8;
    text-decoration: none;
  }
  .section-md :global(a:hover) { text-decoration: underline; }
  .edge-link {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 8px 13px;
    min-height: 40px;
    margin-bottom: 6px;
    background: rgba(255,255,255,0.03);
    border: 1px solid rgba(255,255,255,0.06);
    border-radius: 6px;
    color: #94a3b8;
    cursor: pointer;
    font-size: 0.8125rem;
    text-align: left;
  }
  .edge-link:hover {
    background: rgba(99,102,241,0.12);
    border-color: rgba(99,102,241,0.3);
    color: #c7d2fe;
  }
  .edge-relation {
    font-size: 0.75rem;
    padding: 3px 8px;
    border-radius: 4px;
    background: rgba(255,255,255,0.06);
    color: #64748b;
    flex-shrink: 0;
  }
  .edge-title {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .edge-kind {
    font-size: 0.75rem;
    opacity: 0.5;
    flex-shrink: 0;
  }
</style>
