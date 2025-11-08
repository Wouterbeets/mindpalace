extends Node3D

@onready var mesh_instance: MeshInstance3D = $Floor/MeshInstance3D
@onready var camera: Camera3D = $Player/Camera
@onready var zone_visualizer: Node3D = $ZoneVisualizer

var websocket = WebSocketPeer.new()
const WS_URL = "ws://localhost:8081/godot"
var connected = false
var sent_start_signal = false

var event_count = 0
var debug_timer = 0.0
const DEBUG_INTERVAL = 5.0	# Send debug every 5 seconds
# Removed zone and spacing logic - positions come from server

# Mapping of event types to colors
const EVENT_COLORS = {
	"user_request_received": Color(0.0, 0.82, 1.0, 1.0),
	"task_created": Color(0.18, 1.0, 0.62, 1.0),
	"task_updated": Color(0.42, 0.78, 1.0, 1.0),
	"task_completed": Color(0.1, 0.95, 0.4, 1.0),
	"task_deleted": Color(0.98, 0.22, 0.42, 1.0),
	"task": Color(0.0, 0.62, 0.92, 1.0),
	"note_created": Color(0.95, 0.32, 0.9, 1.0),
	"note_updated": Color(0.88, 0.52, 1.0, 1.0),
	"note_deleted": Color(0.72, 0.1, 0.38, 1.0),
	"note": Color(0.86, 0.44, 1.0, 1.0),
	"calendar_event_created": Color(0.38, 0.92, 1.0, 1.0),
	"calendar_event_updated": Color(0.45, 0.74, 1.0, 1.0),
	"calendar_event_deleted": Color(0.96, 0.24, 0.35, 1.0),
	"calendar_event": Color(0.3, 0.85, 1.0, 1.0),
	"plugin_generated": Color(0.85, 0.18, 1.0, 1.0),
	"request_completed": Color(0.0, 0.92, 0.78, 1.0),
	"agent_call_decided": Color(0.96, 0.4, 1.0, 1.0),
	"agent_execution_failed": Color(1.0, 0.2, 0.2, 1.0),
	"tool_call_failed": Color(1.0, 0.32, 0.12, 1.0),
	"tool_call_started": Color(0.98, 0.66, 0.2, 1.0),
	"tool_call_completed": Color(0.0, 0.95, 0.95, 1.0),
	"orchestrator_ai": Color(1.0, 0.92, 0.5, 1.0),
}

const UI_BG = Color(0.02, 0.05, 0.08, 0.9)
const UI_BG_DEEP = Color(0.01, 0.02, 0.04, 0.96)
const UI_PANEL_BORDER = Color(0.0, 0.82, 1.0, 1.0)
const UI_ACCENT = Color(0.85, 0.18, 1.0, 1.0)
const UI_TEXT = Color(0.82, 0.95, 1.0, 1.0)
const UI_MUTED_TEXT = Color(0.38, 0.75, 0.9, 1.0)
const LABEL_OUTLINE_COLOR = Color(0.02, 0.24, 0.36, 1.0)
const SCREEN_OVERLAY_SHADER: Shader = preload("res://screen_overlay.gdshader")

const TASK_STATUS_OFFSETS := {
	"Pending": 0.0,
	"In Progress": 10.0,
	"Completed": 20.0,
	"Blocked": 30.0,
}

const ZONE_CAMERA_FLIGHT_DURATION := 1.8
const ZONE_CAMERA_MIN_HEIGHT := 6.0
const ZONE_CAMERA_MIN_RETREAT := 4.0

# Store cubes by event ID for updates/deletes
var event_cubes = {}
var model_cache := {}

# UI for info panel
var info_panel: Panel
var info_label: Label
var info_actions_container: VBoxContainer
var conversation_panel: Panel
var conversation_title_label: Label
var conversation_history: RichTextLabel
var conversation_input: LineEdit
var conversation_send_button: Button
var current_conversation_agent: String = ""

# Targeting HUD
var targeting_hud_panel: Panel
var targeting_hud_label: RichTextLabel
var targeted_object: Node = null
var targeting_reticle: ColorRect

# Zone flyover UI
var zone_panel: Panel = null
var zone_button_container: VBoxContainer = null
var zone_scroll_container: ScrollContainer = null
var zone_camera_targets: Dictionary = {}
var zone_flight_tween: Tween = null
var zone_last_focus: Vector3 = Vector3.ZERO

var last_sequence_id: int = 0


func _is_auxiliary_node_id(node_id: String) -> bool:
	return node_id.ends_with("_accent") or node_id.ends_with("_shadow") or node_id.ends_with("_label") or node_id.ends_with("_meta") or node_id.ends_with("_model")


func _ensure_event_entry(node_id: String) -> Dictionary:
	if not event_cubes.has(node_id):
		event_cubes[node_id] = {}
	return event_cubes[node_id]


func _find_registered_entry_for_node(node: Node) -> Dictionary:
	for id in event_cubes.keys():
		var entry: Dictionary = event_cubes[id]
		if entry.get("auxiliary", false):
			continue
		if entry.get("node", null) == node:
			return {
				"id": id,
				"entry": entry,
			}
	return {}


func _update_status_origin(entry: Dictionary):
	if not entry.has("component_properties"):
		return
	var props: Dictionary = entry["component_properties"]
	if not props.has("status"):
		return
	var status := str(props["status"])
	if not TASK_STATUS_OFFSETS.has(status):
		return
	var node: Node3D = entry.get("node", null)
	if node == null:
		return
	entry["status_base_origin"] = node.position.x - TASK_STATUS_OFFSETS[status]
	entry["status"] = status


func _determine_status_from_position(entry: Dictionary, position: Vector3) -> String:
	if not entry.has("status_base_origin"):
		return ""
	var base_origin: float = float(entry["status_base_origin"])
	var closest_status: String = ""
	var closest_distance: float = INF
	for status in TASK_STATUS_OFFSETS.keys():
		var target: float = base_origin + TASK_STATUS_OFFSETS[status]
		var distance: float = abs(position.x - target)
		if distance < closest_distance:
			closest_distance = distance
			closest_status = status
	return closest_status


func _lookup_path(container, path: String):
	if path == null or path == "":
		return container
	var segments := path.split(".")
	var current = container
	for segment in segments:
		if current is Dictionary:
			if not current.has(segment):
				return null
			current = current[segment]
		elif current is Array:
			var index := int(segment)
			if index < 0 or index >= current.size():
				return null
			current = current[index]
		else:
			return null
	return current


func _resolve_binding(binding: Dictionary, component_props: Dictionary, context: Dictionary) -> Variant:
	var source := str(binding.get("source", "static")).to_lower()
	match source:
		"static":
			return binding.get("value")
		"context":
			return _lookup_path(context, binding.get("path", ""))
		"component":
			return _lookup_path(component_props, binding.get("path", ""))
		"user_input":
			var user_input: Dictionary = {}
			if context.has("user_input") and context["user_input"] is Dictionary:
				user_input = context["user_input"]
			return _lookup_path(user_input, binding.get("path", ""))
		_:
			return binding.get("value")


func _resolve_command_descriptor(descriptor: Dictionary, component_props: Dictionary, context: Dictionary) -> Dictionary:
	var command_name := str(descriptor.get("command", ""))
	if command_name == "":
		return {}
	var args := {}
	var bindings = descriptor.get("arguments", {})
	if bindings is Dictionary:
		for arg_name in bindings.keys():
			var binding = bindings[arg_name]
			if binding is Dictionary:
				var value = _resolve_binding(binding, component_props, context)
				if value != null:
					args[arg_name] = value
	return {
		"command": command_name,
		"data": args,
	}


func dispatch_component_action(node_id: String, action_key: String, context := {}) -> bool:
	if typeof(context) != TYPE_DICTIONARY:
		context = {}
	var entry: Dictionary = event_cubes.get(node_id, null)
	if entry == null:
		return false
	var component = entry.get("component", null)
	if component == null:
		return false
	var actions = component.get("actions", {})
	if typeof(actions) != TYPE_DICTIONARY or not actions.has(action_key):
		return false
	var action = actions[action_key]
	if typeof(action) != TYPE_DICTIONARY:
		return false

	var command_name := ""
	var payload := {}
	var component_props: Dictionary = entry.get("component_properties", {})

	if action.has("command_descriptor") and action["command_descriptor"] is Dictionary:
		var resolved := _resolve_command_descriptor(action["command_descriptor"], component_props, context)
		command_name = resolved.get("command", "")
		payload = resolved.get("data", {})
	elif action.has("data") and action["data"] is Dictionary:
		var data_dict: Dictionary = action["data"]
		command_name = str(data_dict.get("command", ""))
		payload = data_dict.get("data", {})

	if command_name == "":
		return false

	if not send_ui_command(command_name, payload):
		return false

	if context.has("user_input") and component_props is Dictionary:
		var user_input: Dictionary = context["user_input"]
		if action_key == "rename" and user_input.has("text"):
			component_props["title"] = user_input["text"]
		if action_key == "edit_description" and user_input.has("text"):
			component_props["description"] = user_input["text"]
		entry["component_properties"] = component_props
	if context.has("drop") and context["drop"] is Dictionary:
		var drop_ctx: Dictionary = context["drop"]
		if drop_ctx.has("zone"):
			component_props["status"] = drop_ctx["zone"]
			entry["component_properties"] = component_props
			_update_status_origin(entry)
	if action_key == "complete":
		component_props["status"] = "Completed"
		entry["component_properties"] = component_props
		_update_status_origin(entry)
	return true


func send_ui_command(command_name: String, payload: Dictionary) -> bool:
	if command_name == "":
		return false
	if websocket.get_ready_state() != WebSocketPeer.STATE_OPEN:
		push_error("Cannot send UI command '" + command_name + "': WebSocket not open")
		return false
	var message := {
		"type": "ui_action",
		"action": {
			"command": command_name,
			"data": payload,
		},
	}
	var err = websocket.send_text(JSON.stringify(message))
	if err != OK:
		push_error("Failed to send UI command '" + command_name + "': " + str(err))
		return false
	log_message("Dispatched UI command '" + command_name + "' with payload " + str(payload))
	return true


# Microphone settings menu
var settings_panel: Panel
var settings_label: Label

var settings_visible: bool = false
var dark_mode: bool = true	# Start in dark mode
var dark_mode_button: Button

# User request input
var user_request_input: LineEdit
var send_request_button: Button

# Game log
var game_log_panel: Panel
var game_log_label: Label
var game_log_text: String = ""

# Environment reference for dynamic updates
var world_env: WorldEnvironment = null
var env: Environment = null
var ambient_particles: GPUParticles3D = null
var sun_light: DirectionalLight3D = null
var core_light: OmniLight3D = null
var fog_density_slider: HSlider = null
var ambient_light_slider: HSlider = null
var sun_energy_slider: HSlider = null
var volumetric_fog_slider: HSlider = null
var screen_overlay_rect: ColorRect = null
# Underground scene for event animations
var underground_node: Node3D = null

# Splash screen
var splash_finished = false

# Orchestrator AI animation
var orchestrator_tween: Tween = null
var orchestrator_animated = false

# Birdview camera mode
var birdview_active = false
# var birdview_camera_pos = Vector3(0, 80, 0)
# var birdview_camera_rot = Vector3(-PI/3, 0, 0)  # 60 degrees down
# var tween = null

# Orchestrator menu
var orchestrator_menu: Node = null

# Model placement UI
var model_picker_layer: CanvasLayer = null
var model_picker_panel: Panel = null
var model_picker_search: LineEdit = null
var model_picker_results: VBoxContainer = null
var model_catalog_loaded: bool = false
var model_catalog_entries: Array = []
var pending_model_id: String = ""
var placement_hint: Label = null


func _apply_neon_panel(panel: Panel, border_color: Color = UI_PANEL_BORDER, bg_color: Color = UI_BG, border_thickness: int = 2, radius: int = 12):
	var style_box = StyleBoxFlat.new()
	style_box.bg_color = bg_color
	style_box.border_color = border_color
	style_box.border_width_left = border_thickness
	style_box.border_width_right = border_thickness
	style_box.border_width_top = border_thickness
	style_box.border_width_bottom = border_thickness
	style_box.corner_radius_top_left = radius
	style_box.corner_radius_top_right = radius
	style_box.corner_radius_bottom_left = radius
	style_box.corner_radius_bottom_right = radius
	style_box.corner_detail = 4
	panel.add_theme_stylebox_override("panel", style_box)


func _apply_neon_label(label: Label, font_size: int = 16, muted: bool = false):
	label.add_theme_font_size_override("font_size", font_size)
	var font_color = UI_MUTED_TEXT if muted else UI_TEXT
	label.add_theme_color_override("font_color", font_color)


func _apply_neon_button(button: Button, font_size: int = 16):
	var normal = StyleBoxFlat.new()
	normal.bg_color = UI_BG_DEEP
	normal.border_color = UI_PANEL_BORDER
	normal.border_width_left = 2
	normal.border_width_right = 2
	normal.border_width_top = 2
	normal.border_width_bottom = 2
	normal.corner_radius_bottom_left = 10
	normal.corner_radius_bottom_right = 10
	normal.corner_radius_top_left = 10
	normal.corner_radius_top_right = 10

	var hover = normal.duplicate()
	hover.bg_color = UI_BG_DEEP.lightened(0.05)
	hover.border_color = UI_ACCENT

	var pressed = normal.duplicate()
	pressed.bg_color = UI_BG_DEEP.darkened(0.15)
	pressed.border_color = UI_ACCENT

	button.add_theme_stylebox_override("normal", normal)
	button.add_theme_stylebox_override("hover", hover)
	button.add_theme_stylebox_override("pressed", pressed)
	button.add_theme_color_override("font_color", UI_TEXT)
	button.add_theme_font_size_override("font_size", font_size)


func _apply_neon_line_edit(line_edit: LineEdit, font_size: int = 16):
	var base = StyleBoxFlat.new()
	base.bg_color = Color(UI_BG_DEEP.r, UI_BG_DEEP.g, UI_BG_DEEP.b, 0.75)
	base.border_color = UI_PANEL_BORDER
	base.border_width_left = 2
	base.border_width_right = 2
	base.border_width_top = 2
	base.border_width_bottom = 2
	base.corner_radius_bottom_left = 8
	base.corner_radius_bottom_right = 8
	base.corner_radius_top_left = 8
	base.corner_radius_top_right = 8

	var focus = base.duplicate()
	focus.border_color = UI_ACCENT
	focus.bg_color = base.bg_color.lightened(0.1)

	line_edit.add_theme_stylebox_override("normal", base)
	line_edit.add_theme_stylebox_override("focus", focus)
	line_edit.add_theme_stylebox_override("read_only", base)
	line_edit.add_theme_color_override("font_color", UI_TEXT)
	line_edit.add_theme_color_override("placeholder_color", UI_MUTED_TEXT)
	line_edit.add_theme_font_size_override("font_size", font_size)


func _build_neon_material(base_color: Color, alpha: float = 1.0) -> StandardMaterial3D:
	var material = StandardMaterial3D.new()
	var dimmed = Color(base_color.r * 0.18, base_color.g * 0.18, base_color.b * 0.18, alpha)
	material.albedo_color = dimmed
	material.metallic = 0.12
	material.roughness = 0.18
	material.emission_enabled = true
	material.emission = base_color
	material.emission_energy_multiplier = 1.4
	material.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA if alpha < 1.0 else BaseMaterial3D.TRANSPARENCY_DISABLED
	material.depth_draw_mode = BaseMaterial3D.DEPTH_DRAW_ALWAYS
	return material


func _array_to_color(value, fallback: Color) -> Color:
	if value is Array and value.size() >= 3:
		var r = clamp(float(value[0]), 0.0, 1.0)
		var g = clamp(float(value[1]), 0.0, 1.0)
		var b = clamp(float(value[2]), 0.0, 1.0)
		var a = fallback.a
		if value.size() > 3:
			a = clamp(float(value[3]), 0.0, 1.0)
		return Color(r, g, b, a)
	return fallback


