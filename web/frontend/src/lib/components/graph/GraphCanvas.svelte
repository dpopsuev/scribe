<script lang="ts">
  import { onMount } from 'svelte';
  import { createProgram, hexToRgb, indexToColor, colorToIndex } from './webgl';
  import { NODE_VERT, NODE_FRAG, EDGE_VERT, EDGE_FRAG, NODE_CORNERS } from './shaders';

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
  let hoveredIndex: number = $state(-1);
  let width = $state(800);
  let height = $state(600);

  // Camera state
  let camX = $state(0);
  let camY = $state(0);
  let camZoom = $state(1);
  let dragging = $state(false);
  let dragStartX = 0;
  let dragStartY = 0;
  let camStartX = 0;
  let camStartY = 0;

  // WebGL resources (not reactive — managed imperatively)
  let gl: WebGL2RenderingContext | null = null;
  let nodeProg: WebGLProgram | null = null;
  let edgeProg: WebGLProgram | null = null;
  let nodeVAO: WebGLVertexArrayObject | null = null;
  let edgeVAO: WebGLVertexArrayObject | null = null;
  let nodeInstanceBuf: WebGLBuffer | null = null;
  let edgeInstanceBuf: WebGLBuffer | null = null;
  let cornerBuf: WebGLBuffer | null = null;
  let pickFBO: WebGLFramebuffer | null = null;
  let pickTex: WebGLTexture | null = null;
  let pickRBO: WebGLRenderbuffer | null = null;
  let animFrame = 0;
  let nodeCount = 0;
  let edgeVertCount = 0;
  let needsPickRedraw = true;

  function buildViewMatrix(): Float32Array {
    const sx = 2 * camZoom / width;
    const sy = 2 * camZoom / height;
    const tx = -camX * sx;
    const ty = -camY * sy;
    return new Float32Array([sx, 0, 0, 0, sy, 0, tx, ty, 1]);
  }

  function setupPickFBO() {
    if (!gl || !canvas) return;
    pickFBO = gl.createFramebuffer();
    pickTex = gl.createTexture();
    pickRBO = gl.createRenderbuffer();
    gl.bindFramebuffer(gl.FRAMEBUFFER, pickFBO);
    gl.bindTexture(gl.TEXTURE_2D, pickTex);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, canvas.width, canvas.height, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, pickTex, 0);
    gl.bindRenderbuffer(gl.RENDERBUFFER, pickRBO);
    gl.renderbufferStorage(gl.RENDERBUFFER, gl.DEPTH_COMPONENT16, canvas.width, canvas.height);
    gl.framebufferRenderbuffer(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.RENDERBUFFER, pickRBO);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
  }

  function uploadNodes() {
    if (!gl || !nodeInstanceBuf) return;
    // Per-instance data: x(f), y(f), size(f), r(ub), g(ub), b(ub), a(ub), pr(ub), pg(ub), pb(ub), pa(ub)
    // = 3 floats + 8 bytes = 20 bytes per instance
    const stride = 20;
    const buf = new ArrayBuffer(nodes.length * stride);
    const floats = new Float32Array(buf);
    const bytes = new Uint8Array(buf);

    for (let i = 0; i < nodes.length; i++) {
      const n = nodes[i];
      const off = i * stride;
      const fOff = off / 4;
      floats[fOff] = n.x;
      floats[fOff + 1] = n.y;
      floats[fOff + 2] = n.size;
      const [r, g, b] = hexToRgb(n.color);
      bytes[off + 12] = r;
      bytes[off + 13] = g;
      bytes[off + 14] = b;
      bytes[off + 15] = 230; // opacity ~0.9
      const [pr, pg, pb, pa] = indexToColor(i);
      bytes[off + 16] = pr;
      bytes[off + 17] = pg;
      bytes[off + 18] = pb;
      bytes[off + 19] = pa;
    }

    gl.bindBuffer(gl.ARRAY_BUFFER, nodeInstanceBuf);
    gl.bufferData(gl.ARRAY_BUFFER, buf, gl.DYNAMIC_DRAW);
    nodeCount = nodes.length;
    needsPickRedraw = true;
  }

  function uploadEdges() {
    if (!gl || !edgeInstanceBuf) return;
    const nodeMap = new Map(nodes.map((n, i) => [n.id, i]));
    // Per edge vertex: sx(f), sy(f), tx(f), ty(f), r(ub), g(ub), b(ub), a(ub), offset(f)
    // = 5 floats + 4 bytes = 24 bytes per vertex, 6 vertices per edge (quad)
    const stride = 24;
    const validEdges: { s: GraphNode; t: GraphNode; color: string }[] = [];
    for (const e of edges) {
      const si = nodeMap.get(e.source);
      const ti = nodeMap.get(e.target);
      if (si !== undefined && ti !== undefined) {
        validEdges.push({ s: nodes[si], t: nodes[ti], color: e.color });
      }
    }

    const vertsPerEdge = 6;
    const buf = new ArrayBuffer(validEdges.length * vertsPerEdge * stride);
    const floats = new Float32Array(buf);
    const bytes = new Uint8Array(buf);
    const offsets = [-1, 1, -1, 1, -1, 1]; // quad strip

    for (let i = 0; i < validEdges.length; i++) {
      const { s, t, color } = validEdges[i];
      const [r, g, b] = hexToRgb(color.replace(/[0-9a-f]{2}$/i, '') || color);
      for (let v = 0; v < vertsPerEdge; v++) {
        const off = (i * vertsPerEdge + v) * stride;
        const fOff = off / 4;
        floats[fOff] = s.x;
        floats[fOff + 1] = s.y;
        floats[fOff + 2] = t.x;
        floats[fOff + 3] = t.y;
        bytes[off + 16] = r;
        bytes[off + 17] = g;
        bytes[off + 18] = b;
        bytes[off + 19] = 70; // ~0.27 opacity
        floats[fOff + 5] = offsets[v];
      }
    }

    gl.bindBuffer(gl.ARRAY_BUFFER, edgeInstanceBuf);
    gl.bufferData(gl.ARRAY_BUFFER, buf, gl.DYNAMIC_DRAW);
    edgeVertCount = validEdges.length * vertsPerEdge;
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

    // Draw edges
    if (edgeProg && edgeVAO && edgeVertCount > 0) {
      gl.useProgram(edgeProg);
      gl.uniformMatrix3fv(gl.getUniformLocation(edgeProg, 'u_matrix'), false, matrix);
      gl.uniform1f(gl.getUniformLocation(edgeProg, 'u_width'), 0.8 / camZoom);
      gl.bindVertexArray(edgeVAO);
      gl.drawArrays(gl.TRIANGLES, 0, edgeVertCount);
    }

    // Draw nodes (normal pass)
    if (nodeProg && nodeVAO && nodeCount > 0) {
      gl.useProgram(nodeProg);
      gl.uniformMatrix3fv(gl.getUniformLocation(nodeProg, 'u_matrix'), false, matrix);
      gl.uniform1f(gl.getUniformLocation(nodeProg, 'u_pixelRatio'), 1.0 / camZoom);
      gl.uniform1i(gl.getUniformLocation(nodeProg, 'u_picking'), 0);
      gl.bindVertexArray(nodeVAO);
      gl.drawArraysInstanced(gl.TRIANGLES, 0, 6, nodeCount);
    }

    // Picking pass (only when needed)
    if (needsPickRedraw && pickFBO && nodeProg && nodeVAO && nodeCount > 0) {
      gl.bindFramebuffer(gl.FRAMEBUFFER, pickFBO);
      gl.clearColor(0, 0, 0, 0);
      gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
      gl.disable(gl.BLEND);
      gl.useProgram(nodeProg);
      gl.uniformMatrix3fv(gl.getUniformLocation(nodeProg, 'u_matrix'), false, matrix);
      gl.uniform1f(gl.getUniformLocation(nodeProg, 'u_pixelRatio'), 1.0 / camZoom);
      gl.uniform1i(gl.getUniformLocation(nodeProg, 'u_picking'), 1);
      gl.bindVertexArray(nodeVAO);
      gl.drawArraysInstanced(gl.TRIANGLES, 0, 6, nodeCount);
      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      gl.enable(gl.BLEND);
      needsPickRedraw = false;
    }

    animFrame = requestAnimationFrame(render);
  }

  function screenToWorld(sx: number, sy: number): [number, number] {
    return [
      sx / camZoom + camX - width / (2 * camZoom),
      -(sy / camZoom + camY - height / (2 * camZoom)),
    ];
  }

  function handleMouseMove(e: MouseEvent) {
    if (dragging) {
      camX = camStartX - (e.clientX - dragStartX) / camZoom;
      camY = camStartY + (e.clientY - dragStartY) / camZoom;
      needsPickRedraw = true;
      return;
    }
    if (!gl || !pickFBO || !canvas) return;
    const rect = canvas.getBoundingClientRect();
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
    dragging = false;
  }

  function handleClick() {
    if (hoveredIndex >= 0 && hoveredIndex < nodes.length) {
      onNodeClick(nodes[hoveredIndex]);
    }
  }

  function handleWheel(e: WheelEvent) {
    e.preventDefault();
    const factor = e.deltaY > 0 ? 0.9 : 1.1;
    camZoom = Math.max(0.01, Math.min(100, camZoom * factor));
    needsPickRedraw = true;
  }

  function handleResize() {
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    width = rect.width;
    height = rect.height;
    canvas.width = width * devicePixelRatio;
    canvas.height = height * devicePixelRatio;
    if (pickTex && gl) {
      gl.bindTexture(gl.TEXTURE_2D, pickTex);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, canvas.width, canvas.height, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
      if (pickRBO) {
        gl.bindRenderbuffer(gl.RENDERBUFFER, pickRBO);
        gl.renderbufferStorage(gl.RENDERBUFFER, gl.DEPTH_COMPONENT16, canvas.width, canvas.height);
      }
    }
    needsPickRedraw = true;
  }

  onMount(() => {
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    width = rect.width;
    height = rect.height;
    canvas.width = width * devicePixelRatio;
    canvas.height = height * devicePixelRatio;

    gl = canvas.getContext('webgl2', { antialias: true, alpha: false });
    if (!gl) { console.error('WebGL2 not supported'); return; }

    // Compile programs
    nodeProg = createProgram(gl, NODE_VERT, NODE_FRAG);
    edgeProg = createProgram(gl, EDGE_VERT, EDGE_FRAG);

    // Node VAO: corner buffer (per-vertex) + instance buffer (per-instance)
    nodeVAO = gl.createVertexArray()!;
    cornerBuf = gl.createBuffer()!;
    nodeInstanceBuf = gl.createBuffer()!;
    gl.bindVertexArray(nodeVAO);

    // Corner attribute (per-vertex, divisor=0)
    gl.bindBuffer(gl.ARRAY_BUFFER, cornerBuf);
    gl.bufferData(gl.ARRAY_BUFFER, NODE_CORNERS, gl.STATIC_DRAW);
    const cornerLoc = gl.getAttribLocation(nodeProg, 'a_corner');
    gl.enableVertexAttribArray(cornerLoc);
    gl.vertexAttribPointer(cornerLoc, 2, gl.FLOAT, false, 0, 0);

    // Instance attributes (divisor=1)
    gl.bindBuffer(gl.ARRAY_BUFFER, nodeInstanceBuf);
    const stride = 20;
    const posLoc = gl.getAttribLocation(nodeProg, 'a_position');
    const sizeLoc = gl.getAttribLocation(nodeProg, 'a_size');
    const colorLoc = gl.getAttribLocation(nodeProg, 'a_color');
    const pickLoc = gl.getAttribLocation(nodeProg, 'a_pickColor');

    gl.enableVertexAttribArray(posLoc);
    gl.vertexAttribPointer(posLoc, 2, gl.FLOAT, false, stride, 0);
    gl.vertexAttribDivisor(posLoc, 1);

    gl.enableVertexAttribArray(sizeLoc);
    gl.vertexAttribPointer(sizeLoc, 1, gl.FLOAT, false, stride, 8);
    gl.vertexAttribDivisor(sizeLoc, 1);

    gl.enableVertexAttribArray(colorLoc);
    gl.vertexAttribPointer(colorLoc, 4, gl.UNSIGNED_BYTE, true, stride, 12);
    gl.vertexAttribDivisor(colorLoc, 1);

    gl.enableVertexAttribArray(pickLoc);
    gl.vertexAttribPointer(pickLoc, 4, gl.UNSIGNED_BYTE, true, stride, 16);
    gl.vertexAttribDivisor(pickLoc, 1);

    gl.bindVertexArray(null);

    // Edge VAO
    edgeVAO = gl.createVertexArray()!;
    edgeInstanceBuf = gl.createBuffer()!;
    gl.bindVertexArray(edgeVAO);
    gl.bindBuffer(gl.ARRAY_BUFFER, edgeInstanceBuf);

    const eStride = 24;
    const eSrcLoc = gl.getAttribLocation(edgeProg, 'a_source');
    const eTgtLoc = gl.getAttribLocation(edgeProg, 'a_target');
    const eColLoc = gl.getAttribLocation(edgeProg, 'a_color');
    const eOffLoc = gl.getAttribLocation(edgeProg, 'a_offset');

    gl.enableVertexAttribArray(eSrcLoc);
    gl.vertexAttribPointer(eSrcLoc, 2, gl.FLOAT, false, eStride, 0);
    gl.enableVertexAttribArray(eTgtLoc);
    gl.vertexAttribPointer(eTgtLoc, 2, gl.FLOAT, false, eStride, 8);
    gl.enableVertexAttribArray(eColLoc);
    gl.vertexAttribPointer(eColLoc, 4, gl.UNSIGNED_BYTE, true, eStride, 16);
    gl.enableVertexAttribArray(eOffLoc);
    gl.vertexAttribPointer(eOffLoc, 1, gl.FLOAT, false, eStride, 20);

    gl.bindVertexArray(null);

    // Picking framebuffer
    setupPickFBO();

    // Initial upload
    uploadNodes();
    uploadEdges();

    // Auto-fit camera
    if (nodes.length > 0) {
      let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
      for (const n of nodes) {
        if (n.x < minX) minX = n.x;
        if (n.x > maxX) maxX = n.x;
        if (n.y < minY) minY = n.y;
        if (n.y > maxY) maxY = n.y;
      }
      camX = (minX + maxX) / 2;
      camY = (minY + maxY) / 2;
      const spanX = (maxX - minX) || 100;
      const spanY = (maxY - minY) || 100;
      camZoom = Math.min(width / (spanX * 1.2), height / (spanY * 1.2));
    }

    // Resize observer
    const ro = new ResizeObserver(handleResize);
    ro.observe(canvas);

    // Start render loop
    animFrame = requestAnimationFrame(render);

    return () => {
      cancelAnimationFrame(animFrame);
      ro.disconnect();
      gl?.deleteProgram(nodeProg);
      gl?.deleteProgram(edgeProg);
      gl?.deleteBuffer(nodeInstanceBuf);
      gl?.deleteBuffer(edgeInstanceBuf);
      gl?.deleteBuffer(cornerBuf);
      gl?.deleteVertexArray(nodeVAO);
      gl?.deleteVertexArray(edgeVAO);
      gl?.deleteFramebuffer(pickFBO);
      gl?.deleteTexture(pickTex);
      gl?.deleteRenderbuffer(pickRBO);
    };
  });

  // Re-upload when data changes
  $effect(() => {
    if (nodes && gl) { uploadNodes(); uploadEdges(); }
  });
</script>

<canvas
  bind:this={canvas}
  onmousemove={handleMouseMove}
  onmousedown={handleMouseDown}
  onmouseup={handleMouseUp}
  onclick={handleClick}
  onwheel={handleWheel}
  style="width:100%;height:100%;display:block;background:{background};cursor:{dragging ? 'grabbing' : hoveredIndex >= 0 ? 'pointer' : 'grab'}"
></canvas>
