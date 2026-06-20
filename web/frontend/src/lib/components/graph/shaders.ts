// shaders.ts — GLSL shaders for instanced node circles and simple edge lines.

export const NODE_VERT = `#version 300 es
precision highp float;

in vec2 a_position;
in float a_size;
in vec4 a_color;
in vec4 a_pickColor;
in vec2 a_corner;

uniform mat3 u_matrix;
uniform bool u_picking;

out vec4 v_color;
out vec2 v_uv;

void main() {
  vec2 pos = a_position + a_corner * a_size;
  vec3 projected = u_matrix * vec3(pos, 1.0);
  gl_Position = vec4(projected.xy, 0.0, 1.0);
  v_color = u_picking ? a_pickColor : a_color;
  v_uv = a_corner;
}
`;

export const NODE_FRAG = `#version 300 es
precision highp float;

in vec4 v_color;
in vec2 v_uv;
uniform bool u_picking;
out vec4 fragColor;

void main() {
  float dist = length(v_uv);
  if (u_picking) {
    fragColor = dist < 0.9 ? v_color : vec4(0.0);
    return;
  }
  float aa = fwidth(dist) * 1.5;
  float outer = 1.0 - smoothstep(0.9 - aa, 0.9, dist);
  if (outer < 0.01) discard;
  // Hollow mode: alpha < 0.3 → draw only a thin ring border, discard interior
  if (v_color.a < 0.3) {
    float inner = smoothstep(0.88 - aa, 0.88, dist);
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

// Quad corners for instanced node rendering (unit square → circle SDF)
export const NODE_CORNERS = new Float32Array([
  -1, -1,
   1, -1,
  -1,  1,
  -1,  1,
   1, -1,
   1,  1,
]);