func _create_screen_overlay():
	var overlay_layer = CanvasLayer.new()
	overlay_layer.layer = 100
	add_child(overlay_layer)

	screen_overlay_rect = ColorRect.new()
	screen_overlay_rect.size = get_viewport().size
	screen_overlay_rect.material = ShaderMaterial.new()
	screen_overlay_rect.material.shader = SCREEN_OVERLAY_SHADER
	screen_overlay_rect.mouse_filter = Control.MOUSE_FILTER_IGNORE
	overlay_layer.add_child(screen_overlay_rect)

	get_viewport().size_changed.connect(func():
		if screen_overlay_rect:
			screen_overlay_rect.size = get_viewport().size
	)


func _augment_orchestrator(node: MeshInstance3D):
	node.name = "orchestrator_ai"
	if node.has_node("OrchestratorHalo"):
		node.get_node("OrchestratorHalo").queue_free()
	if node.mesh == null or not (node.mesh is SphereMesh):
		var sphere = SphereMesh.new()
		sphere.radial_segments = 64
		sphere.rings = 32
		sphere.radius = 0.8
		node.mesh = sphere
	node.scale = Vector3.ONE * 1.25

	var core_material = StandardMaterial3D.new()
	core_material.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
	core_material.albedo_color = Color(0.98, 0.98, 1.0, 0.25)
	core_material.metallic = 0.0
	core_material.roughness = 0.08
	core_material.emission_enabled = true
	core_material.emission = Color(0.95, 0.98, 1.0, 1.0)
	core_material.emission_energy_multiplier = 3.2
	core_material.subsurface_scattering_enabled = true
	core_material.subsurface_scattering_strength = 0.7
	core_material.subsurface_scattering_color = Color(1.0, 0.98, 0.93, 1.0)
	core_material.refraction_enabled = true
	core_material.refraction_scale = 0.02
	node.material_override = core_material

	if not node.has_node("OrchestratorLight"):
		var light = OmniLight3D.new()
		light.name = "OrchestratorLight"
		light.light_color = Color(1.0, 0.96, 0.85, 1.0)
		light.light_energy = 9.0
		light.omni_range = 42.0
		light.shadow_enabled = false
		node.add_child(light)

	if node.has_node("OrchestratorAura"):
		node.get_node("OrchestratorAura").queue_free()
	if node.has_node("OrchestratorMistShell"):
		node.get_node("OrchestratorMistShell").queue_free()

	var inner_aura = GPUParticles3D.new()
	inner_aura.name = "OrchestratorAura"
	var inner_material = ParticleProcessMaterial.new()
	inner_material.emission_shape = ParticleProcessMaterial.EMISSION_SHAPE_SPHERE
	inner_material.emission_sphere_radius = 0.15
	inner_material.initial_velocity_min = 0.12
	inner_material.initial_velocity_max = 0.4
	inner_material.gravity = Vector3.ZERO
	inner_material.orbit_velocity = 0.4
	inner_material.angular_velocity_min = -0.8
	inner_material.angular_velocity_max = 0.8
	inner_material.scale_min = 0.5
	inner_material.scale_max = 1.1
	inner_material.color = Color(0.85, 0.95, 1.0, 0.35)
	inner_material.trail_enabled = true
	inner_material.trail_divisor = 8
	inner_material.trail_lifetime = 0.9
	inner_aura.process_material = inner_material
	inner_aura.amount = 220
	inner_aura.lifetime = 3.2
	inner_aura.speed_scale = 0.9
	node.add_child(inner_aura)

	var mist_shell = GPUParticles3D.new()
	mist_shell.name = "OrchestratorMistShell"
	var mist_material = ParticleProcessMaterial.new()
	mist_material.emission_shape = ParticleProcessMaterial.EMISSION_SHAPE_SPHERE
	mist_material.emission_sphere_radius = 0.6
	mist_material.initial_velocity_min = 0.05
	mist_material.initial_velocity_max = 0.18
	mist_material.gravity = Vector3.ZERO
	mist_material.orbit_velocity = 0.12
	mist_material.angular_velocity_min = -0.2
	mist_material.angular_velocity_max = 0.2
	mist_material.scale_min = 0.9
	mist_material.scale_max = 1.7
	mist_material.color = Color(0.92, 0.99, 1.0, 0.16)
	mist_material.damping = 0.02
	mist_material.radial_velocity_min = -0.08
	mist_material.radial_velocity_max = 0.12
	mist_shell.process_material = mist_material
	mist_shell.amount = 640
	mist_shell.lifetime = 5.0
	mist_shell.speed_scale = 0.6
	mist_shell.draw_pass_1 = SphereMesh.new()
	node.add_child(mist_shell)


func _ready():
	mesh_instance.extra_cull_margin = 2.0

	# Show splash screen first
	show_splash_screen()
	await splash_finished

	# Test animation immediately
	start_orchestrator_animation()

	# Removed zone separators - pure renderer

	# Create game log HUD
	create_game_log()

	# Removed CRT-style screen overlay for clarity


	# Set up info panel
	var canvas_layer = CanvasLayer.new()
	add_child(canvas_layer)
	info_panel = Panel.new()
	info_panel.size = Vector2(320, 320)
	info_panel.position = Vector2(10, 10)
	info_panel.visible = false
	_apply_neon_panel(info_panel)
	canvas_layer.add_child(info_panel)

	info_label = Label.new()
	info_label.position = Vector2(10, 10)
	info_label.size = Vector2(300, 100)
	info_label.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	_apply_neon_label(info_label, 16)
	info_panel.add_child(info_label)

	info_actions_container = VBoxContainer.new()
	info_actions_container.position = Vector2(10, 120)
	info_actions_container.size = Vector2(300, 180)
	info_actions_container.custom_minimum_size = Vector2(300, 180)
	info_actions_container.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	info_actions_container.spacing = 6
	info_panel.add_child(info_actions_container)

	conversation_panel = Panel.new()
	conversation_panel.size = Vector2(420, 360)
	conversation_panel.position = Vector2(get_viewport().size.x - 440, get_viewport().size.y - 380)
	conversation_panel.visible = false
	_apply_neon_panel(conversation_panel, UI_ACCENT, UI_BG_DEEP, 3)
	canvas_layer.add_child(conversation_panel)

	conversation_title_label = Label.new()
	conversation_title_label.position = Vector2(12, 12)
	conversation_title_label.size = Vector2(396, 28)
	_apply_neon_label(conversation_title_label, 22)
	conversation_panel.add_child(conversation_title_label)

	var conversation_header_rule = ColorRect.new()
	conversation_header_rule.color = Color(UI_ACCENT.r, UI_ACCENT.g, UI_ACCENT.b, 0.6)
	conversation_header_rule.size = Vector2(conversation_panel.size.x - 24, 2)
	conversation_header_rule.position = Vector2(12, 44)
	conversation_panel.add_child(conversation_header_rule)

	conversation_history = RichTextLabel.new()
	conversation_history.position = Vector2(12, 50)
	conversation_history.size = Vector2(396, 230)
	conversation_history.scroll_active = true
	conversation_history.scroll_following = true
	conversation_history.bbcode_enabled = false
	conversation_history.add_theme_color_override("default_color", UI_TEXT)
	conversation_history.add_theme_color_override("font_color_selected", UI_ACCENT)
	conversation_history.add_theme_constant_override("line_spacing", 2)
	conversation_panel.add_child(conversation_history)

	conversation_input = LineEdit.new()
	conversation_input.position = Vector2(12, 294)
	conversation_input.size = Vector2(290, 32)
	conversation_input.placeholder_text = "Type a message..."
	conversation_input.connect("text_submitted", Callable(self, "_on_conversation_text_submitted"))
	# Disable player movement while typing; show mouse for text input
	conversation_input.focus_entered.connect(Callable(self, "_on_text_input_focus_entered"))
	conversation_input.focus_exited.connect(Callable(self, "_on_text_input_focus_exited"))
	_apply_neon_line_edit(conversation_input, 16)
	conversation_panel.add_child(conversation_input)

	conversation_send_button = Button.new()
	conversation_send_button.text = "Transmit"
	conversation_send_button.position = Vector2(314, 294)
	conversation_send_button.size = Vector2(94, 32)
	conversation_send_button.pressed.connect(Callable(self, "_on_conversation_send_pressed"))
	_apply_neon_button(conversation_send_button, 16)
	conversation_panel.add_child(conversation_send_button)

	# Set up targeting HUD (top-right)
	targeting_hud_panel = Panel.new()
	targeting_hud_panel.size = Vector2(400, 300)
	targeting_hud_panel.position = Vector2(get_viewport().size.x - targeting_hud_panel.size.x - 20, 20)
	targeting_hud_panel.visible = false
	_apply_neon_panel(targeting_hud_panel, UI_PANEL_BORDER, UI_BG_DEEP, 3)
	canvas_layer.add_child(targeting_hud_panel)

	targeting_hud_label = RichTextLabel.new()
	targeting_hud_label.position = Vector2(10, 10)
	targeting_hud_label.size = Vector2(380, 280)
	targeting_hud_label.bbcode_enabled = true
	targeting_hud_label.fit_content = false
	targeting_hud_label.scroll_active = false
	targeting_hud_label.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	targeting_hud_label.add_theme_font_size_override("normal_font_size", 16)
	targeting_hud_label.add_theme_color_override("default_color", UI_TEXT)
	targeting_hud_label.add_theme_color_override("selection_color", UI_ACCENT)
	targeting_hud_panel.add_child(targeting_hud_label)

	# Add targeting reticle (center of screen)
	targeting_reticle = ColorRect.new()
	targeting_reticle.size = Vector2(6, 6)
	update_reticle_position()
	targeting_reticle.color = UI_PANEL_BORDER
	canvas_layer.add_child(targeting_reticle)

	# Connect to viewport size changes
	get_viewport().size_changed.connect(_on_viewport_size_changed)

	# Use existing WorldEnvironment from scene and bathe the space in neon night
	env = $WorldEnvironment.environment
	if env:
		env.background_mode = Environment.BG_COLOR
		env.background_color = Color(0.01, 0.015, 0.03, 1.0)
		env.ambient_light_color = Color(0.05, 0.32, 0.45, 1.0)
		env.ambient_light_energy = 0.35
		env.fog_enabled = true
		env.fog_color = Color(0.0, 0.25, 0.4, 0.8)
		env.fog_density = 0.01
		env.fog_height_min = -10.0
		env.fog_height_max = 25.0
		env.glow_enabled = true
		env.glow_intensity = 1.2
		env.glow_strength = 1.1
		env.glow_hdr_threshold = 0.6
		env.volumetric_fog_enabled = true
		env.volumetric_fog_density = 0.02
		env.volumetric_fog_emission = Color(0.05, 0.35, 0.5, 0.6)

	# Add stylised lighting
	sun_light = DirectionalLight3D.new()
	sun_light.name = "CyberSun"
	sun_light.rotation_degrees = Vector3(-35, 40, 0)
	sun_light.light_color = Color(0.35, 0.7, 1.0, 1.0)
	sun_light.light_energy = 3.0
	sun_light.shadow_enabled = true
	add_child(sun_light)

	core_light = OmniLight3D.new()
	core_light.name = "PulseCore"
	core_light.position = Vector3(0, 6, 0)
	core_light.light_color = Color(0.8, 0.1, 1.0, 1.0)
	core_light.light_energy = 6.0
	core_light.omni_range = 45.0
	core_light.shadow_enabled = true
	add_child(core_light)

	# Add ambient particle field for hovering holographic motes
	ambient_particles = GPUParticles3D.new()
	var ambient_material = ParticleProcessMaterial.new()
	ambient_material.emission_shape = ParticleProcessMaterial.EMISSION_SHAPE_BOX
	ambient_material.emission_box_extents = Vector3(60, 25, 60)
	ambient_material.color = Color(0.0, 0.85, 1.0, 0.35)
	ambient_material.gravity = Vector3(0, -0.05, 0)
	ambient_material.initial_velocity_min = 0.4
	ambient_material.initial_velocity_max = 1.2
	ambient_material.orbit_velocity = 0.3
	ambient_particles.process_material = ambient_material
	ambient_particles.amount = 120
	ambient_particles.lifetime = 14.0
	ambient_particles.speed_scale = 0.8
	add_child(ambient_particles)



	# Create underground node for event animations
	underground_node = Node3D.new()
	underground_node.name = "Underground"
	add_child(underground_node)

	# Create settings menu
	create_settings_menu()



	# Set up WebSocket connection
	var err = websocket.connect_to_url(WS_URL)
	if err != OK:
		push_error("GODOT: Failed to connect to WebSocket: ", err)
		# Send ready signal once connected
		send_ready_signal()

	# Position camera to see objects
	$Player.position.z = 0
	$Player/Camera.rotation_degrees.x = 0



# Splash screen function
func show_splash_screen():
	var canvas_layer = CanvasLayer.new()
	add_child(canvas_layer)

	# Full screen black panel
	var panel = Panel.new()
	panel.size = get_viewport().size
	var style_box = StyleBoxFlat.new()
	style_box.bg_color = Color.BLACK
	panel.add_theme_stylebox_override("panel", style_box)
	canvas_layer.add_child(panel)

	# Centered label
	var label = Label.new()
	label.text = "MINDPALACE"
	label.add_theme_font_size_override("font_size", 72)
	label.add_theme_color_override("font_color", Color.WHITE)
	label.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	label.vertical_alignment = VERTICAL_ALIGNMENT_CENTER
	label.size = panel.size
	label.position = Vector2(0, 0)
	canvas_layer.add_child(label)

	# Subtitle
	var subtitle = Label.new()
	subtitle.text = "Click to continue"
	subtitle.add_theme_font_size_override("font_size", 24)
	subtitle.add_theme_color_override("font_color", Color.WHITE)
	subtitle.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	subtitle.position = Vector2(panel.size.x / 2 - 100, panel.size.y / 2 + 50)
	canvas_layer.add_child(subtitle)

	# Wait for input
	var input_received = false
	while not input_received:
		await get_tree().process_frame
		if Input.is_action_just_pressed("ui_accept") or Input.is_mouse_button_pressed(MOUSE_BUTTON_LEFT):
			input_received = true

	# Remove splash
	canvas_layer.queue_free()
	splash_finished = true


# Removed create_zone_separators - pure renderer

func _process(delta):
	# Handle WebSocket connection and messages
	websocket.poll()
	var state = websocket.get_ready_state()

	if state == WebSocketPeer.STATE_OPEN:
		if not connected:
			connected = true
			print("GODOT: WebSocket connected")
			send_ready_signal()

		# Process all available messages
		while websocket.get_available_packet_count() > 0:
			var packet = websocket.get_packet()
			var message = packet.get_string_from_utf8()
			print("GODOT: Received WebSocket message: ", message)
			_on_websocket_message(message)

	elif state == WebSocketPeer.STATE_CLOSED:
		if connected:
			connected = false
			print("GODOT: WebSocket disconnected")
			var err = websocket.connect_to_url(WS_URL)
			if err != OK:
				print("GODOT: Failed to reconnect WebSocket: ", err)

	# Update targeting HUD
	update_targeting()

	# Send debug info periodically
	debug_timer += delta
	if debug_timer >= DEBUG_INTERVAL:
		send_debug_info()
		debug_timer = 0.0

var mouse_pressed = false
var last_mouse_pos = Vector2()

# Drag functionality
var dragged_object: Node3D = null
var dragged_node_id = ""
var is_dragging = false
var drag_plane_normal = Vector3(0, 1, 0)  # Drag along XZ plane
var drag_offset = Vector3()

