shader_type spatial;
render_mode blend_mix, depth_draw_opaque, cull_disabled, diffuse_burley, specular_disabled;

uniform float height_scale : hint_range(0.0, 2.0) = 0.1;
uniform sampler2D noise_tex : source_color, filter_linear_mipmap;

void vertex() {
    vec3 world_pos = (MODEL_MATRIX * vec4(VERTEX, 1.0)).xyz;
    float noise = texture(noise_tex, world_pos.xz * 0.005).r;
    VERTEX.y += (noise - 0.5) * height_scale * 20.0;  // Amplified for visible hills on subdivided mesh
}

void fragment() {
    // Etched monochrome: No color intrusion
    ALBEDO = vec3(0.15, 0.15, 0.18);
    ALPHA = 0.3;  // Ghostly translucent
    EMISSION = vec3(0.05);  // Barely-there silver whisper
    ROUGHNESS = 3.0;  // Matte, unyielding
    SPECULAR = 0.0;  // No shine
}