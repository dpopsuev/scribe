/**
 * graph-state.js — All mutable runtime state for the graph UI.
 *
 * Returns a plain object so initGraph creates one per invocation.
 * No module-level lets — state is local to each graph instance,
 * making the graph testable and resettable without page reload.
 */
export function createGraphState() {
  return {
    // ForceGraph3D instance
    graph:         null,

    // Scope drill-down state
    expandedScopes: new Map(),   // scopeName → { nodes, links }
    expandedKinds:  new Map(),   // 'scope:kind' → { nodes, links }
    macroData:      { nodes: [], links: [] },

    // Three.js scene extras
    scopeBubbles:   new Map(),   // key → [innerMesh, wireMesh]
    scopeSpherePos: new Map(),   // scopeName → { x, y, z }
    glowMeshes:     new Map(),   // nodeId → { node, inner, outer }

    // Camera kite anchor (lerps toward weighted CoM)
    virtualCenter:  { x: 0, y: 0, z: 0 },

    // Tick throttle
    tickCount:      0,

    // Double-click detection
    lastClick:      { node: null, time: 0 },

    // DOM element refs (set during initGraph)
    els: {
      modeBadge:     null,
      stats:         null,
      expandedWrap:  null,
      expandedList:  null,
      sidebar:       null,
      sidebarContent: null,
      ctxMenu:       null,
    },
  };
}