func _input(event):
	# If typing in a text field, ignore game/world shortcuts and clicks
	var f = get_viewport().gui_get_focus_owner()
	if f != null and (f is LineEdit or f is TextEdit):
		return
	if not settings_visible:
		if event is InputEventMouseButton:
			if event.button_index == MOUSE_BUTTON_LEFT:
				if event.pressed:
					# If a model pick is pending, place it at ground click
					if pending_model_id != "":
						var pos = _raycast_ground(event.position)
						if pos != null:
							var payload = {
								"ModelID": pending_model_id,
								"Position": [pos.x, pos.y, pos.z],
							}
							send_ui_command("PlaceEntity", payload)
							pending_model_id = ""
							if placement_hint:
								placement_hint.queue_free()
								placement_hint = null
							close_model_picker()
					# Mouse button pressed - start drag immediately
					start_drag(event.position)
					if not is_dragging:
						handle_click(event.position)
				else:
					# Mouse button released - end drag
					if is_dragging:
						end_drag()
					info_panel.visible = false
			elif event.button_index == MOUSE_BUTTON_WHEEL_UP:
				camera.position.y -= 1
			elif event.button_index == MOUSE_BUTTON_WHEEL_DOWN:
				camera.position.y += 1
		elif event is InputEventMouseMotion:
			if is_dragging:
				# Continue dragging
				update_drag_position(event.position)

	# Handle Tab key for settings menu
	if event is InputEventKey and event.keycode == KEY_TAB and event.pressed:
		toggle_settings_menu()

	# Open model picker with 'M'
	if event is InputEventKey and event.keycode == KEY_M and event.pressed:
		if model_picker_layer == null:
			open_model_picker()
		else:
			close_model_picker()

func _raycast_ground(screen_pos: Vector2):
	var ray_origin = camera.project_ray_origin(screen_pos)
	var ray_dir = camera.project_ray_normal(screen_pos)
	var plane = Plane(Vector3(0,1,0), 0.0)
	var hit = plane.intersects_ray(ray_origin, ray_dir)
	return hit

func open_model_picker():
	_load_model_catalog_if_needed()
	if model_picker_layer != null:
		return
	model_picker_layer = CanvasLayer.new()
	add_child(model_picker_layer)

	# Disable movement and show cursor while picker is open
	if has_node("Player"):
		$Player.movement_disabled = true
	Input.mouse_mode = Input.MOUSE_MODE_VISIBLE

	# Background veil
	var bg = ColorRect.new()
	bg.size = get_viewport().size
	bg.color = Color(0,0,0,0.55)
	model_picker_layer.add_child(bg)
	bg.gui_input.connect(func(e):
		if e is InputEventMouseButton and e.pressed:
			close_model_picker()
	)

	# Centered panel
	model_picker_panel = Panel.new()
	model_picker_panel.size = Vector2(600, 500)
	model_picker_panel.position = (get_viewport().size - model_picker_panel.size) / 2
	_apply_neon_panel(model_picker_panel)
	model_picker_layer.add_child(model_picker_panel)

	var vbox = VBoxContainer.new()
	vbox.anchor_right = 1
	vbox.anchor_bottom = 1
	vbox.size = model_picker_panel.size
	model_picker_panel.add_child(vbox)

	var title = Label.new()
	title.text = "Place Model"
	_apply_neon_label(title, 22)
	vbox.add_child(title)

	model_picker_search = LineEdit.new()
	model_picker_search.placeholder_text = "Search catalog…"
	# Pause movement and reveal cursor when focusing search
	model_picker_search.focus_entered.connect(Callable(self, "_on_text_input_focus_entered"))
	model_picker_search.focus_exited.connect(Callable(self, "_on_text_input_focus_exited"))
	_apply_neon_line_edit(model_picker_search, 18)
	vbox.add_child(model_picker_search)

	var scroll = ScrollContainer.new()
	scroll.size_flags_vertical = Control.SIZE_EXPAND_FILL
	vbox.add_child(scroll)

	model_picker_results = VBoxContainer.new()
	scroll.add_child(model_picker_results)

	model_picker_search.text_changed.connect(func(new_text: String):
		_refresh_model_picker_results(new_text)
	)

	_refresh_model_picker_results("")

func close_model_picker():
	if model_picker_layer:
		model_picker_layer.queue_free()
	model_picker_layer = null
	model_picker_panel = null
	model_picker_search = null
	model_picker_results = null

	# Restore controls if no other text input or overlay has focus
	_on_text_input_focus_exited()

func _load_model_catalog_if_needed():
	if model_catalog_loaded:
		return
	var path = "res://assets/models/catalog.json"
	if not ResourceLoader.exists(path):
		model_catalog_loaded = true
		return
	var file := FileAccess.open(path, FileAccess.READ)
	if file == null:
		model_catalog_loaded = true
		return
	var text := file.get_as_text()
	file.close()
	var json := JSON.new()
	if json.parse(text) == OK:
		var data = json.data
		if typeof(data) == TYPE_DICTIONARY and data.has("entries") and data["entries"] is Array:
			model_catalog_entries = data["entries"]
			model_catalog_loaded = true

func _refresh_model_picker_results(query: String):
	if model_picker_results == null:
		return
	for child in model_picker_results.get_children():
		child.queue_free()
	var q := query.to_lower().strip_edges()
	var shown := 0
	for entry in model_catalog_entries:
		if shown >= 40:
			break
		if typeof(entry) != TYPE_DICTIONARY:
			continue
		var id := str(entry.get("id", ""))
		var title := str(entry.get("title", id))
		var tags_val = entry.get("tags", [])
		var tags_text = ""
		if tags_val is Array:
			for t in tags_val:
				tags_text += str(t) + ", "
		var hay := (id + " " + title + " " + tags_text).to_lower()
		if q == "" or hay.find(q) != -1:
			var btn = Button.new()
			btn.text = title + "  [" + id + "]"
			_apply_neon_button(btn, 16)
			btn.pressed.connect(func():
				pending_model_id = id
				_close_picker_and_show_hint()
			)
			model_picker_results.add_child(btn)
			shown += 1

func _close_picker_and_show_hint():
	close_model_picker()
	if placement_hint:
		placement_hint.queue_free()
	placement_hint = Label.new()
	placement_hint.text = "Click to place model"
	placement_hint.add_theme_font_size_override("font_size", 20)
	placement_hint.add_theme_color_override("font_color", UI_TEXT)
	var layer = CanvasLayer.new()
	add_child(layer)
	layer.add_child(placement_hint)
	placement_hint.position = Vector2(get_viewport().size.x/2 - 100, 30)

func _on_websocket_message(message: String):
	var json = JSON.new()
	var error = json.parse(message)
	if error == OK:
		var data = json.data
		if data.has("type"):
			if data["type"] == "keypresses":
				process_keypresses(data)
			elif data["type"] == "transcription_update":
				process_transcription_update(data)
			elif data["type"] == "setup_zones":
				process_setup_zones(data)
			else:
				process_event_message(data)

func send_ready_signal():
	if websocket.get_ready_state() == WebSocketPeer.STATE_OPEN:
		print("GODOT: Sending ready signal")
		var ready_msg = {
			"type": "ready",
			"timestamp": Time.get_unix_time_from_system(),
			"client_info": {
				"version": "1.0",
				"platform": "godot",
				"last_sequence_id": last_sequence_id
			}
		}
		var json_string = JSON.stringify(ready_msg)
		var err = websocket.send_text(json_string)
		if err != OK:
			print("GODOT: Failed to send ready signal: ", err)
		

func process_event_message(data: Dictionary):
	print("GODOT: process_event_message called with data: " + str(data))
	if not data or typeof(data) != TYPE_DICTIONARY:
		print("GODOT: Invalid data type, returning")
		return
	if not data.has("type"):
		print("GODOT: Missing type in event message")
		return

	var message_type = data["type"]
	if message_type == "signal":
		handle_signal_message(data)
		return

	if message_type != "delta":
		print("GODOT: Unsupported message type: " + str(message_type))
		return

	if data.has("is_full_state") and data["is_full_state"]:
		print("GODOT: Received full state snapshot, resetting scene")
		reset_scene()

	if not data.has("actions") or typeof(data["actions"]) != TYPE_ARRAY:
		print("GODOT: Invalid or missing actions, returning")
		return
	print("GODOT: Received event message with " + str(data["actions"].size()) + " actions")
	# Apply actions to underground for animations
	for action in data["actions"]:
		handle_underground_action(action)
	# Process create actions for above ground objects
	for action in data["actions"]:
		var action_type = action.get("type", "")
		var properties = action.get("properties", {})
		if typeof(properties) == TYPE_DICTIONARY and properties.get("layer", "") == "underground":
			continue
		match action_type:
			"create":
				create_node(action["node_id"], action.get("node_type", "MeshInstance3D"), properties)
			"update":
				update_node(action.get("node_id", ""), properties)
			"delete":
				delete_node(action.get("node_id", ""))

	if data.has("components") and typeof(data["components"]) == TYPE_ARRAY:
		apply_components_from_delta(data["components"])

	# Send ACK if sequence_id is present
	print("GODOT: Checking for sequence_id in data: " + str(data.has("sequence_id")))
	if data.has("sequence_id"):
		last_sequence_id = int(data["sequence_id"])
		send_delta_ack(data["sequence_id"])
		print("GODOT: Sent ACK for sequence " + str(data["sequence_id"]))

func apply_components_from_delta(components: Array):
	for component in components:
		if typeof(component) == TYPE_DICTIONARY:
			register_component(component)

func register_component(component: Dictionary):
	var props: Dictionary = component.get("properties", {})
	var node_id := ""
	if props.has("task_id"):
		node_id = str(props["task_id"])
	elif props.has("node_id"):
		node_id = str(props["node_id"])
	if node_id == "":
		return

	var entry := _ensure_event_entry(node_id)
	entry["component"] = component
	entry["component_actions"] = component.get("actions", {})
	entry["component_properties"] = props

	var node: Node = entry.get("node", null)
	if node:
		node.set_meta("ui_component", component)
	if node is Node3D:
		_update_status_origin(entry)

func handle_signal_message(data: Dictionary):
	var summary = data.get("state_summary", {})
	var handled = false
	if typeof(summary) == TYPE_DICTIONARY:
		var signal_name = summary.get("signal", "")
		if signal_name == "reset_scene":
			print("GODOT: Received reset_scene signal, clearing scene")
			reset_scene()
			handled = true
	if not handled:
		print("GODOT: Received signal message with no handler: " + str(summary))

	if data.has("sequence_id"):
		last_sequence_id = int(data["sequence_id"])
		send_delta_ack(data["sequence_id"])
		print("GODOT: Sent ACK for signal sequence " + str(data["sequence_id"]))

func process_transcription_update(data: Dictionary):
	if not data.has("actions") or typeof(data["actions"]) != TYPE_ARRAY:
		push_error("Invalid transcription_update message: missing or invalid 'actions' field")
		return
	print("GODOT: Processing transcription update with " + str(data["actions"].size()) + " actions")
	for action in data["actions"]:
		handle_underground_action(action)

func _compute_zone_camera_target(zone_name: String, zone_data: Dictionary) -> Dictionary:
	var angle_deg: float = float(zone_data.get("angle", 0.0))
	var radius_source: float = float(zone_data.get("radius", 10.0))
	var radius: float = max(1.0, radius_source)
	var angle_rad: float = deg_to_rad(angle_deg)
	var forward: Vector3 = Vector3(cos(angle_rad), 0.0, sin(angle_rad)).normalized()
	var center: Vector3 = Vector3(radius * forward.x, 0.0, radius * forward.z)

	var default_height: float = max(radius * 0.45, ZONE_CAMERA_MIN_HEIGHT)
	var height: float = float(zone_data.get("camera_height", default_height))
	var default_retreat: float = max(radius * 0.5, ZONE_CAMERA_MIN_RETREAT)
	var retreat: float = float(zone_data.get("camera_retreat", default_retreat))

	var elevated_center := center + Vector3(0.0, height, 0.0)
	var target_position := elevated_center - forward * retreat

	return {
		"position": target_position,
		"look_at": center,
		"meta": {
			"angle": angle_deg,
			"radius": radius,
			"height": height,
			"retreat": retreat,
		},
	}

func update_zone_hud(zones: Dictionary):
	if zone_button_container:
		for child in zone_button_container.get_children():
			child.queue_free()
	zone_camera_targets.clear()

	if zones.size() == 0:
		if zone_panel:
			zone_panel.visible = false
		return

	var zone_names: Array = zones.keys()
	zone_names.sort_custom(func(a, b):
		var key_a := str(a)
		var key_b := str(b)
		var data_a: Dictionary = zones.get(a, zones.get(key_a, {}))
		var data_b: Dictionary = zones.get(b, zones.get(key_b, {}))
		var angle_a := float(data_a.get("angle", 0.0))
		var angle_b := float(data_b.get("angle", 0.0))
		if angle_a == angle_b:
			return key_a < key_b
		return angle_a < angle_b
	)

	for zone_name in zone_names:
		var zone_key := str(zone_name)
		var zone_data: Dictionary = zones.get(zone_name, zones.get(zone_key, {}))
		var target := _compute_zone_camera_target(zone_key, zone_data)
		zone_camera_targets[zone_key] = target

		if zone_button_container:
			var button := Button.new()
			button.text = "[ %s ]" % zone_key.to_upper()
			button.size_flags_horizontal = Control.SIZE_EXPAND_FILL
			button.custom_minimum_size = Vector2(0, 38)
			button.focus_mode = Control.FOCUS_ALL
			button.tooltip_text = "Angle %.1f°, radius %.1f" % [
				float(zone_data.get("angle", 0.0)),
				float(zone_data.get("radius", 0.0)),
			]
			_apply_neon_button(button, 16)
			button.connect("pressed", Callable(self, "_on_zone_button_pressed").bind(zone_key))
			zone_button_container.add_child(button)

	if zone_panel:
		zone_panel.visible = false
		if zone_button_container:
			var button_count := zone_button_container.get_child_count()
			if button_count > 0 and zone_scroll_container:
				var content_height := float(button_count) * 44.0
				zone_panel.size.y = clamp(110.0 + content_height, 160.0, 420.0)
				zone_scroll_container.size = Vector2(zone_panel.size.x - 24, zone_panel.size.y - 70)

	if zone_button_container:
		_on_viewport_size_changed()

func _resolve_zone_name_from_node(node: Node) -> String:
	var current: Node = node
	while current:
		if current.has_meta("zone_name"):
			var value = current.get_meta("zone_name")
			if typeof(value) == TYPE_STRING and value != "":
				return value
		if current.name.begins_with("zone_label_"):
			return current.name.substr("zone_label_".length())
		current = current.get_parent()
	return ""

func _on_zone_button_pressed(zone_name: String):
	if zone_name == "":
		return
	if not zone_camera_targets.has(zone_name):
		return
	fly_camera_to_zone(zone_name)

