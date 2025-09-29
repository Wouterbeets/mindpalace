shader_type spatial;
render_mode blend_mix, depth_draw_opaque, cull_disabled, diffuse_burley, specular_disabled;

uniform float height_scale : hint_range(0.0, 2.0) = 0.1;
uniform sampler2D noise_tex : source_color, filter_linear_mipmap;
uniform sampler2D terrain_tex : source_color, filter_linear_mipmap, repeat_enable;  // Your texture (e.g., grass/rock PNG)
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
    vec3 blend_weights = abs(NORMAL);  // Triplanar weights (stronger on steep slopes)
    blend_weights = normalize(max(blend_weights, vec3(0.00001)));  // Avoid div0

    // Sample texture on each axis
    vec2 tex_xy = world_pos.xy * tex_scale;
    vec2 tex_xz = world_pos.xz * tex_scale;
    vec2 tex_yz = world_pos.zy * tex_scale;

    vec3 tex_x = texture(terrain_tex, tex_xy).rgb;
    vec3 tex_y = texture(terrain_tex, tex_yz).rgb;
    vec3 tex_z = texture(terrain_tex, tex_xz).rgb;

    // Blend based on normal
    vec3 tex_color = tex_x * blend_weights.z + tex_y * blend_weights.x + tex_z * blend_weights.y;

    // Mix with your etched monochrome (50/50 for subtlety; tweak the lerp factor)
    ALBEDO = mix(vec3(0.15, 0.15, 0.18), tex_color, 0.5);
    ALPHA = 0.3;  // Ghostly translucent
    EMISSION = vec3(0.05);  // Barely-there silver whisper
    ROUGHNESS = 1.0;  // Matte (clamped from your 3.0)
    SPECULAR = 0.0;  // No shine
}