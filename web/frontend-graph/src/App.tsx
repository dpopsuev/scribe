import { useEffect, useState, useRef, useCallback } from 'react';
import { GraphCanvas, lightTheme } from 'reagraph';
import type { GraphCanvasRef, GraphNode, GraphEdge } from 'reagraph';

const KIND_COLORS: Record<string, string> = {
  project: '#c8d0dc',
  'kind-group': '#a5f3fc',
  task: '#3b82f6',
  goal: '#f59e0b',
  campaign: '#f97316',
  note: '#10b981',
  concept: '#06b6d4',
  bug: '#ef4444',
  decision: '#ec4899',
  spec: '#8b5cf6',
  source: '#64748b',
  doc: '#22d3ee',
  ref: '#94a3b8',
  need: '#a78bfa',
  context: '#34d399',
  journal: '#fbbf24',
};

const RELATION_COLORS: Record<string, string> = {
  'cross-scope': '#4b5563',
  parent_of: '#6b7280',
  depends_on: '#fb923c',
  implements: '#34d399',
  satisfies: '#34d39980',
  justifies: '#8b5cf6',
  cites: '#8b5cf699',
  documents: '#3b82f6',
  blocks: '#ef4444',
  relates_to: '#3b82f650',
  mentions: '#3b82f630',
  elaborates: '#ec4899',
  contradicts: '#ef444490',
  synthesises: '#0ea5e9',
  remembers: '#f59e0b80',
};

const scribeDarkTheme = {
  ...lightTheme,
  canvas: { background: '#05050f', fog: '#05050f' },
  node: {
    ...lightTheme.node,
    fill: '#94a3b8',
    activeFill: '#1DE9AC',
    opacity: 0.95,
    inactiveOpacity: 0.15,
    label: {
      ...lightTheme.node.label,
      color: '#e2e8f0',
      activeColor: '#ffffff',
    },
  },
  edge: {
    ...lightTheme.edge,
    fill: '#4b556340',
    activeFill: '#6366f1cc',
    opacity: 0.36,
    label: {
      ...lightTheme.edge.label,
      color: '#94a3b8',
    },
  },
  ring: {
    fill: '#6366f180',
    activeFill: '#1DE9AC',
  },
  arrow: {
    fill: '#94a3b8',
    activeFill: '#1DE9AC',
  },
};

function kindColor(kind: string): string {
  const short = kind?.split('.').pop() || kind;
  return KIND_COLORS[short] || KIND_COLORS[kind] || '#94a3b8';
}

function relationColor(rel: string): string {
  return RELATION_COLORS[rel] || '#4b556340';
}

interface RawNode {
  id: string;
  name: string;
  kind: string;
  status: string;
  scope: string;
  group?: string;
  val: number;
  violations: number;
}

interface RawLink {
  source: string;
  target: string;
  relation: string;
  weight?: number;
}

async function fetchScopeGraph(): Promise<{ nodes: GraphNode[]; edges: GraphEdge[] }> {
  const res = await fetch('/api/v1/graph/scopes');
  const data: { nodes: RawNode[]; links: RawLink[] } = await res.json();
  return {
    nodes: data.nodes.map((n) => ({
      id: n.id,
      label: n.name,
      fill: kindColor(n.kind),
      size: Math.max(2, Math.cbrt(n.val) * 3),
      data: n,
    })),
    edges: data.links.map((l, i) => ({
      id: `e-${i}`,
      source: l.source,
      target: l.target,
      label: l.relation,
      fill: relationColor(l.relation),
    })),
  };
}

type LayoutType =
  | 'forceDirected2d'
  | 'forceDirected3d'
  | 'hierarchicalTd'
  | 'hierarchicalLr'
  | 'treeTd2d'
  | 'treeLr2d'
  | 'radialOut2d'
  | 'circular2d';

const LAYOUTS: { value: LayoutType; label: string }[] = [
  { value: 'forceDirected3d', label: 'Force 3D' },
  { value: 'forceDirected2d', label: 'Force 2D' },
  { value: 'hierarchicalTd', label: 'Hierarchy (top-down)' },
  { value: 'hierarchicalLr', label: 'Hierarchy (left-right)' },
  { value: 'treeTd2d', label: 'Tree (top-down)' },
  { value: 'treeLr2d', label: 'Tree (left-right)' },
  { value: 'radialOut2d', label: 'Radial' },
  { value: 'circular2d', label: 'Circular' },
];

