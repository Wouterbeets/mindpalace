shader_type spatial;
render_mode blend_mix, depth_draw_opaque, cull_disabled, diffuse_burley, specular_disabled;

uniform float height_scale : hint_range(0.0, 2.0) = 0.1;
uniform sampler2D noise_tex : source_color, filter_linear_mipmap;

uniform float tex_scale : hint_range(0.1, 10.0) = 1.0;  // Controls texture tiling (lower = larger tiles)

varying vec3 world_pos_var;  // Pass displaced world pos to fragment

void vertex() {
    vec3 undisplaced_world = (MODEL_MATRIX * vec4(VERTEX, 1.0)).xyz;
    float noise = texture(noise_tex, undisplaced_world.xz * 0.005).r;
    VERTEX.y += (noise - 0.5) * height_scale * 20.0;  // Displace local pos
    world_pos_var = (MODEL_MATRIX * vec4(VERTEX, 1.0)).xyz;  // Displaced world pos
}

void fragment() {
    vec3 world_pos = world_pos_var;
    float noise = texture(noise_tex, world_pos.xz * 0.005).r;

    // Simple height-based coloring
    ALBEDO = mix(vec3(0.2, 0.4, 0.2), vec3(0.6, 0.3, 0.1), noise);
    ALPHA = 0.3;  // Ghostly translucent
    EMISSION = vec3(0.05);  // Barely-there silver whisper
    ROUGHNESS = 1.0;  // Matte
    SPECULAR = 0.0;  // No shine
}