func fly_camera_to_zone(zone_name: String):
	var target_data = zone_camera_targets.get(zone_name, {})
	if typeof(target_data) != TYPE_DICTIONARY or target_data.is_empty():
		return

	var target_position: Vector3 = target_data.get("position", Vector3.ZERO)
	var look_at_point: Vector3 = target_data.get("look_at", Vector3.ZERO)
	zone_last_focus = look_at_point

	var player_node: CharacterBody3D = $Player
	if player_node == null:
		return

	var camera_node: Camera3D = camera
	if camera_node == null:
		return

	if zone_flight_tween and zone_flight_tween.is_valid():
		zone_flight_tween.kill()

	player_node.movement_disabled = true
	player_node.velocity = Vector3.ZERO

	var direction_to_focus := look_at_point - target_position
	var target_yaw := atan2(direction_to_focus.x, direction_to_focus.z)

	var camera_height_offset := camera_node.position.y
	var camera_final_world := Vector3(
		target_position.x,
		target_position.y + camera_height_offset,
		target_position.z
	)

	var camera_direction := look_at_point - camera_final_world
	var horizontal := Vector2(camera_direction.x, camera_direction.z).length()
	var target_pitch := 0.0
	if horizontal > 0.01:
		target_pitch = -atan2(camera_direction.y, horizontal)
	target_pitch = clamp(target_pitch, -deg_to_rad(80.0), deg_to_rad(10.0))

	var player_target_rotation := player_node.rotation
	player_target_rotation.x = 0.0
	player_target_rotation.z = 0.0
	player_target_rotation.y = target_yaw

	var camera_target_rotation := camera_node.rotation
	camera_target_rotation.x = target_pitch
	camera_target_rotation.y = 0.0
	camera_target_rotation.z = 0.0

	zone_flight_tween = create_tween()
	zone_flight_tween.tween_property(player_node, "global_position", target_position, ZONE_CAMERA_FLIGHT_DURATION).set_trans(Tween.TRANS_QUAD).set_ease(Tween.EASE_IN_OUT)
	zone_flight_tween.parallel().tween_property(player_node, "rotation", player_target_rotation, ZONE_CAMERA_FLIGHT_DURATION).set_trans(Tween.TRANS_QUAD).set_ease(Tween.EASE_IN_OUT)
	zone_flight_tween.parallel().tween_property(camera_node, "rotation", camera_target_rotation, ZONE_CAMERA_FLIGHT_DURATION).set_trans(Tween.TRANS_QUAD).set_ease(Tween.EASE_IN_OUT)
	zone_flight_tween.tween_callback(Callable(self, "_on_zone_flight_finished").bind(zone_name))

func _on_zone_flight_finished(zone_name: String):
	zone_flight_tween = null
	var player_node: CharacterBody3D = $Player
	if player_node:
		player_node.movement_disabled = false
		player_node.velocity = Vector3.ZERO
	var camera_node: Camera3D = camera
	if camera_node and zone_camera_targets.has(zone_name):
		var target_data: Dictionary = zone_camera_targets[zone_name]
		var look_at_point: Vector3 = target_data.get("look_at", camera_node.global_transform.origin)
		var player_position: Vector3
		if player_node:
			player_position = player_node.global_position
		else:
			var stored_position = target_data.get("position", Vector3.ZERO)
			if typeof(stored_position) == TYPE_VECTOR3:
				player_position = stored_position
			else:
				player_position = Vector3.ZERO
		var camera_height_offset := camera_node.position.y
		var camera_world := Vector3(player_position.x, player_position.y + camera_height_offset, player_position.z)
		var camera_direction := look_at_point - camera_world
		var horizontal := Vector2(camera_direction.x, camera_direction.z).length()
		if horizontal > 0.01:
			var target_pitch := -atan2(camera_direction.y, horizontal)
			camera_node.rotation.x = clamp(target_pitch, -deg_to_rad(80.0), deg_to_rad(10.0))
	zone_last_focus = zone_camera_targets.get(zone_name, {}).get("look_at", zone_last_focus)

func process_setup_zones(data: Dictionary):
	if not data.has("payload") or typeof(data["payload"]) != TYPE_STRING:
		push_error("Invalid setup_zones message: missing or invalid 'payload' field")
		return
	var payload_json = JSON.new()
	var parse_result = payload_json.parse(data["payload"])
	if parse_result != OK:
		push_error("Failed to parse setup_zones payload: ", parse_result)
		return
	var payload_data = payload_json.data
	if not payload_data.has("zones") or typeof(payload_data["zones"]) != TYPE_DICTIONARY:
		push_error("Invalid setup_zones payload: missing or invalid 'zones' field")
		return
	var zones = payload_data["zones"]
	print("GODOT: Received setup_zones message with ", zones.size(), " zones")
	for zone_name in zones.keys():
		var zone_data = zones[zone_name]
		print("GODOT: Zone '", zone_name, "' - angle: ", zone_data.get("angle", "N/A"), ", radius: ", zone_data.get("radius", "N/A"))
	# Set zone count for sector calculation
	set_meta("zone_count", zones.size())
	print("GODOT: Calling zone_visualizer.draw_zones()")
	zone_visualizer.draw_zones(zones)
	update_zone_hud(zones)
	print("GODOT: Zone setup complete")

func process_keypresses(data: Dictionary):
	if not data.has("keys") or typeof(data["keys"]) != TYPE_STRING:
		push_error("Invalid keypresses message: missing or invalid 'keys' field")
		return
	var key_string = data["keys"]
	var correlation_id = data.get("correlation_id", "")

	var result = simulate_keypresses(key_string)
	send_state_update()
	if correlation_id != "":
		send_keypress_ack(key_string, correlation_id, result)

func simulate_keypresses(key_string: String) -> Dictionary:
	var result = {
		"success": true,
		"processed_keys": key_string,
		"actions_taken": [],
		"error": ""
	}

	if settings_visible:
		result["actions_taken"].append("menu_keypresses")
		handle_menu_keypresses(key_string)
	else:
		result["actions_taken"].append("player_keypresses")
		handle_player_keypresses(key_string)

	return result

func handle_player_keypresses(key_string: String):
	var key_map = {
		"w": KEY_W,
		"a": KEY_A,
		"s": KEY_S,
		"d": KEY_D,
		"q": KEY_Q,
		"e": KEY_E,
		" ": KEY_SPACE,
		"1": KEY_1,
		"2": KEY_2,
		"3": KEY_3,
		"4": KEY_4,
		"5": KEY_5,
		"6": KEY_6,
		"7": KEY_7,
		"8": KEY_8,
		"9": KEY_9,
		"0": KEY_0,
	}
	for i in range(key_string.length()):
		var char = key_string[i].to_lower()
		if key_map.has(char):
			var key_code = key_map[char]
			var key_event = InputEventKey.new()
			key_event.keycode = key_code
			key_event.pressed = true
			Input.parse_input_event(key_event)
			# Simulate release after a short delay
			await get_tree().create_timer(0.1).timeout
			key_event.pressed = false
			Input.parse_input_event(key_event)

func handle_menu_keypresses(key_string: String):
	for i in range(key_string.length()):
		var char = key_string[i].to_lower()
		if char == "escape":
			# Close menu
			toggle_settings_menu()

func send_position_update_event(node_id: String, position: Vector3):
	if websocket.get_ready_state() != WebSocketPeer.STATE_OPEN:
		return
	var event_msg = {
		"type": "ui_Position3DObject",
		"object_id": node_id,
		"position": [position.x, position.y, position.z]
	}
	var json_string = JSON.stringify(event_msg)
	var err = websocket.send_text(json_string)

func send_state_update():
	if websocket.get_ready_state() != WebSocketPeer.STATE_OPEN:

		return
	var state_msg = {
		"type": "state_update",
		"camera_position": [camera.global_position.x, camera.global_position.y, camera.global_position.z],
		"camera_rotation": [camera.global_rotation.x, camera.global_rotation.y, camera.global_rotation.z],
		"player_position": [$Player.global_position.x, $Player.global_position.y, $Player.global_position.z],
		"settings_visible": settings_visible,
		"timestamp": Time.get_unix_time_from_system(),
	}
	var json_string = JSON.stringify(state_msg)
	var err = websocket.send_text(json_string)

func send_debug_info():
	if websocket.get_ready_state() != WebSocketPeer.STATE_OPEN:
		return
	var debug_msg = {
		"type": "debug_info",
		"object_count": event_cubes.size(),
		"underground_particles": underground_node.get_child_count() if underground_node else 0,
		"timestamp": Time.get_unix_time_from_system(),
	}
	var json_string = JSON.stringify(debug_msg)
	var err = websocket.send_text(json_string)
	if err != OK:
		push_error("Failed to send debug info: ", err)

func send_keypress_ack(keys: String, correlation_id: String, result: Dictionary):
	if websocket.get_ready_state() != WebSocketPeer.STATE_OPEN:
		return

	var ack_msg = {
		"type": "keypress_ack",
		"keys": keys,
		"correlation_id": correlation_id,
		"success": result["success"],
		"processed_keys": result["processed_keys"],
		"actions_taken": result["actions_taken"],
		"timestamp": Time.get_unix_time_from_system()
	}

	if result.has("error") and result["error"] != "":
		ack_msg["error"] = result["error"]

	var json_string = JSON.stringify(ack_msg)
	var err = websocket.send_text(json_string)
	if err != OK:
		push_error("Failed to send keypress ACK: ", err)

func send_delta_ack(sequence_id: int):
	print("GODOT: in send delta ack")
	if websocket.get_ready_state() != WebSocketPeer.STATE_OPEN:
		print("GODOT: websocket not open, returning")
		return

	var ack_msg = {
		"type": "delta_ack",
		"sequence_id": sequence_id
	}

	var json_string = JSON.stringify(ack_msg)
	var err = websocket.send_text(json_string)
	if err != OK:
		push_error("Failed to send delta ACK: ", err)

func handle_click(mouse_pos: Vector2):
	# Handle click (not drag) - show info panel
	var ray_origin = camera.project_ray_origin(mouse_pos)
	var ray_dir = camera.project_ray_normal(mouse_pos)
	var ray_length = 1000.0

	var space_state = get_world_3d().direct_space_state
	var query = PhysicsRayQueryParameters3D.create(ray_origin, ray_origin + ray_dir * ray_length)
	query.collide_with_areas = true
	var result = space_state.intersect_ray(query)

	if result:
		var collider_node: Node = result.collider
		var zone_name := _resolve_zone_name_from_node(collider_node)
		if zone_name != "":
			fly_camera_to_zone(zone_name)
			return

		var clicked_node := collider_node
		while clicked_node:
			var entry_info := _find_registered_entry_for_node(clicked_node)
			if entry_info.size() > 0:
				show_info_panel(clicked_node)
				return
			clicked_node = clicked_node.get_parent()

func start_drag(mouse_pos: Vector2):
	# Try to find object to drag
	var ray_origin = camera.project_ray_origin(mouse_pos)
	var ray_dir = camera.project_ray_normal(mouse_pos)
	var ray_length = 1000.0

	var space_state = get_world_3d().direct_space_state
	var query = PhysicsRayQueryParameters3D.create(ray_origin, ray_origin + ray_dir * ray_length)
	query.collide_with_areas = false
	var result = space_state.intersect_ray(query)

	if result:
		var clicked_node: Node = result.collider
		var entry_info := {}

		while clicked_node:
			entry_info = _find_registered_entry_for_node(clicked_node)
			if entry_info.size() > 0:
				break
			clicked_node = clicked_node.get_parent()

		if entry_info.size() > 0:
			dragged_node_id = entry_info["id"]
			var entry: Dictionary = entry_info["entry"]
			dragged_object = entry.get("node", null)
			if dragged_object == null:
				return
			if entry.get("component_actions", {}).get("change_status", null) == null:
				dragged_object = null
				dragged_node_id = ""
				return
			is_dragging = true
			log_message("Started dragging " + dragged_node_id)

			# Calculate drag offset using a fixed plane at Y=0
			var object_pos = dragged_object.position
			var plane = Plane(drag_plane_normal, 0.0)  # Fixed plane at Y=0
			var intersection = plane.intersects_ray(ray_origin, ray_dir)
			if intersection:
				# Project the intersection point to the object's Y level
				intersection.y = object_pos.y
				drag_offset = object_pos - intersection

			# Visual feedback - make object semi-transparent while dragging
			if dragged_object is MeshInstance3D:
				var material = dragged_object.material_override
				if material and material is StandardMaterial3D:
					material.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
					material.albedo_color.a = 0.7

func update_drag_position(mouse_pos: Vector2):
	if not dragged_object or not is_dragging:
		return

	# Cast ray and find intersection with drag plane
	var ray_origin = camera.project_ray_origin(mouse_pos)
	var ray_dir = camera.project_ray_normal(mouse_pos)

	var plane = Plane(drag_plane_normal, 0.0)  # Fixed plane at Y=0
	var intersection = plane.intersects_ray(ray_origin, ray_dir)

	if intersection:
		# Project to the dragged object's Y level and apply offset
		intersection.y = dragged_object.position.y
		var new_pos = intersection + drag_offset
		new_pos.x = clamp(new_pos.x, -50.0, 50.0)  # Reasonable bounds
		new_pos.y = clamp(new_pos.y, -10.0, 20.0)
		new_pos.z = clamp(new_pos.z, -50.0, 50.0)
		log_message("About to update position of " + dragged_node_id + " to " + str(new_pos))
		dragged_object.position = new_pos

func end_drag():
	var command_sent := false
	if dragged_object and is_dragging:
		if dragged_node_id != "":
			var entry: Dictionary = event_cubes.get(dragged_node_id, null)
			if entry and not entry.get("auxiliary", false):
				var new_status := _determine_status_from_position(entry, dragged_object.position)
				if new_status != "":
					var context := {
						"drop": {
							"zone": new_status,
							"position": [dragged_object.position.x, dragged_object.position.y, dragged_object.position.z],
						},
					}
					command_sent = dispatch_component_action(dragged_node_id, "change_status", context)
					if command_sent:
						entry["status"] = new_status
						if entry.has("status_base_origin"):
							var target_x: float = float(entry["status_base_origin"]) + TASK_STATUS_OFFSETS.get(new_status, 0.0)
							dragged_object.position.x = target_x
						var target_node: Node = entry.get("node", null)
						if target_node:
							var di = target_node.get_meta("display_info", {})
							if di is Dictionary:
								di["status"] = new_status
								target_node.set_meta("display_info", di)
			if not command_sent:
				send_position_update_event(dragged_node_id, dragged_object.position)
				log_message("Placed dragged " + dragged_node_id + " at " + str(dragged_object.position))

	# Restore visual appearance
	if dragged_object is MeshInstance3D:
		var material = dragged_object.material_override
		if material and material is StandardMaterial3D:
			material.transparency = BaseMaterial3D.TRANSPARENCY_DISABLED
			material.albedo_color.a = 1.0

	dragged_object = null
	dragged_node_id = ""
	is_dragging = false

func handle_underground_action(action: Dictionary):
	if typeof(action) != TYPE_DICTIONARY:
		return
	var action_type = action.get("type", "")
	if action_type == "":
		return
	var node_id = action.get("node_id", "")
	if node_id == "":
		return
	var properties = action.get("properties", {})

	if typeof(properties) != TYPE_DICTIONARY:
		return
	if properties.get("layer", "") != "underground":
		return

	var node = underground_node.get_node_or_null(node_id)
	if action_type == "delete":
		if node:
			node.queue_free()
		return

	if action_type != "create":
		return

	if node:
		return

	var mesh_type = action.get("node_type", "MeshInstance3D")
	var new_node
	if mesh_type == "MeshInstance3D":
		new_node = MeshInstance3D.new()
	elif mesh_type == "Label3D":
		new_node = Label3D.new()
		new_node.billboard = BaseMaterial3D.BILLBOARD_ENABLED
		if properties.has("text"):
			new_node.text = str(properties["text"])
		new_node.add_theme_font_size_override("font_size", 32)
		new_node.outline_size = 2
		if properties.has("modulate") and properties["modulate"] is Array:
			var c = properties["modulate"]
			if c.size() >= 3:
				var r = clamp(float(c[0]), 0.0, 1.0)
				var g = clamp(float(c[1]), 0.0, 1.0)
				var b = clamp(float(c[2]), 0.0, 1.0)
				var a = 1.0
				if c.size() > 3:
					a = clamp(float(c[3]), 0.0, 1.0)
				new_node.modulate = Color(r, g, b, a)
	else:
		new_node = Node3D.new()

	if mesh_type == "MeshInstance3D":
		var mesh_name = properties.get("mesh", "box")
		match mesh_name:
			"box":
				var mesh = BoxMesh.new()
				mesh.size = Vector3(0.6, 0.6, 0.6)
				new_node.mesh = mesh
			"sphere":
				var mesh = SphereMesh.new()
				mesh.radius = 0.3
				new_node.mesh = mesh
			_:
				new_node.mesh = BoxMesh.new()
				new_node.mesh.size = Vector3(0.6, 0.6, 0.6)
		if properties.has("material_override"):
			var material = StandardMaterial3D.new()
			var override = properties["material_override"]
			if override is Dictionary and override.has("albedo_color"):
				var color_arr = override["albedo_color"]
				if color_arr is Array and color_arr.size() >= 3:
					var r = float(color_arr[0])
					var g = float(color_arr[1])
					var b = float(color_arr[2])
					var a = 1.0
					if color_arr.size() > 3:
						a = float(color_arr[3])
					material.albedo_color = Color(r, g, b, a)
			new_node.material_override = material
		elif new_node.material_override == null:
			var default_material = StandardMaterial3D.new()
			default_material.albedo_color = Color(0.4, 0.6, 0.9, 1.0)
			new_node.material_override = default_material

	var pos_array = properties.get("position", [])
	if pos_array is Array and pos_array.size() >= 3:
		new_node.position = Vector3(float(pos_array[0]), float(pos_array[1]), float(pos_array[2]))

	var rot_array = properties.get("rotation", [])
	if rot_array is Array and rot_array.size() >= 3:
		new_node.rotation = Vector3(float(rot_array[0]), float(rot_array[1]), float(rot_array[2]))

	var scale_array = properties.get("scale", [])
	if scale_array is Array and scale_array.size() >= 3:
		new_node.scale = Vector3(float(scale_array[0]), float(scale_array[1]), float(scale_array[2]))

	if properties.has("event_type"):
		new_node.set_meta("event_type", properties["event_type"])

	if properties.has("model_path"):
		_apply_model_to_node(new_node, properties["model_path"])

	underground_node.add_child(new_node)
	new_node.name = node_id



