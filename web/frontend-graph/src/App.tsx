import { useEffect, useState, useRef, useCallback } from 'react';
import { GraphCanvas, lightTheme } from 'reagraph';
import type { GraphCanvasRef, GraphNode, GraphEdge } from 'reagraph';

// SiYuan-inspired vivid color palette — distinct, high-contrast on dark bg.
const KIND_COLORS: Record<string, string> = {
  project:     '#e8eaed',  // SiYuan doc-point (bright white-gray)
  'kind-group': '#dd79ff', // SiYuan super-point (light purple)
  task:        '#37A2FF',  // SiYuan table-point (bright blue)
  goal:        '#FFBF00',  // SiYuan todo-point (golden yellow)
  campaign:    '#f65b00',  // SiYuan listitem-point (vivid orange)
  note:        '#80FFA5',  // SiYuan math-point (bright green)
  concept:     '#00DDFF',  // SiYuan code-point (electric cyan)
  bug:         '#FF0087',  // SiYuan list-point (hot pink)
  decision:    '#8d48e3',  // SiYuan bq-point (deep purple)
  spec:        '#076f7e',  // SiYuan p-point (teal)
  source:      '#b3005f',  // SiYuan olist-point (magenta)
  doc:         '#00DDFF',  // cyan
  ref:         '#dbf32f',  // SiYuan tag-point (yellow-green)
  need:        '#8d48e3',  // purple
  context:     '#80FFA5',  // green
  journal:     '#FFBF00',  // golden
};

const RELATION_COLORS: Record<string, string> = {
  'cross-scope': '#5f6368',
  parent_of:   '#5f636850',
  depends_on:  '#fb923ccc',
  implements:  '#34d399aa',
  satisfies:   '#34d39960',
  justifies:   '#8b5cf6aa',
  cites:       '#8b5cf660',
  documents:   '#3b82f6aa',
  blocks:      '#ef4444cc',
  relates_to:  '#5f636830',
  mentions:    '#5f636820',
  elaborates:  '#ec4899aa',
  contradicts: '#ef444480',
  synthesises: '#0ea5e9aa',
  remembers:   '#f59e0b60',
};

const scribeDarkTheme = {
  ...lightTheme,
  canvas: { background: '#0a0a1a' },
  node: {
    ...lightTheme.node,
    fill: '#94a3b8',
    activeFill: '#f3a92f',
    opacity: 0.85,
    inactiveOpacity: 0.12,
    label: {
      ...lightTheme.node.label,
      color: '#f1f5f9',
      activeColor: '#ffffff',
      fontSize: 7,
      backgroundColor: '#0a0a1acc',
      backgroundPadding: 2,
    },
  },
  edge: {
    ...lightTheme.edge,
    fill: '#5f636830',
    activeFill: '#4285f4',
    opacity: 0.24,
    label: {
      ...lightTheme.edge.label,
      color: '#64748b',
      fontSize: 5,
    },
  },
  ring: {
    fill: '#f3a92f60',
    activeFill: '#f3a92f',
  },
  arrow: {
    fill: '#64748b',
    activeFill: '#f3a92f',
  },
};

function kindColor(kind: string): string {
  const short = kind?.split('.').pop() || kind;
  return KIND_COLORS[short] || KIND_COLORS[kind] || '#94a3b8';
}

function relationColor(rel: string): string {
  return RELATION_COLORS[rel] || '#5f636840';
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

interface GraphResponse {
  nodes: RawNode[];
  links: RawLink[];
}

function transformGraph(data: GraphResponse): { nodes: GraphNode[]; edges: GraphEdge[] } {
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

async function fetchGraph(url: string): Promise<{ nodes: GraphNode[]; edges: GraphEdge[] }> {
  const res = await fetch(url);
  const data: GraphResponse = await res.json();
  return transformGraph(data);
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
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const graphRef = useRef<GraphCanvasRef>(null);

  // Load initial scope graph
  useEffect(() => {
    fetchGraph('/api/v1/graph/scopes').then(({ nodes: n, edges: e }) => {
      setNodes(n);
      setEdges(e);
      setLoading(false);
    });
  }, []);

  // Expand a scope: fetch kind graph and merge
  const expandScope = useCallback(async (scopeName: string) => {
    if (expanded.has(scopeName)) return;
    const status = 'work.draft,work.active,work.blocked,work.complete,note.fleeting,note.mature,note.evergreen,decision.proposed,decision.accepted,active';
    const url = `/api/v1/graph/kinds?scope=${encodeURIComponent(scopeName)}&status=${encodeURIComponent(status)}`;
    const { nodes: kindNodes, edges: kindEdges } = await fetchGraph(url);

    // Connect kind nodes to their parent scope node
    const scopeNodeId = `project:${scopeName}`;
    const containsEdges: GraphEdge[] = kindNodes.map((kn, i) => ({
      id: `contains-${scopeName}-${i}`,
      source: scopeNodeId,
      target: kn.id,
      label: 'contains',
      fill: '#5f636830',
    }));

    setNodes(prev => [...prev, ...kindNodes]);
    setEdges(prev => [...prev, ...kindEdges, ...containsEdges]);
    setExpanded(prev => new Set(prev).add(scopeName));
  }, [expanded]);

  const handleNodeClick = useCallback((node: GraphNode) => {
    const data = node.data as RawNode;
    if (!data) return;

    if (data.kind === 'project') {
      expandScope(data.scope || data.name);
      return;
    }

    if (data.kind === 'kind-group') {
      // Could expand to artifacts — for now just open sidebar
    }

    // Open sidebar via HTMX
    const el = document.getElementById('sidebar-content');
    if (el && (window as any).htmx) {
      (window as any).htmx.ajax('GET', `/fragments/artifacts/${data.id || node.id}`, el);
    }
    document.getElementById('sidebar')?.classList.add('open');
  }, [expandScope]);

  return (
    <div style={{ width: '100vw', height: '100vh', background: '#05050f' }}>
      {/* Controls */}
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
        {expanded.size > 0 && (
          <div style={{ marginTop: '0.5rem', fontSize: '0.72em', opacity: 0.6 }}>
            Expanded: {[...expanded].join(', ')}
          </div>
        )}
      </div>

      {/* Sidebar */}
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

      {/* Hint */}
      <div style={{
        position: 'fixed', bottom: '1rem', left: '50%', transform: 'translateX(-50%)',
        zIndex: 10, color: 'rgba(255,255,255,0.3)', fontSize: '0.72em', pointerEvents: 'none',
      }}>
        Click project to expand · Drag to rotate · Scroll to zoom
      </div>

      {/* Graph */}
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
          sizingType="attribute"
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
