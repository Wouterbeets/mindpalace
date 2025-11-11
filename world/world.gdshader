shader_type spatial;
render_mode blend_mix, unshaded, depth_draw_always, cull_disabled;

uniform float grid_scale : hint_range(0.01, 1.0) = 0.06;
uniform float grid_thickness : hint_range(0.001, 0.2) = 0.02;
uniform vec3 base_color : source_color = vec3(0.1529, 0.1568, 0.1333);
uniform vec3 grid_color : source_color = vec3(0.9922, 0.5921, 0.1215);
uniform float pulse_speed : hint_range(0.0, 5.0) = 0.8;
uniform float pulse_amplitude : hint_range(0.0, 2.0) = 0.25;
uniform float hover_height : hint_range(0.0, 2.0) = 0.08;

varying vec3 world_pos_var;

void vertex() {
    vec3 world_pos = (MODEL_MATRIX * vec4(VERTEX, 1.0)).xyz;
    float wave = sin(world_pos.x * 0.035 + TIME * 0.8) + sin(world_pos.z * 0.05 + TIME * 0.6);
    float ripple = sin(length(world_pos.xz) * 0.1 - TIME * 1.2);
    VERTEX.y = (wave + ripple) * hover_height * 0.05;
    world_pos_var = (MODEL_MATRIX * vec4(VERTEX, 1.0)).xyz;
}

void fragment() {
    vec2 grid_uv = world_pos_var.xz * grid_scale;
    vec2 cell = abs(fract(grid_uv) - 0.5);
    float line_distance = min(cell.x, cell.y);
    float glow_band = smoothstep(grid_thickness, grid_thickness * 0.3, line_distance);

    float pulse = 0.35 + pulse_amplitude * 0.35 * (sin(TIME * pulse_speed + world_pos_var.x * 0.08) * 0.5 + 0.5);
    vec3 neon = grid_color * glow_band * pulse;

    ALBEDO = base_color + neon * 0.12;
    EMISSION = neon * 0.7;
    ALPHA = 0.98;
}
