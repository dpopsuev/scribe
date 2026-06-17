<script lang="ts">
  import { onMount } from 'svelte';
  import { createProgram, hexToRgb, indexToColor, colorToIndex } from './webgl';
  import { NODE_VERT, NODE_FRAG, EDGE_VERT, EDGE_FRAG, NODE_CORNERS } from './shaders';
  import { forceSimulation, forceLink, forceManyBody, forceCenter, forceCollide } from 'd3-force';

  // N-body gravitational force: F = G * m1 * m2 / r²
  // Nodes with more connections (higher mass) attract others more strongly.
  // Softening parameter prevents singularity at r=0.
  function forceGravity(G: number, softening: number) {
    let nodes: any[] = [];
    function force(alpha: number) {
      for (let i = 0; i < nodes.length; i++) {
        for (let j = i + 1; j < nodes.length; j++) {
          const a = nodes[i], b = nodes[j];
          const dx = (b.x || 0) - (a.x || 0);
          const dy = (b.y || 0) - (a.y || 0);
          const distSq = dx * dx + dy * dy + softening * softening;
          const dist = Math.sqrt(distSq);
          const massA = (a._size || 5) * 0.5;
          const massB = (b._size || 5) * 0.5;
          const F = G * massA * massB / distSq * alpha;
          const fx = F * dx / dist;
          const fy = F * dy / dist;
          a.vx = (a.vx || 0) + fx / massA;
          a.vy = (a.vy || 0) + fy / massA;
          b.vx = (b.vx || 0) - fx / massB;
          b.vy = (b.vy || 0) - fy / massB;
        }
      }
    }
    force.initialize = (n: any[]) => { nodes = n; };
    return force;
  }

  export interface GraphNode {
    id: string;
    label: string;
    x: number;
    y: number;
    size: number;
    color: string;
    kind?: string;
  }

  export interface GraphEdge {
    source: string;
    target: string;
    color: string;
  }

  let {
    nodes = [] as GraphNode[],
    edges = [] as GraphEdge[],
    background = '#1a1a2e',
    onNodeClick = (_node: GraphNode) => {},
    onNodeHover = (_node: GraphNode | null) => {},
  }: {
    nodes: GraphNode[];
    edges: GraphEdge[];
    background?: string;
    onNodeClick?: (node: GraphNode) => void;
    onNodeHover?: (node: GraphNode | null) => void;
  } = $props();

  let canvas: HTMLCanvasElement | undefined = $state();
  let labelCanvas: HTMLCanvasElement | undefined = $state();
  let tooltipEl: HTMLDivElement | undefined = $state();
  let hoveredIndex: number = $state(-1);
  let mouseX = $state(0);
  let mouseY = $state(0);
  let width = $state(800);
  let height = $state(600);

  let camX = $state(0);
  let camY = $state(0);
  let camZoom = $state(1);
  let dragging = $state(false);
  let dragStartX = 0;
  let dragStartY = 0;
  let camStartX = 0;
  let camStartY = 0;

  let gl: WebGL2RenderingContext | null = null;
  let nodeProg: WebGLProgram | null = null;
  let edgeProg: WebGLProgram | null = null;
  let nodeVAO: WebGLVertexArrayObject | null = null;
  let edgeVAO: WebGLVertexArrayObject | null = null;
  let nodeInstanceBuf: WebGLBuffer | null = null;
  let edgeBuf: WebGLBuffer | null = null;
  let cornerBuf: WebGLBuffer | null = null;
  let pickFBO: WebGLFramebuffer | null = null;
  let pickTex: WebGLTexture | null = null;
  let animFrame = 0;
  let nodeCount = 0;
  let edgeVertCount = 0;
  let needsPickRedraw = true;
  let simulation: any = null;
  let simNodes: any[] = [];
  let simLinks: any[] = [];

  function fitCamera() {
    if (simNodes.length === 0) return;
    let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
    for (const n of simNodes) {
      const s = n._size || 5;
      if ((n.x || 0) - s < minX) minX = (n.x || 0) - s;
      if ((n.x || 0) + s > maxX) maxX = (n.x || 0) + s;
      if ((n.y || 0) - s < minY) minY = (n.y || 0) - s;
      if ((n.y || 0) + s > maxY) maxY = (n.y || 0) + s;
    }
    const pad = 1.15;
    camX = (minX + maxX) / 2;
    camY = (minY + maxY) / 2;
    const spanX = (maxX - minX) * pad || 100;
    const spanY = (maxY - minY) * pad || 100;
    camZoom = Math.min(width / spanX, height / spanY);
    needsPickRedraw = true;
  }

  function buildViewMatrix(): Float32Array {
    const sx = 2 * camZoom / width;
    const sy = 2 * camZoom / height;
    const tx = -camX * sx;
    const ty = camY * sy;
    return new Float32Array([sx, 0, 0, 0, sy, 0, tx, ty, 1]);
  }

  function setupPickFBO() {
    if (!gl || !canvas) return;
    pickFBO = gl.createFramebuffer();
    pickTex = gl.createTexture();
    gl.bindFramebuffer(gl.FRAMEBUFFER, pickFBO);
    gl.bindTexture(gl.TEXTURE_2D, pickTex);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, canvas.width, canvas.height, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, pickTex, 0);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
  }

  function uploadNodes() {
    if (!gl || !nodeInstanceBuf || simNodes.length === 0) return;
    const stride = 20; // x(f), y(f), size(f), color(4ub), pickColor(4ub)
    const buf = new ArrayBuffer(simNodes.length * stride);
    const floats = new Float32Array(buf);
    const bytes = new Uint8Array(buf);

    for (let i = 0; i < simNodes.length; i++) {
      const n = simNodes[i];
      const off = i * stride;
      const fOff = off / 4;
      floats[fOff] = n.x || 0;
      floats[fOff + 1] = n.y || 0;
      floats[fOff + 2] = n._size || 5;
      const [r, g, b] = hexToRgb(n._color || '#8a94a8');
      bytes[off + 12] = r;
      bytes[off + 13] = g;
      bytes[off + 14] = b;
      bytes[off + 15] = 230;
      const [pr, pg, pb, pa] = indexToColor(i);
      bytes[off + 16] = pr;
      bytes[off + 17] = pg;
      bytes[off + 18] = pb;
      bytes[off + 19] = pa;
    }

    gl.bindBuffer(gl.ARRAY_BUFFER, nodeInstanceBuf);
    gl.bufferData(gl.ARRAY_BUFFER, buf, gl.DYNAMIC_DRAW);
    nodeCount = simNodes.length;
    needsPickRedraw = true;
  }

  function uploadEdges() {
    if (!gl || !edgeBuf || simLinks.length === 0) return;
    // Simple GL_LINES: 2 vertices per edge, each vertex = x(f), y(f), r(ub), g(ub), b(ub), a(ub) = 12 bytes
    const stride = 12;
    const buf = new ArrayBuffer(simLinks.length * 2 * stride);
    const floats = new Float32Array(buf);
    const bytes = new Uint8Array(buf);

    for (let i = 0; i < simLinks.length; i++) {
      const l = simLinks[i];
      const s = l.source;
      const t = l.target;
      if (!s || !t) continue;
      const [r, g, b] = hexToRgb(l._color || '#4a4a6a');

      // Vertex 1 (source)
      const off1 = (i * 2) * stride;
      floats[off1 / 4] = s.x || 0;
      floats[off1 / 4 + 1] = s.y || 0;
      bytes[off1 + 8] = r;
      bytes[off1 + 9] = g;
      bytes[off1 + 10] = b;
      bytes[off1 + 11] = 80;

      // Vertex 2 (target)
      const off2 = (i * 2 + 1) * stride;
      floats[off2 / 4] = t.x || 0;
      floats[off2 / 4 + 1] = t.y || 0;
      bytes[off2 + 8] = r;
      bytes[off2 + 9] = g;
      bytes[off2 + 10] = b;
      bytes[off2 + 11] = 80;
    }

    gl.bindBuffer(gl.ARRAY_BUFFER, edgeBuf);
    gl.bufferData(gl.ARRAY_BUFFER, buf, gl.DYNAMIC_DRAW);
    edgeVertCount = simLinks.length * 2;
  }

  function startSimulation() {
    const nodeMap = new Map<string, any>();
    simNodes = nodes.map(n => {
      const sn = { id: n.id, _size: n.size, _color: n.color, _kind: n.kind, _label: n.label, x: n.x, y: n.y };
      nodeMap.set(n.id, sn);
      return sn;
    });
    simLinks = edges.map(e => ({ source: e.source, target: e.target, _color: e.color }))
      .filter(e => nodeMap.has(e.source) && nodeMap.has(e.target));

    // Compute ideal layout area: nodes should fill ~25% of total area
    const totalNodeArea = simNodes.reduce((s, n) => s + Math.PI * ((n._size || 5) ** 2), 0);
    const idealRadius = Math.sqrt(totalNodeArea / (0.25 * Math.PI)) * 1.5;
    const avgSize = simNodes.reduce((s, n) => s + (n._size || 5), 0) / (simNodes.length || 1);

    simulation = forceSimulation(simNodes)
      .force('gravity', forceGravity(0.6, avgSize * 2))
      .force('charge', forceManyBody().strength((d: any) => -(d._size || 5) * 5).distanceMax(avgSize * 8))
      .force('center', forceCenter(0, 0).strength(0.08))
      .force('collision', forceCollide().radius((d: any) => (d._size || 5) * 1.5 + 2).strength(0.9).iterations(2))
      .force('link', forceLink(simLinks).id((d: any) => d.id)
        .distance((l: any) => {
          const s1 = l.source?._size || 5;
          const s2 = l.target?._size || 5;
          return (s1 + s2) * 2;
        })
        .strength(0.4))
      .velocityDecay(0.4)
      .alphaDecay(0.025)
      .on('tick', () => {
        for (const node of simNodes) {
          const dist = Math.hypot(node.x || 0, node.y || 0);
          if (dist > idealRadius) {
            const dampen = idealRadius / dist;
            node.x *= 0.5 + 0.5 * dampen;
            node.y *= 0.5 + 0.5 * dampen;
            node.vx = (node.vx || 0) * 0.3;
            node.vy = (node.vy || 0) * 0.3;
          }
        }
        uploadNodes();
        uploadEdges();
        needsPickRedraw = true;
        // Continuously fit camera during simulation so view tracks clustering
        fitCamera();
      });

    simulation.alpha(1).restart();
  }

  function render() {
    if (!gl || !canvas) return;
    const [bgR, bgG, bgB] = hexToRgb(background);
    gl.viewport(0, 0, canvas.width, canvas.height);
    gl.clearColor(bgR / 255, bgG / 255, bgB / 255, 1);
    gl.clear(gl.COLOR_BUFFER_BIT);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);

    const matrix = buildViewMatrix();

    // Draw edges (GL_LINES)
    if (edgeProg && edgeVAO && edgeVertCount > 0) {
      gl.useProgram(edgeProg);
      gl.uniformMatrix3fv(gl.getUniformLocation(edgeProg, 'u_matrix'), false, matrix);
      gl.bindVertexArray(edgeVAO);
      gl.drawArrays(gl.LINES, 0, edgeVertCount);
    }

    // Draw nodes (instanced)
    if (nodeProg && nodeVAO && nodeCount > 0) {
      gl.useProgram(nodeProg);
      gl.uniformMatrix3fv(gl.getUniformLocation(nodeProg, 'u_matrix'), false, matrix);
      gl.uniform1i(gl.getUniformLocation(nodeProg, 'u_picking'), 0);
      gl.bindVertexArray(nodeVAO);
      gl.drawArraysInstanced(gl.TRIANGLES, 0, 6, nodeCount);
    }

    // Picking pass
    if (needsPickRedraw && pickFBO && nodeProg && nodeVAO && nodeCount > 0) {
      gl.bindFramebuffer(gl.FRAMEBUFFER, pickFBO);
      gl.clearColor(0, 0, 0, 0);
      gl.clear(gl.COLOR_BUFFER_BIT);
      gl.disable(gl.BLEND);
      gl.useProgram(nodeProg);
      gl.uniformMatrix3fv(gl.getUniformLocation(nodeProg, 'u_matrix'), false, matrix);
      gl.uniform1i(gl.getUniformLocation(nodeProg, 'u_picking'), 1);
      gl.bindVertexArray(nodeVAO);
      gl.drawArraysInstanced(gl.TRIANGLES, 0, 6, nodeCount);
      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      gl.enable(gl.BLEND);
      needsPickRedraw = false;
    }

    renderLabels();
    animFrame = requestAnimationFrame(render);
  }

  function worldToScreen(wx: number, wy: number): [number, number] {
    const sx = (wx - camX) * camZoom + width / 2;
    const sy = -(wy - camY) * camZoom + height / 2;
    return [sx, sy];
  }

  function renderLabels() {
    if (!labelCanvas || simNodes.length === 0) return;
    const ctx = labelCanvas.getContext('2d');
    if (!ctx) return;
    ctx.clearRect(0, 0, labelCanvas.width, labelCanvas.height);

    const dpr = devicePixelRatio;
    ctx.save();
    ctx.scale(dpr, dpr);

    // Determine which labels to show based on zoom and distance from center
    const centerX = width / 2;
    const centerY = height / 2;
    const maxLabelDist = Math.max(width, height) * 0.6; // labels fade beyond 60% from center
    const minScreenSize = 3; // don't label nodes smaller than 3px on screen

    ctx.font = '11px system-ui, -apple-system, sans-serif';
    ctx.textBaseline = 'middle';

    for (let i = 0; i < simNodes.length; i++) {
      const n = simNodes[i];
      if (!n._label) continue;
      const [sx, sy] = worldToScreen(n.x || 0, n.y || 0);
      const screenSize = (n._size || 5) * camZoom;

      // Skip if too small on screen
      if (screenSize < minScreenSize) continue;

      // Fade by distance from screen center
      const distFromCenter = Math.hypot(sx - centerX, sy - centerY);
      let alpha = 1.0 - (distFromCenter / maxLabelDist);
      alpha = Math.max(0, Math.min(1, alpha));

      // Boost alpha for hovered node
      if (i === hoveredIndex) alpha = 1.0;

      if (alpha < 0.05) continue;

      // Position label to the right of the node
      const labelX = sx + screenSize + 4;
      const labelY = sy;

      // Background pill
      const text = n._label;
      const metrics = ctx.measureText(text);
      const tw = metrics.width;
      const th = 14;
      const px = 4, py = 2;

      ctx.fillStyle = `rgba(26, 26, 46, ${0.75 * alpha})`;
      ctx.beginPath();
      const rx = labelX - px;
      const ry = labelY - th / 2 - py;
      const rw = tw + px * 2;
      const rh = th + py * 2;
      const cr = 3;
      ctx.moveTo(rx + cr, ry);
      ctx.lineTo(rx + rw - cr, ry);
      ctx.arcTo(rx + rw, ry, rx + rw, ry + cr, cr);
      ctx.lineTo(rx + rw, ry + rh - cr);
      ctx.arcTo(rx + rw, ry + rh, rx + rw - cr, ry + rh, cr);
      ctx.lineTo(rx + cr, ry + rh);
      ctx.arcTo(rx, ry + rh, rx, ry + rh - cr, cr);
      ctx.lineTo(rx, ry + cr);
      ctx.arcTo(rx, ry, rx + cr, ry, cr);
      ctx.closePath();
      ctx.fill();

      // Text
      ctx.fillStyle = `rgba(224, 224, 224, ${alpha})`;
      ctx.fillText(text, labelX, labelY);
    }

    ctx.restore();
  }

  function handleMouseMove(e: MouseEvent) {
    const rect = canvas?.getBoundingClientRect();
    if (rect) {
      mouseX = e.clientX - rect.left;
      mouseY = e.clientY - rect.top;
    }
    if (dragging) {
      camX = camStartX - (e.clientX - dragStartX) / camZoom;
      camY = camStartY - (e.clientY - dragStartY) / camZoom;
      needsPickRedraw = true;
      return;
    }
    if (!gl || !pickFBO || !canvas || !rect) return;
    const px = Math.round((e.clientX - rect.left) * (canvas.width / rect.width));
    const py = Math.round((rect.height - (e.clientY - rect.top)) * (canvas.height / rect.height));
    gl.bindFramebuffer(gl.FRAMEBUFFER, pickFBO);
    const pixel = new Uint8Array(4);
    gl.readPixels(px, py, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, pixel);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    const idx = colorToIndex(pixel[0], pixel[1], pixel[2]);
    if (idx !== hoveredIndex) {
      hoveredIndex = idx;
      onNodeHover(idx >= 0 && idx < nodes.length ? nodes[idx] : null);
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

  function handleMouseUp() {
    if (dragging) {
      const dx = Math.abs(dragStartX - (dragging ? dragStartX : 0));
      dragging = false;
      if (dx < 3 && hoveredIndex >= 0 && hoveredIndex < nodes.length) {
        onNodeClick(nodes[hoveredIndex]);
      }
    }
  }

  function handleWheel(e: WheelEvent) {
    e.preventDefault();
    const factor = e.deltaY > 0 ? 0.9 : 1.1;
    camZoom = Math.max(0.05, Math.min(50, camZoom * factor));
    needsPickRedraw = true;
  }

  onMount(() => {
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    width = rect.width;
    height = rect.height;
    canvas.width = width * devicePixelRatio;
    canvas.height = height * devicePixelRatio;

    // Set up label canvas overlay (same size as WebGL canvas)
    if (labelCanvas) {
      labelCanvas.width = canvas.width;
      labelCanvas.height = canvas.height;
    }

    gl = canvas.getContext('webgl2', { antialias: true, alpha: false });
    if (!gl) return;

    nodeProg = createProgram(gl, NODE_VERT, NODE_FRAG);
    edgeProg = createProgram(gl, EDGE_VERT, EDGE_FRAG);

    // Node VAO
    nodeVAO = gl.createVertexArray()!;
    cornerBuf = gl.createBuffer()!;
    nodeInstanceBuf = gl.createBuffer()!;
    gl.bindVertexArray(nodeVAO);

    gl.bindBuffer(gl.ARRAY_BUFFER, cornerBuf);
    gl.bufferData(gl.ARRAY_BUFFER, NODE_CORNERS, gl.STATIC_DRAW);
    const cLoc = gl.getAttribLocation(nodeProg, 'a_corner');
    gl.enableVertexAttribArray(cLoc);
    gl.vertexAttribPointer(cLoc, 2, gl.FLOAT, false, 0, 0);

    gl.bindBuffer(gl.ARRAY_BUFFER, nodeInstanceBuf);
    const stride = 20;
    const pLoc = gl.getAttribLocation(nodeProg, 'a_position');
    const sLoc = gl.getAttribLocation(nodeProg, 'a_size');
    const colLoc = gl.getAttribLocation(nodeProg, 'a_color');
    const pkLoc = gl.getAttribLocation(nodeProg, 'a_pickColor');

    gl.enableVertexAttribArray(pLoc);
    gl.vertexAttribPointer(pLoc, 2, gl.FLOAT, false, stride, 0);
    gl.vertexAttribDivisor(pLoc, 1);
    gl.enableVertexAttribArray(sLoc);
    gl.vertexAttribPointer(sLoc, 1, gl.FLOAT, false, stride, 8);
    gl.vertexAttribDivisor(sLoc, 1);
    gl.enableVertexAttribArray(colLoc);
    gl.vertexAttribPointer(colLoc, 4, gl.UNSIGNED_BYTE, true, stride, 12);
    gl.vertexAttribDivisor(colLoc, 1);
    gl.enableVertexAttribArray(pkLoc);
    gl.vertexAttribPointer(pkLoc, 4, gl.UNSIGNED_BYTE, true, stride, 16);
    gl.vertexAttribDivisor(pkLoc, 1);
    gl.bindVertexArray(null);

    // Edge VAO (simple GL_LINES)
    edgeVAO = gl.createVertexArray()!;
    edgeBuf = gl.createBuffer()!;
    gl.bindVertexArray(edgeVAO);
    gl.bindBuffer(gl.ARRAY_BUFFER, edgeBuf);
    const eStride = 12;
    const epLoc = gl.getAttribLocation(edgeProg, 'a_position');
    const ecLoc = gl.getAttribLocation(edgeProg, 'a_color');
    gl.enableVertexAttribArray(epLoc);
    gl.vertexAttribPointer(epLoc, 2, gl.FLOAT, false, eStride, 0);
    gl.enableVertexAttribArray(ecLoc);
    gl.vertexAttribPointer(ecLoc, 4, gl.UNSIGNED_BYTE, true, eStride, 8);
    gl.bindVertexArray(null);

    setupPickFBO();
    startSimulation();
    animFrame = requestAnimationFrame(render);

    const ro = new ResizeObserver(() => {
      if (!canvas) return;
      const r = canvas.getBoundingClientRect();
      width = r.width;
      height = r.height;
      canvas.width = width * devicePixelRatio;
      canvas.height = height * devicePixelRatio;
      if (labelCanvas) {
        labelCanvas.width = canvas.width;
        labelCanvas.height = canvas.height;
      }
      if (pickTex && gl) {
        gl.bindTexture(gl.TEXTURE_2D, pickTex);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, canvas.width, canvas.height, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
      }
      needsPickRedraw = true;
    });
    ro.observe(canvas);

    return () => {
      cancelAnimationFrame(animFrame);
      simulation?.stop();
      ro.disconnect();
      gl?.deleteProgram(nodeProg);
      gl?.deleteProgram(edgeProg);
      gl?.deleteBuffer(nodeInstanceBuf);
      gl?.deleteBuffer(edgeBuf);
      gl?.deleteBuffer(cornerBuf);
      gl?.deleteVertexArray(nodeVAO);
      gl?.deleteVertexArray(edgeVAO);
      gl?.deleteFramebuffer(pickFBO);
      gl?.deleteTexture(pickTex);
    };
  });

  $effect(() => {
    if (nodes && gl && nodeProg) {
      simulation?.stop();
      startSimulation();
    }
  });
</script>

<div style="position:relative;width:100%;height:100%">
  <canvas
    bind:this={canvas}
    onmousemove={handleMouseMove}
    onmousedown={handleMouseDown}
    onmouseup={handleMouseUp}
    onwheel={handleWheel}
    style="position:absolute;top:0;left:0;width:100%;height:100%;background:{background};cursor:{dragging ? 'grabbing' : hoveredIndex >= 0 ? 'pointer' : 'grab'}"
  ></canvas>
  <canvas
    bind:this={labelCanvas}
    style="position:absolute;top:0;left:0;width:100%;height:100%;pointer-events:none"
  ></canvas>
  {#if hoveredIndex >= 0 && hoveredIndex < nodes.length}
    <div
      bind:this={tooltipEl}
      style="
        position:absolute;
        left:{mouseX + 16}px;
        top:{mouseY - 10}px;
        background:rgba(26,26,46,0.94);
        color:#E0E0E0;
        padding:6px 10px;
        border-radius:6px;
        font-size:12px;
        pointer-events:none;
        max-width:280px;
        line-height:1.4;
        border:1px solid rgba(255,255,255,0.12);
        z-index:100;
      "
    >
      <div style="font-weight:600">{nodes[hoveredIndex].label}</div>
      <div style="font-size:10px;opacity:0.6;margin-top:2px">
        {nodes[hoveredIndex].kind || ''} · size {Math.round(nodes[hoveredIndex].size)}
      </div>
    </div>
  {/if}
</div>
