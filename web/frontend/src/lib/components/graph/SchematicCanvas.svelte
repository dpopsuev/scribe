<script lang="ts">
  import { onMount } from 'svelte';
  import type { SchematicLayout, SchematicNode, SchematicEdge, ContainmentBox } from './elk-layout';
  import type { GraphNode } from './GraphCanvas.svelte';

  let {
    layout,
    highlightEdge = null as { source: string; target: string } | null,
    onNodeClick = (_node: GraphNode) => {},
    onNodeHover = (_node: GraphNode | null) => {},
  }: {
    layout: SchematicLayout;
    highlightEdge?: { source: string; target: string } | null;
    onNodeClick?: (node: GraphNode) => void;
    onNodeHover?: (node: GraphNode | null) => void;
  } = $props();

  let canvas: HTMLCanvasElement | undefined = $state();
  let width = $state(800);
  let height = $state(600);
  let hoveredId: string | null = $state(null);
  let mouseX = $state(0);
  let mouseY = $state(0);

  let camX = 0;
  let camY = 0;
  let camZoom = 1;
  let dragging = false;
  let dragStartX = 0;
  let dragStartY = 0;
  let camStartX = 0;
  let camStartY = 0;

  let animFrame = 0;
  let ctx: CanvasRenderingContext2D | null = null;

  function screenToWorld(sx: number, sy: number): { x: number; y: number } {
    return {
      x: (sx - width / 2) / camZoom + camX,
      y: (sy - height / 2) / camZoom + camY,
    };
  }

  function fitView() {
    if (!layout || layout.nodes.length === 0) return;
    const pad = 40;
    const lw = layout.width || 800;
    const lh = layout.height || 600;
    camX = lw / 2;
    camY = lh / 2;
    camZoom = Math.min((width - pad * 2) / lw, (height - pad * 2) / lh, 2.5);
  }

  // ── Drawing ─────────────────────────────────────────────────────────────

  function draw() {
    if (!ctx || !canvas) return;
    const dpr = devicePixelRatio;
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.save();
    ctx.scale(dpr, dpr);

    // Camera transform
    ctx.translate(width / 2, height / 2);
    ctx.scale(camZoom, camZoom);
    ctx.translate(-camX, -camY);

    drawContainers(ctx, layout.containers);
    drawEdges(ctx, layout.edges);
    drawNodes(ctx, layout.nodes);

    ctx.restore();
    animFrame = requestAnimationFrame(draw);
  }

  function drawContainers(c: CanvasRenderingContext2D, boxes: ContainmentBox[]) {
    for (const box of boxes) {
      c.fillStyle = 'rgba(30, 35, 60, 0.35)';
      c.strokeStyle = 'rgba(100, 110, 150, 0.45)';
      c.lineWidth = 1;
      roundRect(c, box.x, box.y, box.width, box.height, 6);
      c.fill();
      c.stroke();

      c.fillStyle = 'rgba(150, 160, 200, 0.65)';
      c.font = '600 11px system-ui, -apple-system, sans-serif';
      c.textBaseline = 'top';
      c.fillText(box.label, box.x + 10, box.y + 8);
    }
  }

  function drawEdges(c: CanvasRenderingContext2D, edges: SchematicEdge[]) {
    for (const edge of edges) {
      const pts = edge.points;
      if (pts.length < 2) continue;

      const isHl = highlightEdge &&
        ((edge.source === highlightEdge.source && edge.target === highlightEdge.target) ||
         (edge.source === highlightEdge.target && edge.target === highlightEdge.source));
      const isConnected = hoveredId && (edge.source === hoveredId || edge.target === hoveredId);

      // Style selection
      if (edge.sameLayer) {
        c.strokeStyle = '#f59e0b';
        c.lineWidth = 2.5;
        c.setLineDash([]);
      } else if (edge.relation === 'implements') {
        c.strokeStyle = isConnected ? 'rgba(180, 190, 220, 0.8)' : 'rgba(130, 140, 170, 0.5)';
        c.lineWidth = isHl ? 2 : 1;
        c.setLineDash([5, 3]);
      } else if (isHl) {
        c.strokeStyle = '#6366f1';
        c.lineWidth = 2.5;
        c.setLineDash([]);
      } else if (isConnected) {
        c.strokeStyle = 'rgba(180, 190, 220, 0.75)';
        c.lineWidth = 1.5;
        c.setLineDash([]);
      } else {
        c.strokeStyle = 'rgba(110, 120, 150, 0.4)';
        c.lineWidth = 1;
        c.setLineDash([]);
      }

      // Draw polyline
      c.beginPath();
      c.moveTo(pts[0].x, pts[0].y);
      for (let i = 1; i < pts.length; i++) {
        c.lineTo(pts[i].x, pts[i].y);
      }
      c.stroke();
      c.setLineDash([]);

      // Arrowhead at end
      const last = pts[pts.length - 1];
      const prev = pts[pts.length - 2];
      drawArrow(c, prev, last, edge.sameLayer ? '#f59e0b' : c.strokeStyle);

      // Coupling smell indicator
      if (edge.sameLayer) {
        const mid = pts[Math.floor(pts.length / 2)];
        c.fillStyle = '#f59e0b';
        c.font = '600 8px system-ui';
        c.textBaseline = 'bottom';
        c.fillText('coupling', mid.x + 4, mid.y - 3);
      }

      // Edge label
      if (edge.label) {
        const mid = pts[Math.floor(pts.length / 2)];
        c.fillStyle = 'rgba(200, 200, 220, 0.65)';
        c.font = '9px system-ui';
        c.textBaseline = 'bottom';
        c.fillText(edge.label, mid.x + 4, mid.y - 2);
      }
    }
  }

  function drawArrow(
    c: CanvasRenderingContext2D,
    from: { x: number; y: number },
    to: { x: number; y: number },
    color: string,
  ) {
    const dx = to.x - from.x;
    const dy = to.y - from.y;
    const len = Math.hypot(dx, dy);
    if (len < 1) return;
    const ux = dx / len;
    const uy = dy / len;
    const sz = 5;

    c.fillStyle = color;
    c.beginPath();
    c.moveTo(to.x, to.y);
    c.lineTo(to.x - ux * sz + uy * sz * 0.5, to.y - uy * sz - ux * sz * 0.5);
    c.lineTo(to.x - ux * sz - uy * sz * 0.5, to.y - uy * sz + ux * sz * 0.5);
    c.closePath();
    c.fill();
  }

  function drawNodes(c: CanvasRenderingContext2D, nodes: SchematicNode[]) {
    for (const node of nodes) {
      const isHovered = node.id === hoveredId;
      const fillAlpha = isHovered ? 0.35 : 0.12;

      // Node fill
      c.fillStyle = hexToRgba(node.color, fillAlpha);
      c.strokeStyle = node.color;
      c.lineWidth = isHovered ? 2 : 1;

      if (node.isInterface) {
        c.setLineDash([5, 3]);
      } else {
        c.setLineDash([]);
      }

      roundRect(c, node.x, node.y, node.width, node.height, 3);
      c.fill();
      c.stroke();
      c.setLineDash([]);

      // Kind badge (top-left)
      const shortKind = node.kind.split('.').pop() || node.kind;
      if (shortKind && shortKind !== node.label) {
        c.fillStyle = hexToRgba(node.color, 0.25);
        const badgeW = shortKind.length * 5.5 + 8;
        roundRect(c, node.x + 3, node.y + 3, badgeW, 13, 2);
        c.fill();
        c.fillStyle = hexToRgba(node.color, 0.85);
        c.font = '600 8px system-ui, -apple-system, sans-serif';
        c.textBaseline = 'top';
        c.fillText(shortKind, node.x + 7, node.y + 5);
      }

      // Node label
      c.fillStyle = isHovered ? '#ffffff' : '#e0e0e0';
      c.font = '12px system-ui, -apple-system, sans-serif';
      c.textBaseline = 'middle';
      const labelY = node.height > 36 ? node.y + 24 : node.y + node.height / 2;
      const maxLabelW = node.width - 16;
      let label = node.label;
      if (c.measureText(label).width > maxLabelW) {
        while (label.length > 3 && c.measureText(label + '...').width > maxLabelW) {
          label = label.slice(0, -1);
        }
        label += '...';
      }
      c.fillText(label, node.x + 8, labelY);

      // Port dots
      for (const port of node.ports) {
        c.fillStyle = port.side === 'WEST' ? '#818cf8' : '#34d399';
        c.beginPath();
        c.arc(port.x + 3, port.y + 3, 3, 0, Math.PI * 2);
        c.fill();
      }
    }
  }

  // ── Helpers ────────────────────────────────────────────────────────────

  function roundRect(
    c: CanvasRenderingContext2D,
    x: number, y: number, w: number, h: number, r: number,
  ) {
    c.beginPath();
    c.moveTo(x + r, y);
    c.lineTo(x + w - r, y);
    c.arcTo(x + w, y, x + w, y + r, r);
    c.lineTo(x + w, y + h - r);
    c.arcTo(x + w, y + h, x + w - r, y + h, r);
    c.lineTo(x + r, y + h);
    c.arcTo(x, y + h, x, y + h - r, r);
    c.lineTo(x, y + r);
    c.arcTo(x, y, x + r, y, r);
    c.closePath();
  }

  function hexToRgba(hex: string, alpha: number): string {
    const h = hex.replace('#', '');
    const r = parseInt(h.substring(0, 2), 16);
    const g = parseInt(h.substring(2, 4), 16);
    const b = parseInt(h.substring(4, 6), 16);
    return `rgba(${r},${g},${b},${alpha})`;
  }

  // ── Hit detection ─────────────────────────────────────────────────────

  function nodeAt(wx: number, wy: number): SchematicNode | null {
    for (let i = layout.nodes.length - 1; i >= 0; i--) {
      const n = layout.nodes[i];
      if (wx >= n.x && wx <= n.x + n.width && wy >= n.y && wy <= n.y + n.height) {
        return n;
      }
    }
    return null;
  }

  function toGraphNode(sn: SchematicNode): GraphNode {
    return {
      id: sn.id,
      label: sn.label,
      x: sn.x,
      y: sn.y,
      size: Math.max(sn.width, sn.height) / 2,
      color: sn.color,
      kind: sn.kind,
      depth: 0,
    };
  }

  // ── Interaction handlers ──────────────────────────────────────────────

  function handleMouseMove(e: MouseEvent) {
    const rect = canvas?.getBoundingClientRect();
    if (!rect) return;

    if (dragging) {
      camX = camStartX - (e.clientX - dragStartX) / camZoom;
      camY = camStartY - (e.clientY - dragStartY) / camZoom;
      return;
    }

    mouseX = e.clientX - rect.left;
    mouseY = e.clientY - rect.top;

    const { x, y } = screenToWorld(mouseX, mouseY);
    const hit = nodeAt(x, y);
    const newId = hit?.id || null;
    if (newId !== hoveredId) {
      hoveredId = newId;
      onNodeHover(hit ? toGraphNode(hit) : null);
    }
  }

  function handleMouseDown(e: MouseEvent) {
    if (e.button === 0) {
      dragging = true;
      dragStartX = e.clientX;
      dragStartY = e.clientY;
      camStartX = camX;
      camStartY = camY;
    }
  }

  function handleMouseUp(e: MouseEvent) {
    if (!dragging) return;
    dragging = false;
    const moved = Math.hypot(e.clientX - dragStartX, e.clientY - dragStartY);
    if (moved < 5) {
      const rect = canvas?.getBoundingClientRect();
      if (!rect) return;
      const sx = e.clientX - rect.left;
      const sy = e.clientY - rect.top;
      const { x, y } = screenToWorld(sx, sy);
      const hit = nodeAt(x, y);
      if (hit) onNodeClick(toGraphNode(hit));
    }
  }

  function handleWheel(e: WheelEvent) {
    e.preventDefault();
    const factor = e.deltaY > 0 ? 0.92 : 1.08;
    camZoom = Math.max(0.1, Math.min(8, camZoom * factor));
  }

  // ── Lifecycle ─────────────────────────────────────────────────────────

  onMount(() => {
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    width = rect.width;
    height = rect.height;
    canvas.width = width * devicePixelRatio;
    canvas.height = height * devicePixelRatio;

    ctx = canvas.getContext('2d');
    canvas.addEventListener('wheel', handleWheel, { passive: false });
    fitView();
    animFrame = requestAnimationFrame(draw);

    const ro = new ResizeObserver(() => {
      if (!canvas) return;
      const r = canvas.getBoundingClientRect();
      width = r.width;
      height = r.height;
      canvas.width = width * devicePixelRatio;
      canvas.height = height * devicePixelRatio;
    });
    ro.observe(canvas);

    return () => {
      cancelAnimationFrame(animFrame);
      canvas!.removeEventListener('wheel', handleWheel);
      ro.disconnect();
    };
  });

  $effect(() => {
    const _l = layout;
    if (_l && ctx) fitView();
  });