func reset_scene():
	print("GODOT: Clearing existing 3D nodes")
	var keys = event_cubes.keys()
	for node_id in keys:
		var entry = event_cubes.get(node_id, {})
		var node = entry.get("node", null)
		if node and node.is_inside_tree():
			node.queue_free()
	event_cubes.clear()

	if underground_node:
		for child in underground_node.get_children():
			child.queue_free()

	targeted_object = null
	clear_targeting_hud()
	if typeof(targeting_reticle) != TYPE_NIL:
		targeting_reticle.color = Color.WHITE
	is_dragging = false
	dragged_object = null
	dragged_node_id = ""


func update_above_ground(state_summary: Dictionary):
	# state_summary is a dict like {"tasks": [array of task dicts]}
	print("GODOT: Updating above ground with keys: " + str(state_summary.keys()))
	var existing_ids = event_cubes.keys().duplicate()

	# Handle tasks
	if state_summary.has("tasks"):
		for task in state_summary["tasks"]:
			var node_id = task["id"]
			var props = task
			props["node_type"] = "MeshInstance3D"  # Ensure node_type
			if event_cubes.has(node_id):
				# Update existing
				update_node(node_id, props)
				existing_ids.erase(node_id)
			else:
				# Create new
				create_node(node_id, "MeshInstance3D", props)

	# Handle notes
	if state_summary.has("notes"):
		for note in state_summary["notes"]:
			var node_id = note["id"]
			var props = note
			props["node_type"] = "Label3D"
			if event_cubes.has(node_id):
				update_node(node_id, props)
				existing_ids.erase(node_id)
			else:
				create_node(node_id, "Label3D", props)

	# Handle calendar events
	if state_summary.has("events"):
		for event in state_summary["events"]:
			var node_id = event["id"]
			var props = event
			props["node_type"] = "MeshInstance3D"
			if event_cubes.has(node_id):
				update_node(node_id, props)
				existing_ids.erase(node_id)
			else:
				create_node(node_id, "MeshInstance3D", props)

	# Delete nodes not in state_summary
	for node_id in existing_ids:
		delete_node(node_id)

# Removed plugin type and grid position logic - pure renderer

func create_node(node_id: String, node_type: String, properties: Dictionary):
	print("GODOT: Creating node " + node_id + " type " + node_type + " pos " + str(properties.get("position", "no pos")))
	if node_id == "orchestrator_ai":
		print("GODOT: Creating orchestrator AI node")
	log_message("About to create node " + node_id)
	if event_cubes.has(node_id):
		return
	var node
	if node_type == "MeshInstance3D":
		node = MeshInstance3D.new()
	elif node_type == "CharacterBody3D":
		node = CharacterBody3D.new()
	elif node_type == "Label3D":
		node = Label3D.new()
		node.billboard = BaseMaterial3D.BILLBOARD_ENABLED
	else:
		return
	if node_type == "MeshInstance3D":
		if properties.has("mesh"):
			if properties["mesh"] == "box":
				node.mesh = BoxMesh.new()
				node.mesh.size = Vector3(1, 1, 1)
			elif properties["mesh"] == "sphere":
				node.mesh = SphereMesh.new()
				node.mesh.radius = 0.3
				node.mesh.height = 0.6
			elif properties["mesh"] == "cylinder":
				node.mesh = CylinderMesh.new()
				node.mesh.top_radius = 0.3
				node.mesh.bottom_radius = 0.3
				node.mesh.height = 1.0
			elif properties["mesh"] == "plane":
				node.mesh = PlaneMesh.new()
				node.mesh.size = Vector2(2, 2)
			elif properties["mesh"] == "capsule":
				node.mesh = CapsuleMesh.new()
				node.mesh.radius = 0.3
				node.mesh.height = 1.0
		else:
			return
	  
		# Position comes from server properties


	# Set default mesh if not set
	if node is MeshInstance3D and not node.mesh:
			node.mesh = BoxMesh.new()
			node.mesh.size = Vector3(1, 1, 1)

	if properties.has("model_path"):
		_apply_model_to_node(node, properties["model_path"])

	# If backend provides a specific position, use Y only and add to computed XZ (for height variations)
	log_message("About to place node " + node_id + " at position from properties: " + str(properties.get("position", "no pos")))
	if properties.has("position") and properties["position"] is Array and properties["position"].size() >= 3:
		var backend_pos = properties["position"]
		node.position.x = clamp(float(backend_pos[0]), -1000.0, 1000.0)
		node.position.y = clamp(float(backend_pos[1]), -1000.0, 1000.0)
		node.position.z = clamp(float(backend_pos[2]), -1000.0, 1000.0)
		print("GODOT: Set position for ", node_id, " to ", node.position)
		# Make nodes face the center like zone labels
		if node is MeshInstance3D:
			print("GODOT: Node position: ", node.position, ", target: (0,0,0)")
			var dir = (Vector3(0, 0, 0) - node.position).normalized()
			var up = Vector3.UP
			var right = dir.cross(up).normalized()
			up = right.cross(dir).normalized()
			# For plane mesh, normal is +Y, so set basis.y = dir (normal towards center)
			# basis.x = right, basis.z = up
			node.transform.basis = Basis(right, dir, up)
			print("GODOT: Set basis - right: ", right, ", dir (normal): ", dir, ", up: ", up)
			print("GODOT: Final basis.z: ", node.transform.basis.z, ", basis.y: ", node.transform.basis.y)
	if properties.has("scale"):
		var scl = properties["scale"]
		if scl is Array and scl.size() >= 3:
			var sx = clamp(float(scl[0]), 0.1, 10.0)
			var sy = clamp(float(scl[1]), 0.1, 10.0)
			var sz = clamp(float(scl[2]), 0.1, 10.0)
			node.scale = Vector3(sx, sy, sz)

	if properties.has("rotation"):
		var rot = properties["rotation"]
		if rot is Array and rot.size() >= 3:
			var rx = float(rot[0])
			var ry = float(rot[1])
			var rz = float(rot[2])
			node.rotation = Vector3(rx, ry, rz)
	
	if properties.has("text") and node is Label3D:
		node.text = properties["text"]
	var font_size := 64
	if properties.has("font_size"):
		font_size = int(properties["font_size"])
	if node is Label3D:
		node.add_theme_font_size_override("font_size", font_size)
		if properties.has("outline_size"):
			node.outline_size = int(properties["outline_size"])
		else:
			node.outline_size = 6
		if properties.has("outline_modulate"):
			node.outline_modulate = _array_to_color(properties["outline_modulate"], LABEL_OUTLINE_COLOR)
		else:
			node.outline_modulate = LABEL_OUTLINE_COLOR
		if properties.has("modulate"):
			node.modulate = _array_to_color(properties["modulate"], UI_TEXT)
		elif properties.has("event_type") and EVENT_COLORS.has(properties["event_type"]):
			node.modulate = EVENT_COLORS[properties["event_type"]]
		else:
			node.modulate = UI_TEXT
		if properties.has("horizontal_alignment"):
			var align = str(properties["horizontal_alignment"]).to_lower()
			match align:
				"center":
					node.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
				"right":
					node.horizontal_alignment = HORIZONTAL_ALIGNMENT_RIGHT
				"left":
					node.horizontal_alignment = HORIZONTAL_ALIGNMENT_LEFT
		if properties.has("billboard"):
			var mode = str(properties["billboard"]).to_lower()
			match mode:
				"enabled", "true":
					node.billboard = 1
				"fixed_y", "y":
					node.billboard = 2
				_:
					node.billboard = 0
	
	# Add neon material styling for MeshInstance3D nodes
	if node is MeshInstance3D:
		var base_color = Color(0.0, 0.78, 1.0, 1.0)
		var emission_color = base_color
		if properties.has("event_type") and EVENT_COLORS.has(properties["event_type"]):
			base_color = EVENT_COLORS[properties["event_type"]]
		if properties.has("color"):
			base_color = _array_to_color(properties["color"], base_color)
		if properties.has("material_override"):
			var mo = properties["material_override"]
			if mo is Dictionary and mo.has("albedo_color"):
				base_color = _array_to_color(mo["albedo_color"], base_color)
		if properties.has("emissive_color"):
			emission_color = _array_to_color(properties["emissive_color"], emission_color)

		var neon_material = _build_neon_material(base_color, base_color.a)
		neon_material.emission = emission_color
		node.material_override = neon_material
		if node_id == "orchestrator_ai":
			_augment_orchestrator(node)

	# Add particles if requested
	if properties.has("particles") and properties["particles"] == true:
		var particles = GPUParticles3D.new()
		var particle_process_material = ParticleProcessMaterial.new()
		particle_process_material.emission_shape = ParticleProcessMaterial.EMISSION_SHAPE_POINT
		particle_process_material.direction = Vector3(0, 1, 0)	# Upward
		particle_process_material.spread = 180.0  # Full spread
		particle_process_material.gravity = Vector3(0, -1, 0)  # Slight downward gravity for smoke
		particle_process_material.initial_velocity_min = 1.0
		particle_process_material.initial_velocity_max = 3.0
		particle_process_material.color = Color(1.0, 0.8, 0.4, 0.6)	 # Warm yellow smoke
		particle_process_material.scale_min = 0.5
		particle_process_material.scale_max = 1.5
		particles.process_material = particle_process_material
		particles.amount = 100
		particles.lifetime = 4.0
		particles.speed_scale = 1.0
		particles.one_shot = false
		node.add_child(particles)
	
	# Add collision for interaction
	if node_type == "MeshInstance3D":
		var body = StaticBody3D.new()
		var shape = CollisionShape3D.new()
		var box_shape = BoxShape3D.new()
		box_shape.size = Vector3(1, 1, 1)
		shape.shape = box_shape
		body.add_child(shape)
		node.add_child(body)

	add_child(node)
	var entry := _ensure_event_entry(node_id)
	entry["node"] = node
	if _is_auxiliary_node_id(node_id):
		entry["auxiliary"] = true
	if properties.has("auxiliary") and properties["auxiliary"]:
		entry["auxiliary"] = true
	event_cubes[node_id] = entry
	node.set_meta("display_info", properties.get("display_info", {}))
	if entry.has("component"):
		node.set_meta("ui_component", entry["component"])
	if node is Node3D and not entry.get("auxiliary", false):
		_update_status_origin(entry)

	# Handle parenting if specified
	if properties.has("parent_id"):
		var parent_id = properties["parent_id"]
		var parent_node = event_cubes.get(parent_id, {}).get("node", null)
		if parent_node:
			# Remove from root and add as child of parent
			remove_child(node)
			parent_node.add_child(node)
			# Set local position relative to parent
			if node_type == "Label3D":
				var offset_y = 1.2	# Default for box
				if properties.has("mesh_type"):
					var mesh_type = properties["mesh_type"]
					if mesh_type == "sphere":
						offset_y = 0.8
					elif mesh_type == "cylinder":
						offset_y = 1.5
					elif mesh_type == "plane":
						offset_y = 0.1
					elif mesh_type == "capsule":
						offset_y = 1.0
				log_message("About to parent and place label " + node_id + " to " + parent_id + " at local pos " + str(Vector3(0, offset_y, 0)))
				node.position = Vector3(0, offset_y, 0)
				log_message("Placed parented label " + node_id + " to " + parent_id + " at local pos " + str(node.position))
		else:
			push_warning("Parent node " + parent_id + " not found for " + node_id)
	else:
		log_message("Placed node " + node_id + " at " + str(node.position))

func update_node(node_id: String, properties: Dictionary):
	var node = event_cubes.get(node_id, {}).get("node", null)

	if node:
		if properties.has("display_info") and properties["display_info"] is Dictionary:
			node.set_meta("display_info", properties["display_info"])
			_refresh_conversation_panel_if_matches(properties["display_info"])
			if targeted_object == node:
				var event_id = get_event_id_from_object(node)
				if event_id != "":
					update_hud_for_object(event_id, node)
		if properties.has("model_path"):
			_apply_model_to_node(node, properties["model_path"])
		if node is Label3D:
			if properties.has("text"):
				node.text = properties["text"]
			if properties.has("event_type") and EVENT_COLORS.has(properties["event_type"]):
				node.modulate = EVENT_COLORS[properties["event_type"]]
			node.outline_size = 6
			node.outline_modulate = LABEL_OUTLINE_COLOR
			return	# Skip position/scale/material for labels
		# Skip position updates for now
		# if properties.has("position"):
		#	  var pos = properties["position"]
		#	  if pos is Array and pos.size() >= 3:
		#		  var x = float(pos[0])
		#		  var y = float(pos[1])
		#		  var z = float(pos[2])
		#		  if x != 0 or y != 0 or z != 0:
		#			  var clamped_x = clamp(x, -1000.0, 1000.0)
		#			  var clamped_y = clamp(y, -1000.0, 1000.0)
		#			  var clamped_z = clamp(z, -1000.0, 1000.0)
		#			  node.position = Vector3(clamped_x, clamped_y, clamped_z)
		if properties.has("scale"):
			var scl = properties["scale"]
			if scl is Array and scl.size() >= 3:
				var sx = clamp(float(scl[0]), 0.1, 10.0)
				var sy = clamp(float(scl[1]), 0.1, 10.0)
				var sz = clamp(float(scl[2]), 0.1, 10.0)
				node.scale = Vector3(sx, sy, sz)
		if properties.has("rotation"):
			var rot = properties["rotation"]
			if rot is Array and rot.size() >= 3:
				var rx = float(rot[0])
				var ry = float(rot[1])
				var rz = float(rot[2])
				node.rotation = Vector3(rx, ry, rz)
		if node is MeshInstance3D:
			var base_color = Color(0.0, 0.78, 1.0, 1.0)
			if node.material_override is StandardMaterial3D:
				var existing_material: StandardMaterial3D = node.material_override
				base_color = existing_material.emission if existing_material.emission_enabled else existing_material.albedo_color
			var emission_color = base_color
			var changed = false

			if properties.has("event_type") and EVENT_COLORS.has(properties["event_type"]):
				base_color = EVENT_COLORS[properties["event_type"]]
				changed = true
			if properties.has("color"):
				base_color = _array_to_color(properties["color"], base_color)
				changed = true
			if properties.has("material_override"):
				var mo = properties["material_override"]
				if mo is Dictionary and mo.has("albedo_color"):
					base_color = _array_to_color(mo["albedo_color"], base_color)
					changed = true
			if properties.has("emissive_color"):
				emission_color = _array_to_color(properties["emissive_color"], emission_color)
				changed = true

			if changed:
				var neon_material = _build_neon_material(base_color, base_color.a)
				neon_material.emission = emission_color
				node.material_override = neon_material
			if node_id == "orchestrator_ai":
				_augment_orchestrator(node)