export default function App() {
  const [nodes, setNodes] = useState<GraphNode[]>([]);
  const [edges, setEdges] = useState<GraphEdge[]>([]);
  const [layout, setLayout] = useState<LayoutType>('forceDirected3d');
  const [loading, setLoading] = useState(true);
  const graphRef = useRef<GraphCanvasRef>(null);

  useEffect(() => {
    fetchScopeGraph().then(({ nodes: n, edges: e }) => {
      setNodes(n);
      setEdges(e);
      setLoading(false);
    });
  }, []);

  const handleNodeClick = useCallback((node: GraphNode) => {
    const data = node.data as RawNode;
    if (!data) return;
    if (data.kind === 'project' || data.kind === 'kind-group') return;
    const el = document.getElementById('sidebar-content');
    if (el && (window as any).htmx) {
      (window as any).htmx.ajax('GET', `/fragments/artifacts/${data.id || node.id}`, el);
    }
    document.getElementById('sidebar')?.classList.add('open');
  }, []);

  return (
    <div style={{ width: '100vw', height: '100vh', background: '#05050f' }}>
      <div style={{
        position: 'fixed', top: '1rem', left: '1rem', zIndex: 10,
        background: 'rgba(5,5,20,0.82)', backdropFilter: 'blur(10px)',
        border: '1px solid rgba(255,255,255,0.1)', borderRadius: '10px',
        padding: '0.8rem 1rem', color: '#e2e8f0', fontSize: '0.82em', minWidth: '210px',
      }}>
        <strong style={{ fontSize: '0.95em', color: '#f1f5f9' }}>Scribe Graph</strong>
        <span style={{
          fontSize: '0.72em', padding: '0.15rem 0.5rem', borderRadius: '3px',
          background: 'rgba(99,102,241,0.3)', border: '1px solid rgba(99,102,241,0.5)',
          color: '#c7d2fe', marginLeft: '0.5rem',
        }}>
          {nodes.length} nodes · {edges.length} edges
        </span>
        <div style={{ marginTop: '0.5rem' }}>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '0.15rem' }}>
            Layout
            <select
              value={layout}
              onChange={(e) => setLayout(e.target.value as LayoutType)}
              style={{
                background: 'rgba(255,255,255,0.08)', border: '1px solid rgba(255,255,255,0.18)',
                color: '#e2e8f0', borderRadius: '5px', padding: '0.2rem 0.45rem', fontSize: '0.88em',
              }}
            >
              {LAYOUTS.map((l) => (
                <option key={l.value} value={l.value}>{l.label}</option>
              ))}
            </select>
          </label>
        </div>
      </div>

      <div id="sidebar" style={{
        position: 'fixed', top: 0, right: 0, width: '370px', height: '100vh',
        background: 'rgba(5,5,20,0.92)', backdropFilter: 'blur(14px)',
        borderLeft: '1px solid rgba(255,255,255,0.1)', zIndex: 10,
        overflowY: 'auto', padding: '1.2rem', color: '#e2e8f0',
        transform: 'translateX(100%)', transition: 'transform 0.2s ease',
      }}>
        <span
          onClick={() => document.getElementById('sidebar')?.classList.remove('open')}
          style={{ position: 'absolute', top: '0.8rem', right: '0.8rem', cursor: 'pointer', fontSize: '1.1em', opacity: 0.5 }}
        >✕</span>
        <div id="sidebar-content">
          <p style={{ opacity: 0.45, fontSize: '0.85em' }}>Click a node to view details</p>
        </div>
      </div>

      {loading ? (
        <div style={{ color: '#94a3b8', display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
          Loading graph...
        </div>
      ) : (
        <GraphCanvas
          ref={graphRef}
          nodes={nodes}
          edges={edges}
          layoutType={layout}
          theme={scribeDarkTheme}
          animated={nodes.length < 500}
          cameraMode={layout.includes('3d') ? 'rotate' : 'pan'}
          labelType="auto"
          onNodeClick={handleNodeClick}
        />
      )}

      <style>{`
        #sidebar.open { transform: translateX(0) !important; }
        #sidebar h3 { color: #f1f5f9; margin-bottom: 0.5rem; }
        #sidebar a { color: #93c5fd; }
      `}</style>
    </div>
  );
}
