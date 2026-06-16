// shaders.ts — GLSL shaders for node circles and edge lines.

export const NODE_VERT = `#version 300 es
precision highp float;

// Per-instance attributes
in vec2 a_position;
in float a_size;
in vec4 a_color;
in vec4 a_pickColor;

// Per-vertex attribute (3 vertices per triangle instance)
in vec2 a_corner;

uniform mat3 u_matrix;
uniform float u_pixelRatio;
uniform bool u_picking;

out vec4 v_color;
out vec2 v_uv;

void main() {
  float radius = a_size * u_pixelRatio;
  vec2 pos = a_position + a_corner * radius;
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
    fragColor = dist < 1.0 ? v_color : vec4(0.0);
    return;
  }
  // SDF circle with antialiased edge
  float aa = fwidth(dist);
  float alpha = 1.0 - smoothstep(1.0 - aa, 1.0 + aa, dist);
  if (alpha < 0.01) discard;
  fragColor = vec4(v_color.rgb, v_color.a * alpha);
}
`;

export const EDGE_VERT = `#version 300 es
precision highp float;

in vec2 a_source;
in vec2 a_target;
in vec4 a_color;
in float a_offset; // -1 or +1 for the two vertices forming the thick line

uniform mat3 u_matrix;
uniform float u_width;

out vec4 v_color;

void main() {
  vec2 dir = a_target - a_source;
  vec2 normal = normalize(vec2(-dir.y, dir.x));
  vec2 pos = mix(a_source, a_target, step(0.0, a_offset)) + normal * a_offset * u_width;
  vec3 projected = u_matrix * vec3(pos, 1.0);
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

// Edge quad offsets: two triangles forming a thick line segment
export const EDGE_OFFSETS = new Float32Array([
  -1, 1, -1, 1, -1, 1,
]);