func delete_node(node_id: String):
	var node = event_cubes.get(node_id, {}).get("node", null)
	if node:
		# Also remove any child nodes from event_cubes
		for child in node.get_children():
			for id in event_cubes:
				if event_cubes[id].get("node", null) == child:
					event_cubes.erase(id)
					break
		node.queue_free()
		event_cubes.erase(node_id)

func clear_all_objects():
	for node_id in event_cubes.keys():
		var node = event_cubes[node_id].get("node", null)
		if node:
			node.queue_free()
	event_cubes.clear()

func show_info_panel(node: Node):
	info_panel.visible = true
	var entry_info := _find_registered_entry_for_node(node)
	var node_id := ""
	var entry: Dictionary = {}
	if entry_info.has("id"):
		node_id = entry_info["id"]
		entry = entry_info["entry"]
	else:
		for id in event_cubes:
			if event_cubes[id].get("node", null) == node:
				node_id = id
				entry = event_cubes[id]
				break

	var display_info = node.get_meta("display_info", {})
	var text = "[ ID ] " + node_id + "\n[ TYPE ] " + node.get_class() + "\n[ POS ] " + str(node.position)
	var component_props: Dictionary = entry.get("component_properties", {})
	if component_props.has("status"):
		text += "\n[ STATUS ] " + str(component_props["status"])
	if component_props.has("priority"):
		text += "\n[ PRIORITY ] " + str(component_props["priority"])
	var title_text := ""
	var desc_text := ""
	if component_props.has("title"):
		title_text = str(component_props["title"])
	elif display_info is Dictionary and display_info.has("title"):
		title_text = str(display_info["title"])
	if component_props.has("description"):
		desc_text = str(component_props["description"])
	elif display_info is Dictionary and display_info.has("description"):
		desc_text = str(display_info["description"])
	if title_text != "":
		text += "\n\n[ TITLE ] " + title_text
	if desc_text != "":
		text += "\n[ DESC ] " + desc_text
	info_label.text = text
	_populate_info_actions(node_id, entry)
	open_conversation_panel(display_info)

func _populate_info_actions(node_id: String, entry: Dictionary):
	if info_actions_container == null:
		return
	for child in info_actions_container.get_children():
		child.queue_free()
	if node_id == "":
		info_actions_container.visible = false
		return
	var component = entry.get("component", null)
	if component == null:
		info_actions_container.visible = false
		return
	var actions = component.get("actions", {})
	if typeof(actions) != TYPE_DICTIONARY:
		info_actions_container.visible = false
		return
	var component_props: Dictionary = entry.get("component_properties", {})

	if actions.has("rename"):
		var rename_box = HBoxContainer.new()
		rename_box.size_flags_horizontal = Control.SIZE_EXPAND_FILL
		rename_box.spacing = 6
		var rename_field = LineEdit.new()
		rename_field.size_flags_horizontal = Control.SIZE_EXPAND_FILL
		rename_field.text = str(component_props.get("title", ""))
		# Pause movement while editing titles
		rename_field.focus_entered.connect(Callable(self, "_on_text_input_focus_entered"))
		rename_field.focus_exited.connect(Callable(self, "_on_text_input_focus_exited"))
		_apply_neon_line_edit(rename_field, 16)
		rename_box.add_child(rename_field)
		var rename_button = Button.new()
		rename_button.text = "Rename"
		_apply_neon_button(rename_button, 14)
		var target_node = entry.get("node", null)
		rename_button.pressed.connect(func():
			if dispatch_component_action(node_id, "rename", {"user_input": {"text": rename_field.text}}) and target_node:
				var di = target_node.get_meta("display_info", {})
				if di is Dictionary:
					di["title"] = rename_field.text
					target_node.set_meta("display_info", di)
				show_info_panel(target_node)
		)
		rename_field.text_submitted.connect(func(new_text):
			if dispatch_component_action(node_id, "rename", {"user_input": {"text": new_text}}) and target_node:
				var di = target_node.get_meta("display_info", {})
				if di is Dictionary:
					di["title"] = new_text
					target_node.set_meta("display_info", di)
				show_info_panel(target_node)
		)
		rename_box.add_child(rename_button)
		info_actions_container.add_child(rename_box)

	if actions.has("edit_description"):
		var desc_field = TextEdit.new()
		desc_field.size_flags_horizontal = Control.SIZE_EXPAND_FILL
		desc_field.custom_minimum_size = Vector2(0, 80)
		desc_field.text = str(component_props.get("description", ""))
		desc_field.add_theme_color_override("font_color", UI_TEXT)
		desc_field.add_theme_color_override("selection_color", UI_ACCENT)
		desc_field.add_theme_color_override("caret_color", UI_TEXT)
		# Pause movement while editing descriptions
		desc_field.focus_entered.connect(Callable(self, "_on_text_input_focus_entered"))
		desc_field.focus_exited.connect(Callable(self, "_on_text_input_focus_exited"))
		info_actions_container.add_child(desc_field)
		var desc_button = Button.new()
		desc_button.text = "Update Description"
		_apply_neon_button(desc_button, 14)
		desc_button.pressed.connect(func():
			var target_desc_node: Node = entry.get("node", null)
			if dispatch_component_action(node_id, "edit_description", {"user_input": {"text": desc_field.text}}) and target_desc_node:
				var di = target_desc_node.get_meta("display_info", {})
				if di is Dictionary:
					di["description"] = desc_field.text
					target_desc_node.set_meta("display_info", di)
				show_info_panel(target_desc_node)
		)
		info_actions_container.add_child(desc_button)

	if actions.has("complete"):
		var complete_btn = Button.new()
		complete_btn.text = "Mark Complete"
		_apply_neon_button(complete_btn, 16)
		complete_btn.pressed.connect(func():
			var complete_node: Node = entry.get("node", null)
			if dispatch_component_action(node_id, "complete") and complete_node:
				var di = complete_node.get_meta("display_info", {})
				if di is Dictionary:
					di["status"] = "Completed"
					complete_node.set_meta("display_info", di)
				show_info_panel(complete_node)
		)
		info_actions_container.add_child(complete_btn)

	if actions.has("delete"):
		var delete_btn = Button.new()
		delete_btn.text = "Delete Task"
		_apply_neon_button(delete_btn, 16)
		delete_btn.add_theme_color_override("font_color", Color(1.0, 0.4, 0.5, 1.0))
		delete_btn.pressed.connect(func():
			if dispatch_component_action(node_id, "delete"):
				info_panel.visible = false
		)
		info_actions_container.add_child(delete_btn)

	info_actions_container.visible = info_actions_container.get_child_count() > 0

func open_conversation_panel(display_info: Dictionary):
	if display_info is Dictionary:
		var details = display_info.get("details", {})
		if details is Dictionary and details.has("conversation"):
			populate_conversation_panel(display_info)
			conversation_panel.visible = true
			return
	conversation_panel.visible = false
	current_conversation_agent = ""

func populate_conversation_panel(display_info: Dictionary):
	if conversation_panel == null:
		return
	var details = display_info.get("details", {}) if display_info is Dictionary else {}
	conversation_title_label.text = str(display_info.get("title", "Conversation"))
	var description = str(display_info.get("description", ""))
	var conversation = []
	if details is Dictionary:
		conversation = details.get("conversation", [])
		current_conversation_agent = str(details.get("conversation_agent", ""))
	else:
		current_conversation_agent = ""

	conversation_history.clear()
	if description != "":
		conversation_history.append_text(description + "\n\n")
	if conversation is Array and conversation.size() > 0:
		for entry in conversation:
			if entry is Dictionary:
				var role = str(entry.get("role", ""))
				var content = str(entry.get("content", "")).replace("\n", " ").strip_edges()
				var timestamp = str(entry.get("timestamp", ""))
				if timestamp.length() >= 19:
					timestamp = timestamp.substr(11, 8)
				else:
					timestamp = ""
				var line = ""
				if timestamp != "":
					line += "[" + timestamp + "] "
				line += role + ": " + content
				conversation_history.append_text(line + "\n")
	else:
		conversation_history.append_text("No conversation yet.\n")

	conversation_history.scroll_to_line(conversation_history.get_line_count())
	var placeholder = "Talk to the orchestrator..."
	if current_conversation_agent != "":
		placeholder = "Talk to " + current_conversation_agent.capitalize() + "..."
	conversation_input.placeholder_text = placeholder
	conversation_input.text = ""
	conversation_input.grab_focus()

func _refresh_conversation_panel_if_matches(display_info: Dictionary):
	if conversation_panel == null or !conversation_panel.visible:
		return
	if display_info is Dictionary:
		var details = display_info.get("details", {})
		if details is Dictionary:
			var agent = str(details.get("conversation_agent", ""))
			if agent == current_conversation_agent:
				populate_conversation_panel(display_info)


# Targeting HUD functions
func update_targeting():
	# Raycast from center of screen (camera forward)
	var viewport_size = get_viewport().size
	var center_pos = viewport_size / 2.0

	var ray_origin = camera.project_ray_origin(center_pos)
	var ray_dir = camera.project_ray_normal(center_pos)
	var ray_length = 1000.0

	var space_state = get_world_3d().direct_space_state
	var query = PhysicsRayQueryParameters3D.create(ray_origin, ray_origin + ray_dir * ray_length)
	query.collide_with_areas = false
	var result = space_state.intersect_ray(query)

	if result:
		var hit_object = result.collider
		# Check if it's one of our event objects
		var event_id = get_event_id_from_object(hit_object)
		if event_id:
			if targeted_object != hit_object:
				# Clear previous glow
				if targeted_object and targeted_object is MeshInstance3D and targeted_object.material_override:
					var mat = targeted_object.material_override
					if mat is StandardMaterial3D:
						mat.emission_energy = 0.0
			targeted_object = hit_object
			update_hud_for_object(event_id, hit_object)
			targeting_reticle.color = Color.GREEN
			# Add glow
			if hit_object is MeshInstance3D and hit_object.material_override:
				var mat = hit_object.material_override
				if mat is StandardMaterial3D:
					mat.emission_energy = 0.5
		else:
			# Looking at non-event object
			if targeted_object != null:
				# Clear glow
				if targeted_object is MeshInstance3D and targeted_object.material_override:
					var mat = targeted_object.material_override
					if mat is StandardMaterial3D:
						mat.emission_energy = 0.0
				targeted_object = null
				clear_targeting_hud()
				targeting_reticle.color = Color.YELLOW
	else:
		# Looking at nothing
		if targeted_object != null:
			# Clear glow
			if targeted_object is MeshInstance3D and targeted_object.material_override:
				var mat = targeted_object.material_override
				if mat is StandardMaterial3D:
					mat.emission_energy = 0.0
			targeted_object = null
			clear_targeting_hud()
			targeting_reticle.color = Color.WHITE


func get_event_id_from_object(obj: Node) -> String:
	# Walk up the hierarchy to find event objects
	var current = obj
	while current:
		for event_id in event_cubes:
			if event_cubes[event_id].get("node", null) == current:
				return event_id
		current = current.get_parent()
	return ""


func update_hud_for_object(event_id: String, obj: Node):
	if not targeting_hud_panel:
		return

	var details = get_object_details(event_id, obj)
	targeting_hud_label.text = details
	targeting_hud_panel.visible = true


func clear_targeting_hud():
	if targeting_hud_panel:
		targeting_hud_panel.visible = false


func get_object_details(event_id: String, obj: Node) -> String:
	var details = "[ TARGET LOCKED ]\n\n"
	details += "Event ID: %s\n" % event_id
	details += "Type: %s\n" % obj.get_class()
	details += "Position: %.1f, %.1f, %.1f\n" % [obj.position.x, obj.position.y, obj.position.z]

	# Calculate distance from camera
	var distance = camera.global_position.distance_to(obj.global_position)
	details += "Distance: %.1f units\n\n" % distance

	# Get display_info
	var node = event_cubes.get(event_id, {}).get("node", null)
	var display_info = node.get_meta("display_info", {}) if node else {}
	if display_info.has("title"):
		details += "[b]Title:[/b] %s\n" % display_info["title"]
	if display_info.has("description"):
		details += "[i]Description:[/i] %s\n\n" % display_info["description"]
	if display_info.has("details"):
		var det = display_info["details"]
		details += "[b]Event Telemetry:[/b]\n"
		for key in det.keys():
			if key in ["conversation", "conversation_agent", "conversation_summary", "conversation_last_updated"]:
				continue
			details += "- %s: %s\n" % [key.capitalize(), str(det[key])]
		details += "\n"
		if det.has("conversation_summary"):
			details += "Conversation:\n%s\n" % str(det["conversation_summary"])
			details += "Interact to open dialogue panel.\n"

	# Add visual properties
	if obj is MeshInstance3D and obj.material_override:
		var mat = obj.material_override
		if mat is StandardMaterial3D:
			details += "\nColor: %s" % str(mat.albedo_color).substr(0, 20)

	details += "\n\n[click to interact]"

	return details


func _on_viewport_size_changed():
	# Update HUD panel position for new viewport size
	if targeting_hud_panel:
		targeting_hud_panel.position = Vector2(get_viewport().size.x - targeting_hud_panel.size.x - 20, 20)
	if game_log_panel:
		game_log_panel.position = Vector2(20, get_viewport().size.y - game_log_panel.size.y - 20)
	if zone_panel:
		var max_height = max(160.0, get_viewport().size.y - 40.0)
		zone_panel.size.y = min(max_height, 420.0)
		zone_panel.position = Vector2(get_viewport().size.x - zone_panel.size.x - 20, 20)
		if zone_scroll_container:
			zone_scroll_container.size = Vector2(zone_panel.size.x - 24, zone_panel.size.y - 70)
	if settings_panel:
		settings_panel.size = get_viewport().size
		# Update header
		var header = settings_panel.get_child(0)
		if header:
			header.size.x = settings_panel.size.x - 40
		# Update tab container
		var tab_container = settings_panel.get_child(1)
		if tab_container:
			tab_container.size = Vector2(settings_panel.size.x - 40, settings_panel.size.y - 100)
	if conversation_panel:
		conversation_panel.position = Vector2(get_viewport().size.x - conversation_panel.size.x - 20, get_viewport().size.y - conversation_panel.size.y - 20)
	update_reticle_position()


func update_reticle_position():
	if targeting_reticle:
		targeting_reticle.position = get_viewport().size / 2.0 - Vector2(3, 3)


# Build the zone flyover HUD
func create_zone_hud():
	var canvas_layer = CanvasLayer.new()
	add_child(canvas_layer)

	zone_panel = Panel.new()
	zone_panel.name = "ZonePanel"
	zone_panel.size = Vector2(220, 260)
	zone_panel.position = Vector2(get_viewport().size.x - zone_panel.size.x - 20, 20)
	zone_panel.visible = false
	_apply_neon_panel(zone_panel, UI_PANEL_BORDER, UI_BG, 2, 10)
	canvas_layer.add_child(zone_panel)

	var title_label = Label.new()
	title_label.text = ":: zone flyovers"
	title_label.position = Vector2(12, 12)
	title_label.size = Vector2(zone_panel.size.x - 24, 28)
	_apply_neon_label(title_label, 18)
	zone_panel.add_child(title_label)

	var subtitle = Label.new()
	subtitle.text = "click to drift above"
	subtitle.position = Vector2(12, 36)
	subtitle.size = Vector2(zone_panel.size.x - 24, 18)
	_apply_neon_label(subtitle, 12, true)
	zone_panel.add_child(subtitle)

	zone_scroll_container = ScrollContainer.new()
	zone_scroll_container.name = "ZoneScroll"
	zone_scroll_container.position = Vector2(12, 58)
	zone_scroll_container.size = Vector2(zone_panel.size.x - 24, zone_panel.size.y - 70)
	zone_scroll_container.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	zone_scroll_container.size_flags_vertical = Control.SIZE_EXPAND_FILL
	zone_panel.add_child(zone_scroll_container)

	zone_button_container = VBoxContainer.new()
	zone_button_container.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	zone_button_container.add_theme_constant_override("separation", 10)
	zone_scroll_container.add_child(zone_button_container)


