<script lang="ts">
  import GraphCanvas from '$lib/components/graph/GraphCanvas.svelte';
  import type { GraphNode, GraphEdge } from '$lib/components/graph/GraphCanvas.svelte';
  import { onMount } from 'svelte';

  // Golden angle color generation in OKLCH space.
  // φ = (1+√5)/2, golden_angle = 360/φ² ≈ 137.508°
  // Perceptually uniform, no gray/white/black (C≥0.12, L∈[0.55,0.85])
  const GOLDEN_ANGLE = 137.508;
  const colorCache = new Map<string, string>();
  let colorIndex = 0;

  function goldenColor(seed: string): string {
    if (colorCache.has(seed)) return colorCache.get(seed)!;
    // Hash the seed string to get a stable starting index
    let hash = 0;
    for (let i = 0; i < seed.length; i++) hash = ((hash << 5) - hash + seed.charCodeAt(i)) | 0;
    const idx = Math.abs(hash);
    const hue = (idx * GOLDEN_ANGLE) % 360;
    const L = 0.72;  // bright enough on dark bg, not white
    const C = 0.14;  // vivid enough, never gray
    const hex = oklchToHex(L, C, hue);
    colorCache.set(seed, hex);
    return hex;
  }

  function oklchToHex(L: number, C: number, h: number): string {
    // Convert OKLCH → OKLab → linear sRGB → sRGB → hex
    const hRad = h * Math.PI / 180;
    const a = C * Math.cos(hRad);
    const b = C * Math.sin(hRad);
    // OKLab to linear RGB via approximate matrix
    const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
    const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
    const s_ = L - 0.0894841775 * a - 1.2914855480 * b;
    const l3 = l_ * l_ * l_;
    const m3 = m_ * m_ * m_;
    const s3 = s_ * s_ * s_;
    let r = +4.0767416621 * l3 - 3.3077115913 * m3 + 0.2309699292 * s3;
    let g = -1.2684380046 * l3 + 2.6097574011 * m3 - 0.3413193965 * s3;
    let bl = -0.0041960863 * l3 - 0.7034186147 * m3 + 1.7076147010 * s3;
    // Linear to sRGB gamma
    const gamma = (x: number) => x <= 0.0031308 ? 12.92 * x : 1.055 * Math.pow(x, 1/2.4) - 0.055;
    r = Math.round(Math.max(0, Math.min(1, gamma(r))) * 255);
    g = Math.round(Math.max(0, Math.min(1, gamma(g))) * 255);
    bl = Math.round(Math.max(0, Math.min(1, gamma(bl))) * 255);
    return '#' + [r, g, bl].map(v => v.toString(16).padStart(2, '0')).join('');
  }

  const REL_COLORS: Record<string, string> = {
    'cross-scope': '#4a4a6a', parent_of: '#4a4a6a',
    depends_on: '#d47a3a', implements: '#6bc88a',
    blocks: '#d45a7a', justifies: '#8b6bb5',
    cites: '#8b6bb5', documents: '#5b9bd5',
  };

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
        color: goldenColor(raw.scope || raw.name || raw.id),
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

    const parentId = `project:${scopeName}`;
    const parentIdx = nodes.findIndex(n => n.id === parentId);
    if (parentIdx < 0) return;
    const parent = nodes[parentIdx];
    const cx = parent.x;
    const cy = parent.y;
    const originalSize = parent.size;

    // Ghost node: stays at original size with low opacity (sphere of influence)
    const ghostNode: GraphNode = {
      id: `ghost:${scopeName}`,
      label: '',
      x: cx, y: cy,
      size: originalSize,
      color: parent.color + '20', // ~12% opacity via hex alpha
      kind: 'ghost',
    };

    // Shrink parent: loses mass of children
    const childCount = data.nodes.length || 1;
    const shrunkSize = Math.max(3, originalSize * 0.3);

    // Children orbit within the ghost's radius
    const orbitRadius = originalSize * 0.7;
    const childGoldenAngle = 137.508 * Math.PI / 180;

    const newNodes: GraphNode[] = data.nodes.map((raw: any, i: number) => {
      const angle = i * childGoldenAngle;
      const r = orbitRadius * Math.sqrt((i + 0.5) / childCount);
      return {
        id: raw.id,
        label: raw.name,
        x: cx + r * Math.cos(angle),
        y: cy + r * Math.sin(angle),
        size: Math.max(2, Math.cbrt(raw.val || 1) * 1.2),
        color: goldenColor(raw.kind || raw.name),
        kind: raw.kind,
      };
    });

    const newEdges: GraphEdge[] = [
      ...data.links.map((raw: any) => ({
        source: raw.source, target: raw.target,
        color: REL_COLORS[raw.relation] || '#4a4a6a',
      })),
      ...newNodes.map(n => ({
        source: parentId, target: n.id, color: parent.color + '40',
      })),
    ];

    // Update parent size (shrink) and add ghost + children
    const updatedNodes = nodes.map((n, i) =>
      i === parentIdx ? { ...n, size: shrunkSize } : n
    );

    nodes = [...updatedNodes, ghostNode, ...newNodes];
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
