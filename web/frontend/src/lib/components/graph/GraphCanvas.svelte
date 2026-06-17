<script lang="ts">
  import { onMount } from 'svelte';
  import { createProgram, hexToRgb, indexToColor, colorToIndex } from './webgl';
  import { NODE_VERT, NODE_FRAG, EDGE_VERT, EDGE_FRAG, NODE_CORNERS } from './shaders';
  import { buildViewMatrix, worldToScreen } from './transform';
  import type { Camera } from './transform';
  import { createFocusLock, userTakeLock, checkIdle, systemCanMove, isTrackingNode, fitBounds, startTransition, tickTransition } from './camera';
  import type { FocusLock, CameraTransition } from './camera';
  import { forceGravity } from './gravity';
  import { forceSimulation, forceLink, forceManyBody, forceCenter, forceCollide } from 'd3-force';

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

  // Focus lock: pure state machine (camera.ts) + timer for idle detection
  let lock: FocusLock = $state(createFocusLock());
  let idleInterval: ReturnType<typeof setInterval> | null = null;
  let transition: CameraTransition | null = null;
  let simActive = false;

  function onUserInteract(focusNode?: string) {
    const wasSystem = lock.owner === 'system';
    lock = userTakeLock(lock, focusNode);
    transition = null;
    lastInteractTime = performance.now();
    if (wasSystem) { _frameHist.length = 0; prevFrameTs = 0; }
  }

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

  // Cached uniform locations (avoids 5 getUniformLocation calls per frame)
  let uEdgeMatrix: WebGLUniformLocation | null = null;
  let uNodeMatrix: WebGLUniformLocation | null = null;
  let uNodePicking: WebGLUniformLocation | null = null;

  // Cached label rendering context + background color
  let labelCtx: CanvasRenderingContext2D | null = null;
  let bgRgb: [number, number, number] = [26, 26, 46];
  let lastInteractTime = 0; // performance.now() of last user interaction
  const LABEL_DEBOUNCE_MS = 100;

  // Performance HUD — per-frame FPS via rAF timestamp delta
  let perf = $state({ fps: 0, total: 0, webgl: 0, pick: 0, labels: 0 });
  let _pf = { n: 0, total: 0, webgl: 0, pick: 0, labels: 0, ts: 0 };
  let prevFrameTs = 0;
  let instantFps = $state(60);

  // Frame history ring buffer — Playwright reads this for per-frame analysis
  const FRAME_HIST_SIZE = 240;
  const _frameHist: number[] = [];

  function fpsColor(fps: number): string {
    if (fps >= 60) return '#3b82f6';  // blue
    if (fps >= 30) return '#22c55e';  // green
    if (fps >= 15) return '#eab308';  // yellow
    return '#ef4444';                 // red
  }
  let edgeVertCount = 0;
  let needsPickRedraw = true;
  let simulation: any = null;
  let simNodes: any[] = [];
  let simLinks: any[] = [];
  let prewarming = false;

  function getCamera(): Camera {
    return { x: camX, y: camY, zoom: camZoom, width, height };
  }

  function fitCamera() {
    if (!systemCanMove(lock)) return;
    const cam = fitBounds(simNodes, width, height);
    if (!cam) return;
    camX = cam.x;
    camY = cam.y;
    camZoom = cam.zoom;
    needsPickRedraw = true;
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
      .force('gravity', forceGravity(0.2, avgSize * 3))
      .force('charge', forceManyBody().strength((d: any) => -(d._size || 5) * 2.5).distanceMax(avgSize * 12))
      .force('center', forceCenter(0, 0).strength(0.03))
      .force('collision', forceCollide().radius((d: any) => (d._size || 5) * 1.4 + 2).strength(0.5).iterations(3))
      .force('link', forceLink(simLinks).id((d: any) => d.id)
        .distance((l: any) => {
          const s1 = l.source?._size || 5;
          const s2 = l.target?._size || 5;
          return (s1 + s2) * 2.5;
        })
        .strength(0.3))
      .velocityDecay(0.55)
      .alphaDecay(0.012)
      .on('tick', () => {
        // Soft spring boundary — gentle restoring force instead of hard clamp
        for (const node of simNodes) {
          const dist = Math.hypot(node.x || 0, node.y || 0);
          if (dist > idealRadius) {
            const pull = ((dist - idealRadius) / idealRadius) * 0.08;
            node.vx = (node.vx || 0) - (node.x / dist) * pull;
            node.vy = (node.vy || 0) - (node.y / dist) * pull;
          }
        }
        if (prewarming) return;
        uploadNodes();
        uploadEdges();
        needsPickRedraw = true;

        if (isTrackingNode(lock)) {
          const fn = simNodes.find((n: any) => n.id === lock.focusNodeId);
          if (fn) { camX = fn.x || 0; camY = fn.y || 0; }
        } else {
          fitCamera();
        }
      });

    // Pre-warm: run physics to full completion before first render.
    // ~473 ticks of CPU math (~5ms for 43 nodes), zero GPU work.
    // No breathing settle — nodes appear in final positions immediately.
    prewarming = true;
    simulation.alpha(0.3).stop();
    while (simulation.alpha() >= simulation.alphaMin()) simulation.tick();
    prewarming = false;

    uploadNodes();
    uploadEdges();
    fitCamera();

    // Simulation complete — no restart. Only re-runs on data change ($effect).
    simActive = false;
    simulation.on('end', () => { simActive = false; });
  }

  function render(timestamp: number = 0) {
    if (!gl || !canvas) return;
    const t0 = performance.now();

    // Per-frame FPS from rAF timestamp delta (captures ALL main-thread work)
    if (prevFrameTs > 0 && timestamp > 0) {
      const delta = timestamp - prevFrameTs;
      if (delta > 0) {
        instantFps = Math.round(1000 / delta);
        _frameHist.push(instantFps);
        if (_frameHist.length > FRAME_HIST_SIZE) _frameHist.shift();
      }
    }
    prevFrameTs = timestamp;

    // Tick smooth camera transition (smootherstep ease-in-out)
    if (transition) {
      const { cam, done } = tickTransition(transition);
      camX = cam.x;
      camY = cam.y;
      camZoom = cam.zoom;
      needsPickRedraw = true;
      if (done) transition = null;
    }

    gl.viewport(0, 0, canvas.width, canvas.height);
    gl.clearColor(bgRgb[0] / 255, bgRgb[1] / 255, bgRgb[2] / 255, 1);
    gl.clear(gl.COLOR_BUFFER_BIT);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);

    const cam = getCamera();
    const matrix = buildViewMatrix(cam);

    // Draw edges (GL_LINES) — cached uniform location
    if (edgeProg && edgeVAO && edgeVertCount > 0) {
      gl.useProgram(edgeProg);
      gl.uniformMatrix3fv(uEdgeMatrix, false, matrix);
      gl.bindVertexArray(edgeVAO);
      gl.drawArrays(gl.LINES, 0, edgeVertCount);
    }

    // Draw nodes (instanced) — cached uniform locations
    if (nodeProg && nodeVAO && nodeCount > 0) {
      gl.useProgram(nodeProg);
      gl.uniformMatrix3fv(uNodeMatrix, false, matrix);
      gl.uniform1i(uNodePicking, 0);
      gl.bindVertexArray(nodeVAO);
      gl.drawArraysInstanced(gl.TRIANGLES, 0, 6, nodeCount);
    }
    const t1 = performance.now();

    // Picking pass — deferred: only redraw when mouse moves, not every zoom step
    if (needsPickRedraw && pickFBO && nodeProg && nodeVAO && nodeCount > 0) {
      gl.bindFramebuffer(gl.FRAMEBUFFER, pickFBO);
      gl.clearColor(0, 0, 0, 0);
      gl.clear(gl.COLOR_BUFFER_BIT);
      gl.disable(gl.BLEND);
      gl.useProgram(nodeProg);
      gl.uniformMatrix3fv(uNodeMatrix, false, matrix);
      gl.uniform1i(uNodePicking, 1);
      gl.bindVertexArray(nodeVAO);
      gl.drawArraysInstanced(gl.TRIANGLES, 0, 6, nodeCount);
      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      gl.enable(gl.BLEND);
      needsPickRedraw = false;
    }
    const t2 = performance.now();

    // Labels are 85% of frame budget — skip during active interaction,
    // redraw 100ms after last zoom/drag stops (Sigma.js / Google Maps pattern)
    if (t0 - lastInteractTime > LABEL_DEBOUNCE_MS || lock.owner === 'system') {
      renderLabels(cam);
    } else if (labelCtx) {
      labelCtx.clearRect(0, 0, labelCanvas!.width, labelCanvas!.height);
    }
    const t3 = performance.now();

    // Performance HUD — accumulate, update display at 2Hz
    _pf.webgl += t1 - t0;
    _pf.pick += t2 - t1;
    _pf.labels += t3 - t2;
    _pf.total += t3 - t0;
    _pf.n++;
    if (t3 - _pf.ts > 500) {
      const n = _pf.n;
      const fps = Math.round(n / ((t3 - _pf.ts) / 1000));
      const total = _pf.total / n;
      const webgl = _pf.webgl / n;
      const pick = _pf.pick / n;
      const labels = _pf.labels / n;
      perf = { fps, total, webgl, pick, labels };
      _pf.n = _pf.total = _pf.webgl = _pf.pick = _pf.labels = 0;
      _pf.ts = t3;
      // Plain objects for programmatic testing (Svelte proxy won't serialize over CDP)
      (window as any).__GRAPH_PERF__ = { fps, total, webgl, pick, labels };
      (window as any).__GRAPH_FRAME_HIST__ = [..._frameHist];
      // Push to backend ring buffer — curl http://localhost:8083/api/v1/debug/perf
      const hist = [..._frameHist];
      const bins = { blue: 0, green: 0, yellow: 0, red: 0 };
      for (const f of hist) { if (f >= 60) bins.blue++; else if (f >= 30) bins.green++; else if (f >= 15) bins.yellow++; else bins.red++; }
      const diagMin = hist.length ? Math.min(...hist) : 0;
      const diagMax = hist.length ? Math.max(...hist) : 0;
      const diagAvg = hist.length ? Math.round(hist.reduce((s, v) => s + v, 0) / hist.length) : 0;
      fetch('/api/v1/debug/perf', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ fps, total, webgl, pick, labels, bins, avg: diagAvg, min: diagMin, max: diagMax, frames: hist.length, hist }),
      }).catch(() => {});
      // Reset function: clears history AND prevFrameTs to avoid stale first-frame delta
      (window as any).__GRAPH_RESET_PERF__ = () => { _frameHist.length = 0; prevFrameTs = 0; };
      // Diagnostic dump: call from console or press 'p' to print report
      (window as any).__GRAPH_DIAG__ = () => {
        const h = [..._frameHist];
        const bins = { blue: 0, green: 0, yellow: 0, red: 0 };
        for (const f of h) { if (f >= 60) bins.blue++; else if (f >= 30) bins.green++; else if (f >= 15) bins.yellow++; else bins.red++; }
        const min = h.length ? Math.min(...h) : 0;
        const max = h.length ? Math.max(...h) : 0;
        const avg = h.length ? Math.round(h.reduce((s, v) => s + v, 0) / h.length) : 0;
        console.log(`%c=== SCRIBE GRAPH PERF REPORT ===`, 'font-weight:bold;font-size:14px');
        console.log(`Frames: ${h.length} | Avg: ${avg} fps | Min: ${min} | Max: ${max}`);
        console.log(`🔵 Blue  60+: ${bins.blue} (${h.length ? ((bins.blue/h.length)*100).toFixed(0) : 0}%)`);
        console.log(`🟢 Green 30-59: ${bins.green} (${h.length ? ((bins.green/h.length)*100).toFixed(0) : 0}%)`);
        console.log(`🟡 Yellow 15-29: ${bins.yellow} (${h.length ? ((bins.yellow/h.length)*100).toFixed(0) : 0}%)`);
        console.log(`🔴 Red   <15: ${bins.red} (${h.length ? ((bins.red/h.length)*100).toFixed(0) : 0}%)`);
        console.log(`Current: gl:${perf.webgl.toFixed(2)}ms pk:${perf.pick.toFixed(2)}ms lbl:${perf.labels.toFixed(2)}ms`);
        return { bins, avg, min, max, frames: h.length, perf: { ...perf } };
      };
    }

    animFrame = requestAnimationFrame(render);
  }

  // Text width cache — measureText is expensive, labels don't change
  const _textWidths = new Map<string, number>();

  function renderLabels(cam: Camera) {
    if (!labelCtx || simNodes.length === 0) return;
    const ctx = labelCtx;
    ctx.clearRect(0, 0, labelCanvas!.width, labelCanvas!.height);

    const dpr = devicePixelRatio;
    ctx.save();
    ctx.scale(dpr, dpr);

    const centerX = cam.width / 2;
    const centerY = cam.height / 2;
    const maxLabelDist = Math.max(cam.width, cam.height) * 0.6;
    const minScreenSize = 3;

    ctx.font = '11px system-ui, -apple-system, sans-serif';
    ctx.textBaseline = 'middle';

    for (let i = 0; i < simNodes.length; i++) {
      const n = simNodes[i];
      if (!n._label) continue;
      const [sx, sy] = worldToScreen(cam, n.x || 0, n.y || 0);
      const screenSize = (n._size || 5) * cam.zoom;

      if (screenSize < minScreenSize) continue;

      const distFromCenter = Math.hypot(sx - centerX, sy - centerY);
      let alpha = 1.0 - (distFromCenter / maxLabelDist);
      alpha = Math.max(0, Math.min(1, alpha));
      if (i === hoveredIndex) alpha = 1.0;
      if (alpha < 0.05) continue;

      const labelX = sx + screenSize + 4;
      const labelY = sy;

      // Cached text width — avoids measureText per frame
      const text = n._label;
      let tw = _textWidths.get(text);
      if (tw === undefined) {
        tw = ctx.measureText(text).width;
        _textWidths.set(text, tw);
      }
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
    needsPickRedraw = true;
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
      onUserInteract();
    }
  }

  function handleMouseUp(e: MouseEvent) {
    if (dragging) {
      dragging = false;
      const moved = Math.hypot(e.clientX - dragStartX, e.clientY - dragStartY);
      if (moved < 5 && hoveredIndex >= 0 && hoveredIndex < nodes.length) {
        if (e.shiftKey) {
          const n = simNodes[hoveredIndex];
          if (n) {
            onUserInteract(n.id);
            camX = n.x || 0;
            camY = n.y || 0;
          }
        } else {
          onNodeClick(nodes[hoveredIndex]);
        }
      }
    }
  }

  function handleWheel(e: WheelEvent) {
    e.preventDefault();
    onUserInteract();
    const factor = e.deltaY > 0 ? 0.9 : 1.1;
    camZoom = Math.max(0.05, Math.min(50, camZoom * factor));
    // Don't set needsPickRedraw here — pick FBO only needed when mouse moves
    // (readPixels in handleMouseMove). This avoids a full extra draw on every scroll tick.
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

    // Cache uniform locations (eliminates 5 GL calls per frame)
    uEdgeMatrix = gl.getUniformLocation(edgeProg, 'u_matrix');
    uNodeMatrix = gl.getUniformLocation(nodeProg, 'u_matrix');
    uNodePicking = gl.getUniformLocation(nodeProg, 'u_picking');

    // Cache label canvas context and background color
    if (labelCanvas) labelCtx = labelCanvas.getContext('2d');
    bgRgb = hexToRgb(background) as [number, number, number];
    _pf.ts = performance.now();

    setupPickFBO();
    startSimulation();
    animFrame = requestAnimationFrame(render);

    // Poll idle state every second — smooth transition back when lock returns to system
    idleInterval = setInterval(() => {
      const wasUser = lock.owner === 'user';
      lock = checkIdle(lock);
      if (wasUser && lock.owner === 'system' && !simActive) {
        const target = fitBounds(simNodes, width, height);
        if (target) {
          transition = startTransition({ x: camX, y: camY, zoom: camZoom }, target);
        }
      }
    }, 1000);

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
      if (idleInterval) clearInterval(idleInterval);
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
  <!-- FPS ruler: color encodes per-frame rate (blue=60+ green=30-60 yellow=15-30 red=<15) -->
  <div style="
    position:absolute;bottom:0;left:0;right:0;height:18px;
    display:flex;align-items:center;gap:0;
    background:rgba(0,0,0,0.5);pointer-events:none;z-index:50;
  ">
    <div style="
      width:6px;height:12px;margin-left:4px;border-radius:2px;
      background:{fpsColor(instantFps)};
      box-shadow:0 0 4px {fpsColor(instantFps)};
    "></div>
    <span style="font:10px monospace;color:{fpsColor(instantFps)};margin-left:4px">{instantFps}</span>
    <span style="font:9px monospace;color:#6b7280;margin-left:6px">{perf.total.toFixed(1)}ms gl:{perf.webgl.toFixed(1)} pk:{perf.pick.toFixed(1)} lbl:{perf.labels.toFixed(1)}</span>
    <!-- Mini sparkline: last 60 frames as 1px bars -->
    <div style="display:flex;align-items:end;height:12px;margin-left:auto;margin-right:4px;gap:0">
      {#each _frameHist.slice(-60) as f}
        <div style="width:1px;background:{fpsColor(f)};height:{Math.min(12, Math.max(1, f / 5))}px"></div>
      {/each}
    </div>
  </div>
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