# Settings menu functions
func create_game_log():
	var canvas_layer = CanvasLayer.new()
	add_child(canvas_layer)

	game_log_panel = Panel.new()
	game_log_panel.size = Vector2(400, 200)
	game_log_panel.position = Vector2(20, get_viewport().size.y - 220)
	_apply_neon_panel(game_log_panel, UI_ACCENT, UI_BG, 2)
	canvas_layer.add_child(game_log_panel)

	game_log_label = Label.new()
	game_log_label.position = Vector2(10, 10)
	game_log_label.size = Vector2(380, 180)
	game_log_label.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	_apply_neon_label(game_log_label, 14, true)
	game_log_panel.add_child(game_log_label)

	# Initial log
	log_message("Game log initialized")

func log_message(msg: String):
	print("GAME_LOG: " + msg)
	game_log_text += Time.get_datetime_string_from_system() + ": " + msg + "\n"
	# Keep only last 10 lines
	var lines = game_log_text.split("\n")
	if lines.size() > 11:
		lines = lines.slice(-11)
		game_log_text = "\n".join(lines)
	game_log_label.text = game_log_text

func create_settings_menu():
	var canvas_layer = CanvasLayer.new()
	add_child(canvas_layer)

	settings_panel = Panel.new()
	settings_panel.size = get_viewport().size
	settings_panel.position = Vector2.ZERO
	settings_panel.visible = false
	_apply_neon_panel(settings_panel, UI_PANEL_BORDER, Color(0, 0, 0, 0.92), 3, 0)
	canvas_layer.add_child(settings_panel)

	var header_hbox = HBoxContainer.new()
	header_hbox.size = Vector2(settings_panel.size.x - 40, 60)
	header_hbox.position = Vector2(20, 20)
	header_hbox.add_theme_constant_override("separation", 20)
	settings_panel.add_child(header_hbox)

	var title_label = Label.new()
	title_label.text = "[ MINDPALACE // CONTROL MATRIX ]"
	_apply_neon_label(title_label, 26)
	title_label.vertical_alignment = VERTICAL_ALIGNMENT_CENTER
	title_label.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	header_hbox.add_child(title_label)

	var close_button = Button.new()
	close_button.text = "EXIT (Tab)"
	close_button.size = Vector2(150, 40)
	close_button.tooltip_text = "Close the control matrix overlay."
	close_button.connect("pressed", Callable(self, "_on_close_settings"))
	_apply_neon_button(close_button, 16)
	header_hbox.add_child(close_button)

	var tab_container = TabContainer.new()
	tab_container.size = Vector2(settings_panel.size.x - 40, settings_panel.size.y - 100)
	tab_container.position = Vector2(20, 80)
	_apply_neon_panel(tab_container, UI_PANEL_BORDER, UI_BG_DEEP, 2, 10)
	tab_container.add_theme_color_override("font_color", UI_TEXT)
	tab_container.add_theme_color_override("font_unselected_color", UI_MUTED_TEXT)
	tab_container.add_theme_color_override("font_hovered_color", UI_ACCENT)
	settings_panel.add_child(tab_container)

	var env_tab = ScrollContainer.new()
	env_tab.name = "Environment"
	env_tab.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	env_tab.size_flags_vertical = Control.SIZE_EXPAND_FILL
	tab_container.add_child(env_tab)

	var env_vbox = VBoxContainer.new()
	env_vbox.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	env_vbox.add_theme_constant_override("separation", 18)
	env_tab.add_child(env_vbox)

	var env_title = Label.new()
	env_title.text = ":: atmospheric systems"
	_apply_neon_label(env_title, 20)
	env_vbox.add_child(env_title)

	var fog_hbox = HBoxContainer.new()
	fog_hbox.add_theme_constant_override("separation", 15)
	env_vbox.add_child(fog_hbox)

	var fog_label = Label.new()
	fog_label.text = "fog density"
	fog_label.custom_minimum_size = Vector2(140, 30)
	_apply_neon_label(fog_label, 14, true)
	fog_hbox.add_child(fog_label)

	fog_density_slider = HSlider.new()
	fog_density_slider.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	fog_density_slider.min_value = 0.0
	fog_density_slider.max_value = 0.12
	fog_density_slider.step = 0.001
	fog_density_slider.value = env.fog_density if env else 0.01
	fog_density_slider.connect("value_changed", Callable(self, "_on_fog_density_changed"))
	fog_hbox.add_child(fog_density_slider)

	var fog_value_label = Label.new()
	fog_value_label.text = "0.01"
	fog_value_label.custom_minimum_size = Vector2(50, 30)
	fog_density_slider.connect("value_changed", Callable(fog_value_label, "set_text").bind("%.3f"))
	_apply_neon_label(fog_value_label, 14)
	fog_hbox.add_child(fog_value_label)

	var light_hbox = HBoxContainer.new()
	light_hbox.add_theme_constant_override("separation", 15)
	env_vbox.add_child(light_hbox)

	var light_label = Label.new()
	light_label.text = "ambient energy"
	light_label.custom_minimum_size = Vector2(140, 30)
	_apply_neon_label(light_label, 14, true)
	light_hbox.add_child(light_label)

	ambient_light_slider = HSlider.new()
	ambient_light_slider.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	ambient_light_slider.min_value = 0.0
	ambient_light_slider.max_value = 1.2
	ambient_light_slider.step = 0.01
	ambient_light_slider.value = env.ambient_light_energy if env else 0.5
	ambient_light_slider.connect("value_changed", Callable(self, "_on_ambient_light_changed"))
	light_hbox.add_child(ambient_light_slider)

	var light_value_label = Label.new()
	light_value_label.text = "0.50"
	light_value_label.custom_minimum_size = Vector2(50, 30)
	ambient_light_slider.connect("value_changed", Callable(light_value_label, "set_text").bind("%.2f"))
	_apply_neon_label(light_value_label, 14)
	light_hbox.add_child(light_value_label)

	var volumetric_hbox = HBoxContainer.new()
	volumetric_hbox.add_theme_constant_override("separation", 15)
	env_vbox.add_child(volumetric_hbox)

	var volumetric_label = Label.new()
	volumetric_label.text = "volumetric fog"
	volumetric_label.custom_minimum_size = Vector2(140, 30)
	_apply_neon_label(volumetric_label, 14, true)
	volumetric_hbox.add_child(volumetric_label)

	volumetric_fog_slider = HSlider.new()
	volumetric_fog_slider.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	volumetric_fog_slider.min_value = 0.0
	volumetric_fog_slider.max_value = 0.08
	volumetric_fog_slider.step = 0.001
	volumetric_fog_slider.value = env.volumetric_fog_density if env else 0.02
	volumetric_fog_slider.connect("value_changed", Callable(self, "_on_volumetric_fog_changed"))
	volumetric_hbox.add_child(volumetric_fog_slider)

	var volumetric_value = Label.new()
	volumetric_value.text = "%.3f" % (volumetric_fog_slider.value)
	volumetric_value.custom_minimum_size = Vector2(60, 30)
	volumetric_fog_slider.connect("value_changed", Callable(volumetric_value, "set_text").bind("%.3f"))
	_apply_neon_label(volumetric_value, 14)
	volumetric_hbox.add_child(volumetric_value)

	var sun_hbox = HBoxContainer.new()
	sun_hbox.add_theme_constant_override("separation", 15)
	env_vbox.add_child(sun_hbox)

	var sun_label = Label.new()
	sun_label.text = "light intensity"
	sun_label.custom_minimum_size = Vector2(140, 30)
	_apply_neon_label(sun_label, 14, true)
	sun_hbox.add_child(sun_label)

	sun_energy_slider = HSlider.new()
	sun_energy_slider.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	sun_energy_slider.min_value = 0.0
	sun_energy_slider.max_value = 6.0
	sun_energy_slider.step = 0.05
	sun_energy_slider.value = sun_light.light_energy if sun_light else 3.0
	sun_energy_slider.connect("value_changed", Callable(self, "_on_sun_light_energy_changed"))
	sun_hbox.add_child(sun_energy_slider)

	var sun_value_label = Label.new()
	sun_value_label.text = "%.2f" % sun_energy_slider.value
	sun_value_label.custom_minimum_size = Vector2(60, 30)
	sun_energy_slider.connect("value_changed", Callable(sun_value_label, "set_text").bind("%.2f"))
	_apply_neon_label(sun_value_label, 14)
	sun_hbox.add_child(sun_value_label)

	dark_mode_button = Button.new()
	dark_mode_button.text = "ignite chrome dawn"
	dark_mode_button.size = Vector2(240, 45)
	dark_mode_button.connect("pressed", Callable(self, "_on_toggle_dark_mode"))
	_apply_neon_button(dark_mode_button, 16)
	env_vbox.add_child(dark_mode_button)

	var controls_tab = ScrollContainer.new()
	controls_tab.name = "Controls"
	controls_tab.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	controls_tab.size_flags_vertical = Control.SIZE_EXPAND_FILL
	tab_container.add_child(controls_tab)

	var controls_vbox = VBoxContainer.new()
	controls_vbox.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	controls_vbox.add_theme_constant_override("separation", 18)
	controls_tab.add_child(controls_vbox)

	var controls_title = Label.new()
	controls_title.text = ":: operator manual"
	_apply_neon_label(controls_title, 20)
	controls_vbox.add_child(controls_title)

	var movement_info = Label.new()
	movement_info.text = "W/A/S/D :: stride\nMouse :: orient\nWheel :: altitude\nTab :: toggle console"
	movement_info.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	_apply_neon_label(movement_info, 14, true)
	controls_vbox.add_child(movement_info)

	var audio_title = Label.new()
	audio_title.text = ":: audio uplink"
	_apply_neon_label(audio_title, 16)
	controls_vbox.add_child(audio_title)

	var audio_grid = GridContainer.new()
	audio_grid.columns = 2
	audio_grid.add_theme_constant_override("h_separation", 10)
	audio_grid.add_theme_constant_override("v_separation", 10)

	var toggle_mic_button = Button.new()
	toggle_mic_button.text = "toggle mic channel"
	toggle_mic_button.size = Vector2(180, 45)
	toggle_mic_button.tooltip_text = "Enable or disable live voice capture"
	toggle_mic_button.connect("pressed", Callable(self, "_on_toggle_mic"))
	_apply_neon_button(toggle_mic_button, 16)
	audio_grid.add_child(toggle_mic_button)

	var test_transcription_button = Button.new()
	test_transcription_button.text = "inject test transcription"
	test_transcription_button.size = Vector2(220, 45)
	test_transcription_button.tooltip_text = "Emit a synthetic transcription payload"
	test_transcription_button.connect("pressed", Callable(self, "_on_test_transcription"))
	_apply_neon_button(test_transcription_button, 16)
	audio_grid.add_child(test_transcription_button)

	controls_vbox.add_child(audio_grid)

	var actions_tab = ScrollContainer.new()
	actions_tab.name = "Actions"
	actions_tab.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	actions_tab.size_flags_vertical = Control.SIZE_EXPAND_FILL
	tab_container.add_child(actions_tab)

	var actions_vbox = VBoxContainer.new()
	actions_vbox.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	actions_vbox.add_theme_constant_override("separation", 18)
	actions_tab.add_child(actions_vbox)

	var actions_title = Label.new()
	actions_title.text = ":: quick routines"
	_apply_neon_label(actions_title, 20)
	actions_vbox.add_child(actions_title)

	var actions_grid = GridContainer.new()
	actions_grid.columns = 2
	actions_grid.add_theme_constant_override("h_separation", 10)
	actions_grid.add_theme_constant_override("v_separation", 10)

	var list_tasks_button = Button.new()
	list_tasks_button.text = "scan task mesh"
	list_tasks_button.size = Vector2(180, 45)
	list_tasks_button.tooltip_text = "Emit a fresh task scan to the scene"
	list_tasks_button.connect("pressed", Callable(self, "_on_list_tasks"))
	_apply_neon_button(list_tasks_button, 16)
	actions_grid.add_child(list_tasks_button)

	var clear_button = Button.new()
	clear_button.text = "purge world nodes"
	clear_button.size = Vector2(180, 45)
	clear_button.tooltip_text = "Remove all visualized entities"
	clear_button.connect("pressed", Callable(self, "_on_clear_objects"))
	_apply_neon_button(clear_button, 16)
	actions_grid.add_child(clear_button)

	var purge_db_button = Button.new()
	purge_db_button.text = "wipe events.db"
	purge_db_button.size = Vector2(180, 45)
	purge_db_button.tooltip_text = "Delete all stored events and reset state"
	purge_db_button.connect("pressed", Callable(self, "_on_purge_all_data"))
	_apply_neon_button(purge_db_button, 16)
	actions_grid.add_child(purge_db_button)

	var nightly_button = Button.new()
	nightly_button.text = "run nightly scoring"
	nightly_button.size = Vector2(220, 45)
	nightly_button.tooltip_text = "Replay events and score them via LLM tool"
	nightly_button.connect("pressed", Callable(self, "_on_run_nightly_scoring"))
	_apply_neon_button(nightly_button, 16)
	actions_grid.add_child(nightly_button)

	var particles_button = Button.new()
	particles_button.text = "toggle ambient motes"
	particles_button.size = Vector2(200, 45)
	particles_button.tooltip_text = "Show or hide the atmospheric particle layer"
	_apply_neon_button(particles_button, 16)
	particles_button.connect("pressed", Callable(self, "_on_toggle_particles"))
	actions_grid.add_child(particles_button)

	actions_vbox.add_child(actions_grid)

	var request_title = Label.new()
	request_title.text = ":: dispatch request"
	_apply_neon_label(request_title, 18)
	actions_vbox.add_child(request_title)

	var request_hbox = HBoxContainer.new()
	request_hbox.add_theme_constant_override("separation", 10)
	actions_vbox.add_child(request_hbox)

	user_request_input = LineEdit.new()
	user_request_input.custom_minimum_size = Vector2(500, 45)
	user_request_input.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	user_request_input.placeholder_text = "channel instructions to the palace..."
	# Pause movement while typing top-level requests
	user_request_input.focus_entered.connect(Callable(self, "_on_text_input_focus_entered"))
	user_request_input.focus_exited.connect(Callable(self, "_on_text_input_focus_exited"))
	_apply_neon_line_edit(user_request_input, 16)
	request_hbox.add_child(user_request_input)

	send_request_button = Button.new()
	send_request_button.text = "execute"
	send_request_button.size = Vector2(120, 45)
	send_request_button.connect("pressed", Callable(self, "_on_send_request"))
	_apply_neon_button(send_request_button, 16)
	request_hbox.add_child(send_request_button)

	var system_tab = ScrollContainer.new()
	system_tab.name = "Diagnostics"
	system_tab.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	system_tab.size_flags_vertical = Control.SIZE_EXPAND_FILL
	tab_container.add_child(system_tab)

	var system_vbox = VBoxContainer.new()
	system_vbox.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	system_vbox.add_theme_constant_override("separation", 18)
	system_tab.add_child(system_vbox)

	var system_title = Label.new()
	system_title.text = ":: diagnostics"
	_apply_neon_label(system_title, 20)
	system_vbox.add_child(system_title)

	var debug_info = Label.new()
	debug_info.name = "debug_info"
	debug_info.text = "objects online: " + str(event_cubes.size()) + "\nunderground: " + str(underground_node.get_child_count() if underground_node else 0)
	_apply_neon_label(debug_info, 14, true)
	system_vbox.add_child(debug_info)

	var exit_button = Button.new()
	exit_button.text = "terminate session"
	exit_button.size = Vector2(220, 48)
	exit_button.tooltip_text = "Quit MindPalace immediately."
	_apply_neon_button(exit_button, 16)
	var exit_normal = StyleBoxFlat.new()
	exit_normal.bg_color = Color(0.12, 0.0, 0.02, 0.95)
	exit_normal.border_color = Color(1.0, 0.26, 0.42, 1.0)
	exit_normal.border_width_left = 3
	exit_normal.border_width_right = 3
	exit_normal.border_width_top = 3
	exit_normal.border_width_bottom = 3
	exit_normal.corner_radius_bottom_left = 10
	exit_normal.corner_radius_bottom_right = 10
	exit_normal.corner_radius_top_left = 10
	exit_normal.corner_radius_top_right = 10
	var exit_hover = exit_normal.duplicate()
	exit_hover.bg_color = exit_normal.bg_color.lightened(0.1)
	exit_button.add_theme_stylebox_override("normal", exit_normal)
	exit_button.add_theme_stylebox_override("hover", exit_hover)
	exit_button.add_theme_color_override("font_color", Color(1.0, 0.46, 0.6, 1.0))
	exit_button.connect("pressed", Callable(self, "_on_exit_mindpalace"))
	system_vbox.add_child(exit_button)



