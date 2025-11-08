shader_type spatial;
render_mode blend_mix, unshaded, depth_draw_always, cull_disabled;

uniform float grid_scale : hint_range(0.01, 1.0) = 0.08;
uniform float grid_thickness : hint_range(0.001, 0.2) = 0.025;
uniform vec3 base_color : source_color = vec3(0.01, 0.035, 0.05);
uniform vec3 grid_color : source_color = vec3(0.0, 0.85, 1.0);
uniform float pulse_speed : hint_range(0.0, 5.0) = 1.4;
uniform float pulse_amplitude : hint_range(0.0, 2.0) = 0.6;
uniform float hover_height : hint_range(0.0, 2.0) = 0.25;

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

    float pulse = 0.4 + pulse_amplitude * 0.5 * (sin(TIME * pulse_speed + world_pos_var.x * 0.12) * 0.5 + 0.5);
    vec3 neon = grid_color * glow_band * pulse;

    ALBEDO = base_color + neon * 0.1;
    EMISSION = neon * 3.5;
    ALPHA = 0.92;
}
