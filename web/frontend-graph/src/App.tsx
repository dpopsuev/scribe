import { useEffect, useState, useRef, useCallback } from 'react';
import { GraphCanvas, lightTheme } from 'reagraph';
import type { GraphCanvasRef, GraphNode, GraphEdge } from 'reagraph';
import { Perf } from 'r3f-perf';

// Desaturated palette for dark mode — 70-80% saturation, no vibrating neons.
// Based on SiYuan hues but toned down per WCAG dark-mode research.
const KIND_COLORS: Record<string, string> = {
  project:     '#b0b8c4',  // neutral silver
  'kind-group': '#b58edb', // muted purple
  task:        '#5b9bd5',  // soft blue
  goal:        '#d4a844',  // warm gold
  campaign:    '#d47a3a',  // muted orange
  note:        '#6bc88a',  // soft green
  concept:     '#4db8c7',  // muted cyan
  bug:         '#d45a7a',  // soft rose
  decision:    '#8b6bb5',  // muted violet
  spec:        '#4a9097',  // teal
  source:      '#a15a7a',  // muted magenta
  doc:         '#4db8c7',  // muted cyan
  ref:         '#a8b84a',  // olive-green
  need:        '#8b6bb5',  // muted violet
  context:     '#6bc88a',  // soft green
  journal:     '#d4a844',  // warm gold
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

// Dark theme: #1a1a2e background + #E0E0E0 text per WCAG dark-mode research.
// Avoids pure black (#000) and pure white (#FFF) — both cause halation.
// Contrast ratio ~11:1 for body text (well above 4.5:1 minimum).
const scribeDarkTheme = {
  ...lightTheme,
  canvas: { background: '#1a1a2e' },
  node: {
    ...lightTheme.node,
    fill: '#8a94a8',
    activeFill: '#d4944a',
    opacity: 0.9,
    inactiveOpacity: 0.15,
    label: {
      ...lightTheme.node.label,
      color: '#E0E0E0',
      activeColor: '#F5F5F5',
      fontSize: 6,
    },
  },
  edge: {
    ...lightTheme.edge,
    fill: '#4a4a6a40',
    activeFill: '#5b8dd4',
    opacity: 0.28,
    label: {
      ...lightTheme.edge.label,
      color: '#8a8aaa',
      fontSize: 4,
    },
  },
  ring: {
    fill: '#d4944a50',
    activeFill: '#d4944a',
  },
  arrow: {
    fill: '#6a6a8a',
    activeFill: '#d4944a',
  },
};

// Light theme for daytime use.
const scribeLightTheme = {
  ...lightTheme,
  canvas: { background: '#f8f9fa' },
  node: {
    ...lightTheme.node,
    fill: '#5a6577',
    activeFill: '#c07a2a',
    opacity: 0.9,
    inactiveOpacity: 0.15,
    label: {
      ...lightTheme.node.label,
      color: '#1a1a2e',
      activeColor: '#000000',
      fontSize: 6,
    },
  },
  edge: {
    ...lightTheme.edge,
    fill: '#9ca3af50',
    activeFill: '#3b6bb5',
    opacity: 0.3,
    label: {
      ...lightTheme.edge.label,
      color: '#6b7280',
      fontSize: 4,
    },
  },
  ring: {
    fill: '#c07a2a50',
    activeFill: '#c07a2a',
  },
  arrow: {
    fill: '#9ca3af',
    activeFill: '#c07a2a',
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
  const [darkMode, setDarkMode] = useState(true);
  const graphRef = useRef<GraphCanvasRef>(null);
  const theme = darkMode ? scribeDarkTheme : scribeLightTheme;
  const bgColor = darkMode ? '#1a1a2e' : '#f8f9fa';
  const textColor = darkMode ? '#E0E0E0' : '#1a1a2e';
  const panelBg = darkMode ? 'rgba(26,26,46,0.88)' : 'rgba(248,249,250,0.92)';

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
    <div style={{ width: '100vw', height: '100vh', background: bgColor }}>
      {/* Controls */}
      <div style={{
        position: 'fixed', top: '1rem', left: '1rem', zIndex: 10,
        background: panelBg, backdropFilter: 'blur(10px)',
        border: `1px solid ${darkMode ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.1)'}`,
        borderRadius: '10px',
        padding: '0.8rem 1rem', color: textColor, fontSize: '0.82em', minWidth: '210px',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <strong style={{ fontSize: '0.95em' }}>Scribe Graph</strong>
          <button
            onClick={() => setDarkMode(!darkMode)}
            style={{
              background: 'none', border: `1px solid ${darkMode ? 'rgba(255,255,255,0.2)' : 'rgba(0,0,0,0.2)'}`,
              borderRadius: '4px', padding: '0.1rem 0.4rem', cursor: 'pointer',
              color: textColor, fontSize: '0.75em',
            }}
          >
            {darkMode ? '☀' : '☾'}
          </button>
        </div>
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
                background: darkMode ? 'rgba(255,255,255,0.08)' : 'rgba(0,0,0,0.05)',
                border: `1px solid ${darkMode ? 'rgba(255,255,255,0.18)' : 'rgba(0,0,0,0.15)'}`,
                color: textColor, borderRadius: '5px', padding: '0.2rem 0.45rem', fontSize: '0.88em',
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
        background: panelBg, backdropFilter: 'blur(14px)',
        borderLeft: `1px solid ${darkMode ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.1)'}`, zIndex: 10,
        overflowY: 'auto', padding: '1.2rem', color: textColor,
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
        zIndex: 10, color: darkMode ? 'rgba(255,255,255,0.3)' : 'rgba(0,0,0,0.3)', fontSize: '0.72em', pointerEvents: 'none',
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
          theme={theme}
          animated={false}
          cameraMode={layout.includes('3d') ? 'rotate' : 'pan'}
          labelType="auto"
          sizingType="attribute"
          onNodeClick={handleNodeClick}
        >
          <Perf position="bottom-left" deepAnalyze />
        </GraphCanvas>
      )}

      <style>{`
        #sidebar.open { transform: translateX(0) !important; }
        #sidebar h3 { color: #f1f5f9; margin-bottom: 0.5rem; }
        #sidebar a { color: #93c5fd; }
      `}</style>
    </div>
  );
}