func _on_close_settings():
	toggle_settings_menu()

func _on_exit_mindpalace():
	var dialog = ConfirmationDialog.new()
	dialog.title = "Exit MindPalace"
	dialog.dialog_text = "Are you sure you want to exit MindPalace?\n\nAll unsaved progress will be lost."
	dialog.ok_button_text = "Exit"
	dialog.cancel_button_text = "Cancel"
	dialog.connect("confirmed", Callable(self, "_confirm_exit"))
	add_child(dialog)
	dialog.popup_centered()

func _confirm_exit():
	get_tree().quit()

# func toggle_birdview():
#	birdview_active = !birdview_active
#	if tween:
#	  tween.kill()
#	tween = create_tween()
#	tween.tween_callback(Callable(self, "_on_birdview_tween_complete"))
#	if birdview_active:
#	  # Tween to birdview
#	  tween.tween_property($Player/Camera, "position", birdview_camera_pos, 1.0)
#	  tween.tween_property($Player/Camera, "rotation", birdview_camera_rot, 1.0)
#	  Input.mouse_mode = Input.MOUSE_MODE_VISIBLE
#	  $Player.movement_disabled = true
#	  # Optional: orthographic
#	  camera.projection = Camera3D.PROJECTION_ORTHOGONAL
#	  camera.size = 100
#	  # Update HUD
#	  targeting_hud_label.text = "Birdview Mode: Click objects to interact"
#	  targeting_hud_panel.visible = true
#	else:
#	  # Tween back to player
#	  var player_pos = $Player.position
#	  tween.tween_property($Player/Camera, "position", player_pos + Vector3(0, 1.5, 10), 1.0)
#	  tween.tween_property($Player/Camera, "rotation", Vector3(-PI/6, 0, 0), 1.0)
#	  Input.mouse_mode = Input.MOUSE_MODE_CAPTURED
#	  $Player.movement_disabled = false
#	  camera.projection = Camera3D.PROJECTION_PERSPECTIVE
#	  clear_targeting_hud()

# func _on_birdview_tween_complete():
#	pass  # Placeholder

# func birdview_click(mouse_pos: Vector2):
#	var ray_origin = camera.project_ray_origin(mouse_pos)
#	var ray_dir = camera.project_ray_normal(mouse_pos)
#	var ray_length = 1000.0
#	var space_state = get_world_3d().direct_space_state
#	var query = PhysicsRayQueryParameters3D.create(ray_origin, ray_origin + ray_dir * ray_length)
#	var result = space_state.intersect_ray(query)
#	if result:
#	  var hit_object = result.collider
#	  var event_id = get_event_id_from_object(hit_object)
#	  if event_id:
#		# Highlight
#		if hit_object is MeshInstance3D:
#		  var highlight_tween = create_tween()
#		  var original_scale = hit_object.scale
#		  highlight_tween.tween_property(hit_object, "scale", original_scale * 1.2, 0.2)
#		  highlight_tween.tween_property(hit_object, "scale", original_scale, 0.2).set_delay(0.5)
#		# Send interaction
#		send_interaction(event_id, "select")
#		# Update HUD
#	   update_hud_for_object(event_id, hit_object)

# func send_interaction(node_id: String, action: String):
#	if websocket.get_ready_state() != WebSocketPeer.STATE_OPEN:
#	  return
#	var msg = {
#	  "type": "birdview_interact",
#	  "node_id": node_id,
#	  "action": action
#	}
#	var json_string = JSON.stringify(msg)
#	websocket.send_text(json_string)

func toggle_settings_menu():
	print("Toggling settings menu")
	if not settings_panel:
		print("Settings panel is null")
		return
	settings_visible = !settings_visible
	settings_panel.visible = settings_visible
	print("Settings visible: ", settings_visible)

	if settings_visible:
		Input.mouse_mode = Input.MOUSE_MODE_VISIBLE	 # Release mouse for GUI interaction
		# Update debug info
		var debug_label = settings_panel.find_child("debug_info", true, false)
		if debug_label:
			debug_label.text = "Debug Info:\nObjects: " + str(event_cubes.size()) + "\nUnderground: " + str(underground_node.get_child_count() if underground_node else 0)
	else:
		Input.mouse_mode = Input.MOUSE_MODE_CAPTURED  # Capture mouse for camera control

	send_state_update()



# Control panel handlers
func _on_fog_density_changed(value: float):
	if env:
		env.fog_density = value

func _on_ambient_light_changed(value: float):
	if env:
		env.ambient_light_energy = value

func _on_volumetric_fog_changed(value: float):
	if env:
		env.volumetric_fog_density = value

func _on_sun_light_energy_changed(value: float):
	if sun_light:
		sun_light.light_energy = value
	if core_light:
		core_light.light_energy = clamp(value * 1.6, 0.0, 9.0)

func _on_toggle_particles():
	if ambient_particles:
		ambient_particles.visible = !ambient_particles.visible

func _on_toggle_mic():
	send_toggle_mic()

func _on_toggle_dark_mode():
	dark_mode = !dark_mode
	if dark_mode_button:
		dark_mode_button.text = "ignite chrome dawn" if dark_mode else "restore night grid"
	if env:
		if dark_mode:
			env.background_mode = Environment.BG_COLOR
			env.background_color = Color(0.003, 0.006, 0.012, 1.0)
			env.fog_enabled = true
			env.fog_color = Color(0.0, 0.18, 0.32, 0.9)
			env.fog_density = 0.02
			env.volumetric_fog_density = 0.03
			env.glow_intensity = 1.35
			env.ambient_light_energy = 0.22
		else:
			env.background_mode = Environment.BG_COLOR
			env.background_color = Color(0.2, 0.08, 0.05, 1.0)
			env.fog_enabled = true
			env.fog_color = Color(0.45, 0.12, 0.08, 0.45)
			env.fog_density = 0.006
			env.volumetric_fog_density = 0.015
			env.glow_intensity = 0.95
			env.ambient_light_energy = 0.48

	if fog_density_slider:
		fog_density_slider.value = env.fog_density if env else fog_density_slider.value
	if volumetric_fog_slider:
		volumetric_fog_slider.value = env.volumetric_fog_density if env else volumetric_fog_slider.value
	if ambient_light_slider:
		ambient_light_slider.value = env.ambient_light_energy if env else ambient_light_slider.value

	if sun_light:
		sun_light.light_color = Color(0.22, 0.65, 1.0, 1.0) if dark_mode else Color(1.0, 0.55, 0.25, 1.0)
		sun_light.light_energy = 2.0 if dark_mode else 2.6
	if core_light:
		core_light.light_color = Color(0.82, 0.16, 1.1, 1.0) if dark_mode else Color(1.0, 0.36, 0.3, 1.0)
		core_light.light_energy = 3.2 if dark_mode else 4.2
	if sun_energy_slider and sun_light:
		sun_energy_slider.value = sun_light.light_energy

func _on_create_task():
	send_request("Create a new task titled 'New Task from Control Panel'")

func _on_list_tasks():
	send_request("List all tasks")

func _on_clear_objects():
	send_request("Clear all objects in the 3D world")

func _on_purge_all_data():
	# Basic confirmation for destructive action
	var dlg := AcceptDialog.new()
	dlg.dialog_text = "Wipe all local events? This cannot be undone."
	dlg.ok_button_text = "Confirm Purge"
	get_tree().root.add_child(dlg)
	dlg.connect("confirmed", Callable(self, "_confirm_purge_all_data"))
	dlg.popup_centered(Vector2(420, 180))

func _confirm_purge_all_data():
	var ok = send_ui_command("PurgeAllData", {})
	if ok:
		log_message("Issued PurgeAllData command")

func _on_run_nightly_scoring():
	var payload := {
		"Aggregate": "taskmanager",
		"ScoreName": "relevance",
		"Label": "Task List",
	}
	var ok = send_ui_command("RunNightlyScoring", payload)
	if ok:
		log_message("Triggered nightly scoring replay")

func _on_send_request():
	var text = user_request_input.text.strip_edges()
	if text != "":
		send_request(text)
		user_request_input.text = ""

func send_request(text: String, target_agent: String = ""):
	if websocket.get_ready_state() != WebSocketPeer.STATE_OPEN:
		return
	var req_msg = {
		"type": "request",
		"text": text
	}
	if target_agent != "":
		req_msg["target_agent"] = target_agent
	var json_string = JSON.stringify(req_msg)
	var err = websocket.send_text(json_string)
	if err != OK:
		push_error("Failed to send request: ", err)

func _on_conversation_send_pressed():
	_send_conversation_message()

func _on_conversation_text_submitted(text: String):
	conversation_input.text = text
	_send_conversation_message()

func _send_conversation_message():
	var text = conversation_input.text.strip_edges()
	if text == "":
		return
	send_request(text, current_conversation_agent)
	conversation_input.text = ""

func send_toggle_mic():
	if websocket.get_ready_state() != WebSocketPeer.STATE_OPEN:
		return
	var msg = {
		"type": "toggle_mic"
	}
	var json_string = JSON.stringify(msg)
	var err = websocket.send_text(json_string)
	if err != OK:
		push_error("Failed to send toggle mic: ", err)

func show_orchestrator_menu(mouse_pos: Vector2):
	if orchestrator_menu:
		return  # Menu already open

	var canvas_layer = CanvasLayer.new()
	add_child(canvas_layer)
	orchestrator_menu = canvas_layer

	# Background for click-outside detection
	var bg = ColorRect.new()
	bg.size = get_viewport().size
	bg.color = Color(0, 0, 0, 0.45)  # Darkened veil
	canvas_layer.add_child(bg)
	bg.connect("gui_input", Callable(self, "_on_menu_bg_input"))

	# Menu container
	var menu_control = Control.new()
	menu_control.position = mouse_pos
	canvas_layer.add_child(menu_control)

	# Button
	var button = Button.new()
	button.text = "link voice uplink"
	button.size = Vector2(200, 40)
	button.position = Vector2(-100, 20)  # Center below cursor
	_apply_neon_button(button, 16)
	menu_control.add_child(button)
	button.connect("pressed", Callable(self, "_on_activate_transcription"))



func _on_activate_transcription():
	send_toggle_mic()
	log_message("Activated transcription via orchestrator menu")
	close_orchestrator_menu()

func _on_menu_bg_input(event):
	if event is InputEventMouseButton and event.pressed:
		close_orchestrator_menu()

func close_orchestrator_menu():
	if orchestrator_menu:
		orchestrator_menu.queue_free()
	orchestrator_menu = null

# When a text input gains focus, pause movement and show cursor.
func _on_text_input_focus_entered():
	if has_node("Player"):
		$Player.movement_disabled = true
	Input.mouse_mode = Input.MOUSE_MODE_VISIBLE

# When text input loses focus, only re-enable movement if no other text field is focused.
func _on_text_input_focus_exited():
	var f = get_viewport().gui_get_focus_owner()
	var text_still_focused = f != null and (f is LineEdit or f is TextEdit)
	if text_still_focused:
		return
	if has_node("Player"):
		$Player.movement_disabled = false
	# Avoid recapturing mouse while global overlays are open
	if not settings_visible and model_picker_layer == null:
		Input.mouse_mode = Input.MOUSE_MODE_CAPTURED


func _load_model_resource(model_path: String):
	if model_path == "":
		return null
	if model_cache.has(model_path):
		return model_cache[model_path]
	var resource = ResourceLoader.load(model_path)
	if resource == null:
		push_warning("Failed to load model at %s" % model_path)
		return null
	model_cache[model_path] = resource
	return resource


func _apply_model_to_node(node: Node, model_path_value):
	if node == null:
		return
	if typeof(model_path_value) != TYPE_STRING:
		return
	var model_path: String = model_path_value
	if model_path == "":
		return
	var resource = _load_model_resource(model_path)
	if resource == null:
		return
	if node.has_meta("model_path") and node.get_meta("model_path") == model_path:
		return
	node.set_meta("model_path", model_path)
	if node is MeshInstance3D:
		if resource is ArrayMesh or resource is Mesh:
			node.mesh = resource
		elif resource is PackedScene:
			for child in node.get_children():
				child.queue_free()
			var inst = resource.instantiate()
			node.add_child(inst)
	else:
		if resource is PackedScene:
			for child in node.get_children():
				child.queue_free()
			var inst_scene = resource.instantiate()
			node.add_child(inst_scene)
		elif resource is ArrayMesh or resource is Mesh:
			for child in node.get_children():
				child.queue_free()
			var mesh_instance := MeshInstance3D.new()
			mesh_instance.mesh = resource
			node.add_child(mesh_instance)

func format_with_line_breaks(text: String, chars_per_line: int) -> String:
	var words = text.split(" ", false)
	var lines = []
	var current_line = ""
	var char_count = 0

	for word in words:
		var word_with_space = word + " "
		var word_len = word_with_space.length()
		if char_count + word_len > chars_per_line:
			lines.append(current_line.strip_edges())
			current_line = ""
			char_count = 0
		current_line += word_with_space
		char_count += word_len

	if current_line != "":
		lines.append(current_line.strip_edges())

	return "\n".join(lines)

# Animation helper functions
func get_node_by_id(node_id: String) -> Node:
	# First check event_cubes
	if event_cubes.has(node_id):
		return event_cubes[node_id].get("node", null)
	# Then check scene tree
	return get_node_or_null(node_id)




func start_orchestrator_animation():
	print("GODOT: Starting orchestrator animation")
	var orchestrator = get_node_or_null("orchestrator_ai")
	if not orchestrator:
		print("GODOT: Orchestrator AI node not found")
		return
	print("GODOT: Found orchestrator node at " + str(orchestrator.position))
	if orchestrator_tween:
		orchestrator_tween.kill()
		print("GODOT: Killed previous tween")
	orchestrator_tween = create_tween()
	print("GODOT: Created new tween")
	# Define spiral positions downward, simulating reading event cubes
	var positions = []
	for i in range(20):
		var theta = i * 0.3
		var r = 0.2 + i * 0.05
		var x = r * cos(theta)
		var z = r * sin(theta)
		var y = -i * 0.2
		positions.append(Vector3(x, y, z))
	print("GODOT: Defined " + str(positions.size()) + " positions")
	# Tween to each position sequentially
	for pos in positions:
		orchestrator_tween.tween_property(orchestrator, "position", pos, 0.3).set_trans(Tween.TRANS_SINE).set_ease(Tween.EASE_IN_OUT)
	# Return to center
	orchestrator_tween.tween_property(orchestrator, "position", Vector3(0, 0, 0), 0.5)
	print("GODOT: Animation tween set up")
