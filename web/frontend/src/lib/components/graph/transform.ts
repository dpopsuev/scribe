// transform.ts — Coordinate transform shared between WebGL and Canvas layers.
// Single source of truth: both the shader matrix and the label renderer
// must use the same world→screen mapping.

export interface Camera {
  x: number;      // world-space center X
  y: number;      // world-space center Y
  zoom: number;   // pixels per world unit
  width: number;  // viewport width in CSS pixels
  height: number; // viewport height in CSS pixels
}

/**
 * Build the 3x3 view matrix for the WebGL vertex shader.
 * Maps world coordinates to clip space [-1, 1].
 *
 * Convention: +Y is up in world space, +Y is up in clip space.
 * The matrix is column-major for WebGL uniformMatrix3fv.
 */
export function buildViewMatrix(cam: Camera): Float32Array {
  const sx = 2 * cam.zoom / cam.width;
  const sy = 2 * cam.zoom / cam.height;
  const tx = -cam.x * sx;
  const ty = -cam.y * sy;
  return new Float32Array([sx, 0, 0, 0, sy, 0, tx, ty, 1]);
}

/**
 * Convert world coordinates to screen (CSS pixel) coordinates.
 * Must produce the same result as the vertex shader + viewport transform.
 */
export function worldToScreen(cam: Camera, wx: number, wy: number): [number, number] {
  const sx = (wx - cam.x) * cam.zoom + cam.width / 2;
  const sy = -(wy - cam.y) * cam.zoom + cam.height / 2;
  return [sx, sy];
}

/**
 * Verify that worldToScreen matches the shader matrix.
 * The shader does: clip = matrix * [wx, wy, 1]
 * Then viewport maps clip to screen: screenX = (clipX+1)/2 * width
 *                                    screenY = (1-clipY)/2 * height
 */
export function verifyTransform(cam: Camera, wx: number, wy: number): { match: boolean; shaderScreen: [number, number]; labelScreen: [number, number] } {
  const m = buildViewMatrix(cam);
  // matrix * [wx, wy, 1] (column-major: m[0]=sx, m[3]=0, m[6]=tx, etc.)
  const clipX = m[0] * wx + m[3] * wy + m[6];
  const clipY = m[1] * wx + m[4] * wy + m[7];
  const shaderScreenX = (clipX + 1) / 2 * cam.width;
  const shaderScreenY = (1 - clipY) / 2 * cam.height;

  const [labelX, labelY] = worldToScreen(cam, wx, wy);

  const match = Math.abs(shaderScreenX - labelX) < 0.01 && Math.abs(shaderScreenY - labelY) < 0.01;
  return {
    match,
    shaderScreen: [shaderScreenX, shaderScreenY],
    labelScreen: [labelX, labelY],
  };
}
