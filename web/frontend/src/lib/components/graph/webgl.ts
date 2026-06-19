// webgl.ts — Low-level WebGL2 utilities for the graph renderer.
// Compiles shaders, creates programs, manages buffers.

export function createProgram(
  gl: WebGL2RenderingContext,
  vertSrc: string,
  fragSrc: string
): WebGLProgram {
  const vert = compileShader(gl, gl.VERTEX_SHADER, vertSrc);
  const frag = compileShader(gl, gl.FRAGMENT_SHADER, fragSrc);
  const prog = gl.createProgram()!;
  gl.attachShader(prog, vert);
  gl.attachShader(prog, frag);
  gl.linkProgram(prog);
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
    const log = gl.getProgramInfoLog(prog);
    gl.deleteProgram(prog);
    throw new Error(`Program link failed: ${log}`);
  }
  return prog;
}

function compileShader(
  gl: WebGL2RenderingContext,
  type: number,
  src: string
): WebGLShader {
  const shader = gl.createShader(type)!;
  gl.shaderSource(shader, src);
  gl.compileShader(shader);
  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
    const log = gl.getShaderInfoLog(shader);
    gl.deleteShader(shader);
    throw new Error(`Shader compile failed: ${log}`);
  }
  return shader;
}

export function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace('#', '');
  const n = parseInt(h.length === 3 ? h.split('').map(c => c + c).join('') : h.substring(0, 6), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

export function hexAlpha(hex: string): number {
  const h = hex.replace('#', '');
  if (h.length === 8) return parseInt(h.substring(6, 8), 16);
  return 230;
}

export function indexToColor(i: number): [number, number, number, number] {
  const id = i + 1;
  return [(id >> 16) & 255, (id >> 8) & 255, id & 255, 255];
}

export function colorToIndex(r: number, g: number, b: number): number {
  return ((r << 16) | (g << 8) | b) - 1;
}
