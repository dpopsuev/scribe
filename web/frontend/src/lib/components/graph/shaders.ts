// shaders.ts — GLSL shaders for instanced shape-varied nodes and simple edge lines.

export const NODE_VERT = `#version 300 es
precision highp float;

in vec2 a_position;
in float a_size;
in vec4 a_color;
in vec4 a_pickColor;
in vec2 a_corner;
in float a_shape;

uniform mat3 u_matrix;
uniform bool u_picking;

out vec4 v_color;
out vec2 v_uv;
flat out int v_shape;

void main() {
  vec2 pos = a_position + a_corner * a_size;
  vec3 projected = u_matrix * vec3(pos, 1.0);
  gl_Position = vec4(projected.xy, 0.0, 1.0);
  v_color = u_picking ? a_pickColor : a_color;
  v_uv = a_corner;
  v_shape = int(a_shape + 0.5);
}
`;

export const NODE_FRAG = `#version 300 es
precision highp float;

in vec4 v_color;
in vec2 v_uv;
flat in int v_shape;
uniform bool u_picking;
out vec4 fragColor;

const float PI = 3.14159265;

// SDF for regular polygon with n sides
float sdPolygon(vec2 p, int n, float r) {
  float an = PI / float(n);
  float a = atan(p.y, p.x);
  float sector = floor(a / (2.0 * an) + 0.5);
  a -= sector * 2.0 * an;
  return length(p) * cos(a) - r * cos(an);
}

// SDF for a 5-pointed star
float sdStar(vec2 p, float r) {
  p = vec2(abs(p.x), p.y);
  float an = PI / 5.0;
  float bn = atan(p.x, p.y) / (2.0 * an);
  bn = floor(bn + 0.5);
  float a = bn * 2.0 * an;
  vec2 rn = vec2(cos(a), sin(a));
  float d1 = dot(p, vec2(-rn.y, rn.x));
  float innerR = r * 0.38;
  float d2 = length(p) - innerR;
  return max(d1 - r * sin(an) * 0.7, -d2 + innerR * 0.3);
}

// SDF for rounded rectangle
float sdRoundedRect(vec2 p, vec2 b, float r) {
  vec2 q = abs(p) - b + r;
  return length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - r;
}

// Returns distance from edge (negative = inside, positive = outside)
float shapeSDF(vec2 uv, int shape) {
  // 0: circle
  if (shape == 0) return length(uv) - 0.82;
  // 1: square
  if (shape == 1) {
    vec2 d = abs(uv) - vec2(0.72);
    return length(max(d, 0.0)) + min(max(d.x, d.y), 0.0);
  }
  // 2: diamond (rotated square)
  if (shape == 2) {
    vec2 r = vec2(uv.x + uv.y, uv.x - uv.y) * 0.7071;
    vec2 d = abs(r) - vec2(0.62);
    return length(max(d, 0.0)) + min(max(d.x, d.y), 0.0);
  }
  // 3: triangle (pointing up)
  if (shape == 3) {
    vec2 p = vec2(uv.x, -uv.y + 0.15);
    return sdPolygon(p, 3, 0.72);
  }
  // 4: hexagon
  if (shape == 4) return sdPolygon(uv, 6, 0.76);
  // 5: pentagon
  if (shape == 5) {
    vec2 p = vec2(uv.x, -uv.y + 0.06);
    return sdPolygon(p, 5, 0.74);
  }
  // 6: star
  if (shape == 6) return sdStar(uv, 0.78);
  // 7: rounded rect
  if (shape == 7) return sdRoundedRect(uv, vec2(0.78, 0.56), 0.18);
  // fallback: circle
  return length(uv) - 0.82;
}

void main() {
  float dist = shapeSDF(v_uv, v_shape);

  if (u_picking) {
    fragColor = dist < 0.0 ? v_color : vec4(0.0);
    return;
  }

  float aa = fwidth(dist) * 1.5;
  float outer = 1.0 - smoothstep(-aa, aa, dist);
  if (outer < 0.01) discard;

  // Hollow mode: alpha < 0.3 → draw only a thin border ring
  if (v_color.a < 0.3) {
    float inner = smoothstep(-0.04 - aa, -0.04, dist);
    float ring = outer * inner;
    if (ring < 0.01) discard;
    fragColor = vec4(v_color.rgb, ring * 0.85);
    return;
  }

  fragColor = vec4(v_color.rgb, v_color.a * outer);
}
`;

export const EDGE_VERT = `#version 300 es
precision highp float;

in vec2 a_position;
in vec4 a_color;

uniform mat3 u_matrix;

out vec4 v_color;

void main() {
  vec3 projected = u_matrix * vec3(a_position, 1.0);
  gl_Position = vec4(projected.xy, 0.0, 1.0);
  v_color = a_color;
}
`;

export const EDGE_FRAG = `#version 300 es
precision highp float;

in vec4 v_color;
out vec4 fragColor;

void main() {
  fragColor = v_color;
}
`;

// Quad corners for instanced node rendering (unit square → shape SDF)
export const NODE_CORNERS = new Float32Array([
  -1, -1,
   1, -1,
  -1,  1,
  -1,  1,
   1, -1,
   1,  1,
]);