</script>

<div style="position:relative;width:100%;height:100%">
  <canvas
    bind:this={canvas}
    onmousemove={handleMouseMove}
    onmousedown={handleMouseDown}
    onmouseup={handleMouseUp}
    style="position:absolute;top:0;left:0;width:100%;height:100%;background:#1a1a2e;cursor:{dragging ? 'grabbing' : hoveredId ? 'pointer' : 'grab'}"
  ></canvas>

  <!-- Legend -->
  <div class="legend">
    <div class="legend-row">
      <span class="swatch" style="background:#818cf8"></span>
      <span>input port</span>
    </div>
    <div class="legend-row">
      <span class="swatch" style="background:#34d399"></span>
      <span>output port</span>
    </div>
    <div class="legend-row">
      <span class="swatch-line dashed"></span>
      <span>interface / implements</span>
    </div>
    <div class="legend-row">
      <span class="swatch-line coupling"></span>
      <span>coupling smell</span>
    </div>
  </div>

  {#if hoveredId}
    {@const node = layout.nodes.find(n => n.id === hoveredId)}
    {#if node}
      <div
        class="tooltip"
        style="left:{mouseX + 16}px;top:{mouseY - 10}px"
      >
        <div style="font-weight:600">{node.label}</div>
        <div style="font-size:10px;opacity:0.6;margin-top:2px">
          {node.kind} · {node.width}x{node.height}
          {#if node.isInterface} · interface{/if}
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .legend {
    position: absolute;
    bottom: 8px;
    right: 8px;
    background: rgba(26, 26, 46, 0.92);
    border: 1px solid rgba(255,255,255,0.1);
    border-radius: 6px;
    padding: 8px 10px;
    font-size: 10px;
    color: #94a3b8;
    display: flex;
    flex-direction: column;
    gap: 4px;
    pointer-events: none;
  }
  .legend-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .swatch {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .swatch-line {
    width: 16px;
    height: 0;
    border-top: 2px solid #94a3b8;
    flex-shrink: 0;
  }
  .swatch-line.dashed {
    border-top-style: dashed;
  }
  .swatch-line.coupling {
    border-top: 2px solid #f59e0b;
  }
  .tooltip {
    position: absolute;
    background: rgba(26,26,46,0.94);
    color: #e0e0e0;
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 12px;
    pointer-events: none;
    max-width: 280px;
    line-height: 1.4;
    border: 1px solid rgba(255,255,255,0.12);
    z-index: 100;
  }
</style>
