<script lang="ts">
  import GraphCanvas from '$lib/components/graph/GraphCanvas.svelte';
  import type { GraphNode, GraphEdge } from '$lib/components/graph/GraphCanvas.svelte';
  import { onMount } from 'svelte';

  const KIND_COLORS: Record<string, string> = {
    project: '#b0b8c4', 'kind-group': '#b58edb',
    task: '#5b9bd5', goal: '#d4a844', campaign: '#d47a3a',
    note: '#6bc88a', concept: '#4db8c7', bug: '#d45a7a',
    decision: '#8b6bb5', spec: '#4a9097', source: '#a15a7a',
    doc: '#4db8c7', ref: '#a8b84a', need: '#8b6bb5',
    context: '#6bc88a', journal: '#d4a844',
  };

  const REL_COLORS: Record<string, string> = {
    'cross-scope': '#4a4a6a', parent_of: '#4a4a6a',
    depends_on: '#d47a3a', implements: '#6bc88a',
    blocks: '#d45a7a', justifies: '#8b6bb5',
    cites: '#8b6bb5', documents: '#5b9bd5',
  };

  function kindColor(kind: string): string {
    return KIND_COLORS[kind?.split('.').pop() || kind] || KIND_COLORS[kind] || '#8a94a8';
  }

  let nodes: GraphNode[] = $state([]);
  let edges: GraphEdge[] = $state([]);
  let loading = $state(true);
  let expanded = $state(new Set<string>());

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
      };
    });

    edges = data.links.map((raw: any) => ({
      source: raw.source,
      target: raw.target,
      color: REL_COLORS[raw.relation] || '#4a4a6a',
    }));

    loading = false;
  }

  async function expandScope(scopeName: string) {
    if (expanded.has(scopeName)) return;
    const status = 'work.draft,work.active,work.blocked,work.complete,note.fleeting,note.mature,note.evergreen,decision.proposed,decision.accepted,active';
    const res = await fetch(`/api/v1/graph/kinds?scope=${encodeURIComponent(scopeName)}&status=${encodeURIComponent(status)}`);
    const data = await res.json();

    const parent = nodes.find(n => n.id === `project:${scopeName}`);
    const cx = parent?.x || 0;
    const cy = parent?.y || 0;
    const childRadius = 40;

    const newNodes: GraphNode[] = data.nodes.map((raw: any, i: number) => ({
      id: raw.id,
      label: raw.name,
      x: cx + childRadius * Math.cos((2 * Math.PI * i) / data.nodes.length),
      y: cy + childRadius * Math.sin((2 * Math.PI * i) / data.nodes.length),
      size: Math.max(2, Math.cbrt(raw.val) * 1.5),
      color: kindColor(raw.kind),
      kind: raw.kind,
    }));

    const parentId = `project:${scopeName}`;
    const newEdges: GraphEdge[] = [
      ...data.links.map((raw: any) => ({
        source: raw.source, target: raw.target,
        color: REL_COLORS[raw.relation] || '#4a4a6a',
      })),
      ...newNodes.map(n => ({
        source: parentId, target: n.id, color: '#4a4a6a40',
      })),
    ];

    nodes = [...nodes, ...newNodes];
    edges = [...edges, ...newEdges];
    expanded = new Set([...expanded, scopeName]);
  }

  function handleNodeClick(node: GraphNode) {
    if (node.kind === 'project') {
      const scope = node.label || node.id.replace('project:', '');
      expandScope(scope);
    }
  }

  onMount(() => { fetchScopeGraph(); });
</script>

<div class="graph-page">
  {#if loading}
    <div class="loading">Loading graph...</div>
  {:else}
    <div class="controls">
      <strong>Scribe Graph</strong>
      <span class="badge">{nodes.length} nodes · {edges.length} edges</span>
      {#if expanded.size > 0}
        <div class="expanded">Expanded: {[...expanded].join(', ')}</div>
      {/if}
    </div>
    <GraphCanvas {nodes} {edges} onNodeClick={handleNodeClick} />
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
  .controls {
    position: fixed;
    top: 1rem;
    left: 1rem;
    z-index: 10;
    background: rgba(26,26,46,0.88);
    backdrop-filter: blur(10px);
    border: 1px solid rgba(255,255,255,0.1);
    border-radius: 10px;
    padding: 0.8rem 1rem;
    color: #E0E0E0;
    font-size: 0.82em;
    min-width: 180px;
  }
  .badge {
    font-size: 0.72em;
    padding: 0.15rem 0.5rem;
    border-radius: 3px;
    background: rgba(99,102,241,0.3);
    border: 1px solid rgba(99,102,241,0.5);
    color: #c7d2fe;
    margin-left: 0.5rem;
  }
  .expanded {
    margin-top: 0.4rem;
    font-size: 0.72em;
    opacity: 0.6;
  }
</style>
