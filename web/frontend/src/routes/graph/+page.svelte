<script lang="ts">
  import GraphCanvas from '$lib/components/graph/GraphCanvas.svelte';
  import type { GraphNode, GraphEdge } from '$lib/components/graph/GraphCanvas.svelte';
  import SchematicCanvas from '$lib/components/graph/SchematicCanvas.svelte';
  import { computeSchematicLayout, type SchematicLayout } from '$lib/components/graph/elk-layout';
  import Sidebar from '$lib/components/graph/Sidebar.svelte';
  import LensPanel from '$lib/components/graph/LensPanel.svelte';
  import { fetchLenses, fetchLensGraph } from '$lib/api';
  import type { LensInfo } from '$lib/api';
  import { kindColor } from '$lib/colors';
  import { layoutNodes, layoutEdges } from '$lib/components/graph/layout';
  import { filterNodes, filterEdges } from '$lib/components/graph/search';
  import { computePacking } from '$lib/components/graph/packing';
  import { onMount } from 'svelte';

  let nodes: GraphNode[] = $state([]);
  let edges: GraphEdge[] = $state([]);
  let loading = $state(true);
  let expanded = $state(new Set<string>());

  // ── Raw data cache (for schematic layout) ─────────────────────────────
  let rawNodes: any[] = $state([]);
  let rawLinks: any[] = $state([]);
  let schematicLayout: SchematicLayout | null = $state(null);

  // ── Search state ──────────────────────────────────────────────────────
  let searchQuery = $state('');
  let debouncedQuery = $state('');
  let searchInput: HTMLInputElement | undefined = $state();
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;

  $effect(() => {
    const q = searchQuery;
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => { debouncedQuery = q; }, 150);
  });

  let filteredNodes = $derived(filterNodes(nodes, debouncedQuery));
  let filteredEdges = $derived(filterEdges(edges, filteredNodes));

  // ── Mode state ────────────────────────────────────────────────────────
  type MapMode = 'scopes' | 'lens' | 'schematic';
  let activeMode: MapMode = $state('scopes');
  let lenses: LensInfo[] = $state([]);
  let activeLens: string | null = $state(null);
  let lensStats: { traversed: number; edges: number } | null = $state(null);
  let focusedNodeId: string | null = $state(null);

  // ── Sidebar state ─────────────────────────────────────────────────────
  let sidebar: any = $state(null);
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

  // ── Data fetching ─────────────────────────────────────────────────────
  async function fetchScopeGraph() {
    const res = await fetch('/api/v1/graph/scopes');
    const data = await res.json();
    rawNodes = data.nodes;
    rawLinks = data.links;
    nodes = layoutNodes(data.nodes);
    edges = layoutEdges(data.links);
    loading = false;
  }

  async function loadLens(lensId: string) {
    loading = true;
    activeLens = lensId;
    const data = await fetchLensGraph({ context_id: lensId });
    rawNodes = data.nodes;
    rawLinks = data.links;
    nodes = layoutNodes(data.nodes, { minSize: 3, maxSize: 15, spread: 14 });
    edges = layoutEdges(data.links);
    lensStats = { traversed: data.nodes.length, edges: data.links.length };
    expanded = new Set();
    loading = false;
  }

  async function switchToSchematic() {
    loading = true;
    schematicLayout = null;
    try {
      schematicLayout = await computeSchematicLayout(rawNodes, rawLinks);
    } catch (e) {
      console.error('ELK layout failed:', e);
    }
    loading = false;
  }

  // ── Shared expansion logic ─────────────────────────────────────────────
  function expandNode(
    parentId: string,
    childData: { id: string; name: string; kind: string; val?: number }[],
    linkData: { source: string; target: string }[],
  ) {
    if (childData.length === 0) return;
    const parentIdx = nodes.findIndex(n => n.id === parentId);
    if (parentIdx < 0) return;
    const parent = nodes[parentIdx];

    const pack = computePacking(parent.size, childData.length);
    const { childSize, orbitRadius, parentSize } = pack;
    const goldenAngle = 137.508 * Math.PI / 180;

    const newNodes: GraphNode[] = childData.map((raw, i) => {
      const angle = i * goldenAngle;
      const r = orbitRadius * Math.sqrt((i + 0.5) / childData.length);
      return {
        id: raw.id, label: raw.name,
        x: parent.x + r * Math.cos(angle),
        y: parent.y + r * Math.sin(angle),
        size: childSize,
        color: kindColor(raw.kind), kind: raw.kind,
        depth: (parent.depth || 0) + 1,
      };
    });

    const newEdges = [
      ...linkData.map(l => ({ source: l.source, target: l.target, color: '#5a5a7a' })),
      ...newNodes.map(n => ({ source: parentId, target: n.id, color: '#4a4a6a' })),
    ];

    nodes = [
      ...nodes.map((n, i) => i === parentIdx ? { ...n, size: parentSize, color: n.color.substring(0, 7) + '40' } : n),
      ...newNodes,
    ];
    edges = [...edges, ...newEdges];
  }

  async function expandScope(scopeName: string) {
    if (expanded.has(scopeName)) return;
    const status = 'work.draft,work.active,work.blocked,work.complete,note.fleeting,note.mature,note.evergreen,decision.proposed,decision.accepted,active';
    const res = await fetch(`/api/v1/graph/kinds?scope=${encodeURIComponent(scopeName)}&status=${encodeURIComponent(status)}`);
    const data = await res.json();
    expandNode(`project:${scopeName}`, data.nodes, data.links);
    expanded = new Set([...expanded, scopeName]);
  }

  async function expandKindGroup(node: GraphNode) {
    const parts = node.id.replace('kind:', '').split(':');
    if (parts.length < 2) return;
    const scope = parts[0];
    const kindName = parts.slice(1).join(':');
    const status = 'work.draft,work.active,work.blocked,work.complete,note.fleeting,note.mature,note.evergreen,decision.proposed,decision.accepted';
    const res = await fetch(`/api/v1/graph?scope=${encodeURIComponent(scope)}&status=${encodeURIComponent(status)}&max_nodes=200`);
    const data = await res.json();
    const childIds = new Set(data.nodes.filter((n: any) => n.kind === kindName).map((n: any) => n.id));
    const filtered = data.nodes.filter((n: any) => childIds.has(n.id));
    const filteredLinks = data.links.filter((l: any) => childIds.has(l.source) && childIds.has(l.target));
    expandNode(node.id, filtered, filteredLinks);
  }

  // ── Interaction ───────────────────────────────────────────────────────
  function switchMode(mode: MapMode) {
    activeMode = mode;
    focusNode(null);
    if (mode === 'schematic') {
      switchToSchematic();
      return;
    }
    schematicLayout = null;
    if (mode === 'scopes' && activeLens) {
      activeLens = null;
      lensStats = null;
      loading = true;
      fetchScopeGraph();
    }
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

  function handleNodeClick(node: GraphNode) {
    if (node.kind === 'ghost') return;
    if (activeMode === 'lens') {
      focusNode(focusedNodeId === node.id ? null : node.id);
      if (focusedNodeId) openSidebar(node.id);
      return;
    }
    if (node.kind === 'project') {
      expandScope(node.label || node.id.replace('project:', ''));
      return;
    }
    if (node.kind === 'kind-group') {
      expandKindGroup(node);
      return;
    }
    openSidebar(node.id);
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
    <div class="mode-bar">
      <div class="mode-icons">
        <button class="mode-icon" class:active={activeMode === 'scopes'} style="--mode-color: #6366f1" title="Force-directed" onclick={() => switchMode('scopes')}>S</button>
        <button class="mode-icon" class:active={activeMode === 'lens'} style="--mode-color: #ec4899" title="Lens" onclick={() => switchMode('lens')}>L</button>
        <button class="mode-icon" class:active={activeMode === 'schematic'} style="--mode-color: #10b981" title="Schematic (ELK layered)" onclick={() => switchMode('schematic')}>A</button>
      </div>

      <div class="mode-panel">
        <div class="mode-panel-header">
          <span class="mode-label">{activeMode === 'scopes' ? 'Scopes' : activeMode === 'lens' ? 'Lens' : 'Architecture'}</span>
          {#if activeMode === 'schematic' && schematicLayout}
            <span class="badge">{schematicLayout.nodes.length} · {schematicLayout.edges.length}</span>
          {:else}
            <span class="badge">{nodes.length} · {edges.length}</span>
          {/if}
        </div>

        {#if activeMode === 'scopes'}
          <div class="mode-detail">
            {expanded.size > 0 ? `Expanded: ${[...expanded].join(', ')}` : 'Click a scope to drill in'}
          </div>
        {:else if activeMode === 'lens'}
          <LensPanel {lenses} {activeLens} {lensStats} onSelect={loadLens} onLensesChanged={l => { lenses = l; }} />
        {:else}
          <div class="mode-detail">
            ELK layered layout · Y=depth N→S · orthogonal routing
          </div>
        {/if}
      </div>
    </div>

    <div class="search-bar">
      <input
        bind:this={searchInput}
        bind:value={searchQuery}
        type="text"
        placeholder="Filter nodes..."
        spellcheck="false"
        onkeydown={(e) => { if (e.key === 'Escape') { searchQuery = ''; searchInput?.blur(); }}}
      />
      {#if searchQuery}
        <span class="search-count">{filteredNodes.length}/{nodes.length}</span>
        <button class="search-clear" onclick={() => { searchQuery = ''; debouncedQuery = ''; clearTimeout(debounceTimer); }}>✕</button>
      {/if}
    </div>

    {#if focusedNodeId}
      <button class="focus-indicator" onclick={() => focusNode(null)}>Focused · Click to clear</button>
    {/if}

    {#if activeMode === 'schematic' && schematicLayout}
      <SchematicCanvas layout={schematicLayout} {highlightEdge} onNodeClick={handleNodeClick} />
    {:else}
      <GraphCanvas nodes={filteredNodes} edges={filteredEdges} {highlightEdge} onNodeClick={handleNodeClick} />
    {/if}

    <Sidebar
      state={sidebar}
      onNavigate={openSidebar}
      onClose={() => { sidebar = null; }}
      onEdgeHover={e => { highlightEdge = e; }}
    />
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
  .search-bar {
    position: fixed;
    top: 13px;
    right: 13px;
    z-index: 10;
    display: flex;
    align-items: center;
    gap: 6px;
    background: rgba(26,26,46,0.95);
    border: 1px solid rgba(255,255,255,0.12);
    border-radius: 8px;
    padding: 4px 10px;
    transition: border-color 0.15s;
  }
  .search-bar:focus-within {
    border-color: rgba(99,102,241,0.6);
  }
  .search-bar input {
    background: transparent;
    border: none;
    outline: none;
    color: #e2e8f0;
    font-size: 0.875rem;
    width: 180px;
    font-family: inherit;
  }
  .search-bar input::placeholder {
    color: #64748b;
  }
  .search-count {
    font-size: 0.75rem;
    color: #64748b;
    white-space: nowrap;
  }
  .search-clear {
    background: none;
    border: none;
    color: #64748b;
    cursor: pointer;
    font-size: 0.75rem;
    padding: 2px 4px;
  }
  .search-clear:hover {
    color: #e2e8f0;
  }
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
</style>
