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

const MONOKAI_BASE = Color(0.15294, 0.15686, 0.13333, 1.0)
const MONOKAI_BASE_DEEP = Color(0.11765, 0.12157, 0.1098, 1.0)
const MONOKAI_FOG = Color(0.06667, 0.07059, 0.05882, 1.0)
const MONOKAI_FG = Color(0.97255, 0.97255, 0.94902, 1.0)
const MONOKAI_MUTED = Color(0.45882, 0.44314, 0.36863, 1.0)
const MONOKAI_GREEN = Color(0.65098, 0.88627, 0.18039, 1.0)
const MONOKAI_ORANGE = Color(0.99216, 0.59216, 0.12157, 1.0)
const MONOKAI_PINK = Color(0.97647, 0.15294, 0.44706, 1.0)
const MONOKAI_BLUE = Color(0.4, 0.851, 0.937, 1.0)
const MONOKAI_PURPLE = Color(0.68235, 0.50588, 1.0, 1.0)
const MONOKAI_YELLOW = Color(0.902, 0.858, 0.4549, 1.0)

# Mapping of event types to colors
const EVENT_COLORS = {
	"user_request_received": MONOKAI_BLUE,
	"task_created": MONOKAI_GREEN,
	"task_updated": MONOKAI_YELLOW,
	"task_completed": MONOKAI_ORANGE,
	"task_deleted": MONOKAI_PINK,
	"task": MONOKAI_GREEN,
	"note_created": MONOKAI_PINK,
	"note_updated": MONOKAI_PURPLE,
	"note_deleted": MONOKAI_PINK,
	"note": MONOKAI_PURPLE,
	"calendar_event_created": MONOKAI_BLUE,
	"calendar_event_updated": MONOKAI_YELLOW,
	"calendar_event_deleted": MONOKAI_PINK,
	"calendar_event": MONOKAI_BLUE,
	"plugin_generated": MONOKAI_PURPLE,
	"request_completed": MONOKAI_GREEN,
	"agent_call_decided": MONOKAI_ORANGE,
	"agent_execution_failed": MONOKAI_PINK,
	"tool_call_failed": MONOKAI_PINK,
	"tool_call_started": MONOKAI_ORANGE,
	"tool_call_completed": MONOKAI_GREEN,
	"orchestrator_ai": MONOKAI_YELLOW,
}

const PLUGIN_FIELD_TYPES := [
	{
		"label": "Text",
		"go": "string",
		"hint": "Names, notes, descriptors.",
	},
	{
		"label": "Integer",
		"go": "int",
		"hint": "Whole numbers such as counts or rating levels.",
	},
	{
		"label": "Decimal",
		"go": "float64",
		"hint": "Measurements that need precision (fuel litres, weights).",
	},
	{
		"label": "Boolean",
		"go": "bool",
		"hint": "True/false toggles.",
	},
	{
		"label": "Timestamp",
		"go": "time.Time",
		"hint": "Dates or exact times (stored as ISO strings).",
	},
]

const PLUGIN_INTERVIEW_STEPS := [
	{
		"id": "plugin_name",
		"title": "Name the Plugin",
		"prompt": "Give this plugin a short codename (letters, numbers, underscores).",
	},
	{
		"id": "description",
		"title": "Describe the Work",
		"prompt": "In one or two sentences, explain what this plugin should help with.",
	},
	{
		"id": "entity_name",
		"title": "Primary Entity",
		"prompt": "What's the singular item this plugin tracks? (Examples: Drink, FuelLog, LessonPlan)",
	},
	{
		"id": "fields",
		"title": "Capture the Fields",
		"prompt": "List the data you need for each record. We'll auto-add an ID so focus on meaningful attributes.",
	},
	{
		"id": "commands",
		"title": "Choose Commands",
		"prompt": "Pick the actions MindPalace should wire up for this plugin.",
	},
	{
		"id": "review",
		"title": "Review & Forge",
		"prompt": "Double-check the generated spec, then light the forge.",
	},
]

const UI_BG = Color(MONOKAI_BASE.r, MONOKAI_BASE.g, MONOKAI_BASE.b, 0.92)
const UI_BG_DEEP = Color(MONOKAI_BASE_DEEP.r, MONOKAI_BASE_DEEP.g, MONOKAI_BASE_DEEP.b, 0.98)
const UI_PANEL_BORDER = MONOKAI_ORANGE
const UI_ACCENT = MONOKAI_GREEN
const UI_TEXT = MONOKAI_FG
const UI_MUTED_TEXT = MONOKAI_MUTED
const LABEL_OUTLINE_COLOR = Color(MONOKAI_FOG.r, MONOKAI_FOG.g, MONOKAI_FOG.b, 1.0)
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
var model_cache = {}

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

# Plugin interview UI
var plugin_interview_flow = null

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
var zones_state: Dictionary = {}

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


func _set_nested_path(dict: Dictionary, path: String, value: Variant) -> void:
	# Creates intermediate dictionaries as needed and sets a value at the dotted path
	if path == null or path == "":
		return
	var segments := path.split(".")
	var current: Dictionary = dict
	for i in range(segments.size()):
		var seg := str(segments[i])
		var is_last := i == segments.size() - 1
		if is_last:
			current[seg] = value
			return
		if not current.has(seg) or typeof(current[seg]) != TYPE_DICTIONARY:
			current[seg] = {}
		current = current[seg]


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
	var args: Dictionary = {}
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


func dispatch_component_action(node_id: String, action_key: String, context: Dictionary = {}) -> bool:
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
	var payload: Dictionary = {}
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

# Task card constants
const TASK_CARD_SIZE: Vector3 = Vector3(0.9, 1.3, 0.06)
const TASK_CARD_FACE: Vector2 = Vector2(0.86, 1.26)
const TASK_CARD_UI_SIZE: Vector2i = Vector2i(512, 768)


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


func _apply_neon_line_edit(line_edit: Control, font_size: int = 16):
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


func _create_task_card_ui(viewport: SubViewport) -> Control:
	# Root UI for task card front; returns the Control root
	var root = Control.new()
	root.name = "CardUI"
	root.size = TASK_CARD_UI_SIZE
	root.custom_minimum_size = TASK_CARD_UI_SIZE

	var panel = Panel.new()
	panel.name = "CardPanel"
	panel.size = TASK_CARD_UI_SIZE
	_apply_neon_panel(panel, UI_PANEL_BORDER, UI_BG, 3, 18)
	root.add_child(panel)

	var vbox = VBoxContainer.new()
	vbox.name = "CardVBox"
	vbox.anchor_left = 0
	vbox.anchor_top = 0
	vbox.anchor_right = 1
	vbox.anchor_bottom = 1
	vbox.offset_left = 16
	vbox.offset_top = 16
	vbox.offset_right = -16
	vbox.offset_bottom = -16
	vbox.add_theme_constant_override("separation", 10)
	panel.add_child(vbox)

	var title = Label.new()
	title.name = "Title"
	_apply_neon_label(title, 30)
	title.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	vbox.add_child(title)

	var divider = ColorRect.new()
	divider.color = UI_PANEL_BORDER
	divider.custom_minimum_size = Vector2(0, 2)
	vbox.add_child(divider)

	var desc = RichTextLabel.new()
	desc.name = "Description"
	desc.bbcode_enabled = true
	desc.scroll_active = false
	desc.fit_content = true
	desc.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	desc.add_theme_font_size_override("normal_font_size", 20)
	desc.add_theme_color_override("default_color", UI_TEXT)
	vbox.add_child(desc)

	viewport.add_child(root)
	return root


func _refresh_task_card(node: Node):
	if not (node is Node3D):
		return
	if not node.has_meta("task_card"):
		return
	var ui_root: Control = node.get_node_or_null("CardViewport/CardUI")
	if ui_root == null:
		return
	var title_label: Label = ui_root.get_node_or_null("CardPanel/CardVBox/Title")
	var desc_label: RichTextLabel = ui_root.get_node_or_null("CardPanel/CardVBox/Description")
	if title_label == null or desc_label == null:
		return

	var entry_info := _find_registered_entry_for_node(node)
	var component_props: Dictionary = entry_info.get("entry", {}).get("component_properties", {})
	var display_info = node.get_meta("display_info", {})

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

	title_label.text = title_text
	desc_label.clear()
	if desc_text != "":
		desc_label.append_text(desc_text)

	# Update border color by status if present
	var border_color := UI_PANEL_BORDER
	var status := str(component_props.get("status", ""))
	if status != "":
		match status:
			"Completed":
				border_color = MONOKAI_GREEN
			"Blocked":
				border_color = MONOKAI_PINK
			"In Progress":
				border_color = MONOKAI_YELLOW
	var panel: Panel = ui_root.get_node_or_null("CardPanel")
	if panel:
		_apply_neon_panel(panel, border_color, UI_BG, 3, 18)

	# Force a redraw of the viewport texture
	var vp: SubViewport = node.get_node_or_null("CardViewport")
	if vp:
		vp.render_target_update_mode = SubViewport.UPDATE_ALWAYS


func _augment_orchestrator(node: MeshInstance3D):
	node.name = "orchestrator_ai"

	# Minimal, bright pinpoint: a tiny sphere with strong light, no bubbles.
	if node.mesh == null or not (node.mesh is SphereMesh):
		var sphere = SphereMesh.new()
		sphere.radial_segments = 32
		sphere.rings = 16
		sphere.radius = 0.06
		node.mesh = sphere
	node.scale = Vector3.ONE

	var core_material = StandardMaterial3D.new()
	core_material.albedo_color = Color(0, 0, 0, 1)
	core_material.metallic = 0.0
	core_material.roughness = 0.2
	core_material.emission_enabled = true
	# Warm-white emission to help the glow pop under bloom
	core_material.emission = Color(1.0, 0.98, 0.85, 1.0)
	core_material.emission_energy_multiplier = 1.2
	core_material.transparency = BaseMaterial3D.TRANSPARENCY_DISABLED
	node.material_override = core_material

	# Ensure a strong point light to drive volumetric scattering
	var light: OmniLight3D = node.get_node_or_null("OrchestratorLight")
	if light == null:
		light = OmniLight3D.new()
		light.name = "OrchestratorLight"
		node.add_child(light)
	light.light_color = Color(1.0, 0.98, 0.85, 1.0)
	light.light_energy = 14.0
	light.omni_range = 12.0
	light.shadow_enabled = false
	# Boost contribution to volumetric fog for a dense halo
	if light.has_method("set"):
		# Godot exposes this as a property on Light3D subclasses in 4.x
		light.set("volumetric_fog_energy", 2.0)

	# Clean up any legacy bubbly effects if present
	for child_name in ["OrchestratorHalo", "OrchestratorAura", "OrchestratorMistShell"]:
		if node.has_node(child_name):
			node.get_node(child_name).queue_free()

	# Kick shimmering animation if available
	if orchestrator_tween == null:
		start_orchestrator_animation()


func _augment_task_card(node: MeshInstance3D, properties: Dictionary):
	# Turn a MeshInstance into a thin card with a live UI front
	node.set_meta("task_card", true)
	# Body geometry
	var body := BoxMesh.new()
	body.size = TASK_CARD_SIZE
	node.mesh = body
	var body_mat = StandardMaterial3D.new()
	body_mat.albedo_color = Color(MONOKAI_BASE_DEEP.r, MONOKAI_BASE_DEEP.g, MONOKAI_BASE_DEEP.b, 1.0)
	body_mat.roughness = 0.45
	body_mat.metallic = 0.08
	body_mat.emission_enabled = true
	body_mat.emission = UI_PANEL_BORDER.darkened(0.25)
	body_mat.emission_energy_multiplier = 0.4
	node.material_override = body_mat

	# SubViewport for front face UI
	var vp := SubViewport.new()
	vp.name = "CardViewport"
	vp.size = TASK_CARD_UI_SIZE
	vp.own_world_3d = false
	vp.render_target_update_mode = SubViewport.UPDATE_ONCE
	vp.transparent_bg = true
	node.add_child(vp)
	_create_task_card_ui(vp)

	# Front plane that displays the viewport texture
	var front := MeshInstance3D.new()
	front.name = "CardFront"
	var plane := PlaneMesh.new()
	plane.size = TASK_CARD_FACE
	front.mesh = plane
	# Position slightly in front of the card body on +Z
	front.position = Vector3(0, 0, (TASK_CARD_SIZE.z * 0.5) + 0.002)
	# Material for front: show the viewport texture, no shading
	var m := StandardMaterial3D.new()
	m.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	m.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
	m.albedo_texture = vp.get_texture()
	front.material_override = m
	node.add_child(front)

	# Initial content
	_refresh_task_card(node)


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
		env.background_color = MONOKAI_BASE
		env.ambient_light_color = Color(0.58, 0.54, 0.42, 1.0)
		env.ambient_light_energy = 0.3
		env.fog_enabled = true
		env.fog_color = Color(MONOKAI_BASE_DEEP.r, MONOKAI_BASE_DEEP.g, MONOKAI_BASE_DEEP.b, 0.92)
		env.fog_density = 0.008
		env.fog_height_min = -10.0
		env.fog_height_max = 25.0
		env.glow_enabled = true
		env.glow_intensity = 0.9
		env.glow_strength = 0.8
		env.glow_hdr_threshold = 0.7
		env.volumetric_fog_enabled = true
		env.volumetric_fog_density = 0.02
		env.volumetric_fog_emission = Color(MONOKAI_ORANGE.r, MONOKAI_ORANGE.g, MONOKAI_ORANGE.b, 0.4)

	# Add stylised lighting
	sun_light = DirectionalLight3D.new()
	sun_light.name = "CyberSun"
	sun_light.rotation_degrees = Vector3(-35, 40, 0)
	sun_light.light_color = MONOKAI_YELLOW
	sun_light.light_energy = 3.0
	sun_light.shadow_enabled = true
	add_child(sun_light)

	core_light = OmniLight3D.new()
	core_light.name = "PulseCore"
	core_light.position = Vector3(0, 6, 0)
	core_light.light_color = MONOKAI_PINK
	core_light.light_energy = 6.0
	core_light.omni_range = 45.0
	core_light.shadow_enabled = true
	add_child(core_light)

	# Add ambient particle field for hovering holographic motes
	ambient_particles = GPUParticles3D.new()
	var ambient_material = ParticleProcessMaterial.new()
	ambient_material.emission_shape = ParticleProcessMaterial.EMISSION_SHAPE_BOX
	ambient_material.emission_box_extents = Vector3(60, 25, 60)
	ambient_material.color = Color(MONOKAI_BLUE.r, MONOKAI_BLUE.g, MONOKAI_BLUE.b, 0.28)
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

	# Prepare plugin interview workflow helper
	plugin_interview_flow = PluginInterviewFlow.new(self)



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

func _get_zone_basis(zone_name: String) -> Dictionary:
	var zone_data: Dictionary = {}
	if zones_state.has(zone_name):
		zone_data = zones_state[zone_name]
	var angle_deg: float = float(zone_data.get("angle", 0.0))
	var radius_source: float = float(zone_data.get("radius", 12.0))
	var radius: float = max(1.0, radius_source)
	var angle_rad: float = deg_to_rad(angle_deg)
	var forward: Vector3 = Vector3(cos(angle_rad), 0.0, sin(angle_rad)).normalized()
	var right: Vector3 = Vector3(-forward.z, 0.0, forward.x).normalized()
	# Pull cards much closer to the orchestrator (center)
	var center_dist: float = clamp(radius * 0.12, 1.2, 3.6)
	var centerline: Vector3 = forward * center_dist
	return {
		"forward": forward,
		"right": right,
		"center": centerline,
		"radius": radius,
	}

func _collect_task_nodes() -> Array:
	var tasks: Array = []
	for id in event_cubes.keys():
		var entry: Dictionary = event_cubes[id]
		if entry.get("auxiliary", false):
			continue
		var node: Node = entry.get("node", null)
		if node == null:
			continue
		if node is MeshInstance3D:
			var is_task := false
			if node.has_meta("event_type") and str(node.get_meta("event_type")) == "task":
				is_task = true
			else:
				var props: Dictionary = entry.get("component_properties", {})
				if typeof(props) == TYPE_DICTIONARY:
					if props.has("status") or props.has("title") or props.has("description"):
						is_task = true
			if is_task:
				tasks.append({"id": id, "node": node})
	return tasks

func reflow_taskmanager_layout():
	var basis: Dictionary = _get_zone_basis("taskmanager")
	var forward: Vector3 = basis.get("forward", Vector3(0,0,1))
	var right: Vector3 = basis.get("right", Vector3(1,0,0))
	var center: Vector3 = basis.get("center", Vector3(0,0,6))
	var radius: float = float(basis.get("radius", 12.0))

	var items: Array = _collect_task_nodes()
	if items.is_empty():
		return

	# Determine grid size adaptively
	var count: int = items.size()
	var cols: int = max(3, int(ceil(sqrt(float(count)))))
	var rows: int = int(ceil(float(count) / float(cols)))
	var spacing_x: float = 1.2
	var spacing_z: float = 1.0

	# Compute grid origin centered around centerline
	var total_w: float = float(cols - 1) * spacing_x
	var total_d: float = float(rows - 1) * spacing_z
	var origin: Vector3 = center - right * (total_w * 0.5) - forward * (total_d * 0.5)

	# Stable ordering by ID
	items.sort_custom(func(a, b): return str(a["id"]) < str(b["id"]))

	for i in range(items.size()):
		var col: int = i % cols
		var row: int = i / cols
		var pos: Vector3 = origin + right * (float(col) * spacing_x) + forward * (float(row) * spacing_z)
		var node: Node3D = items[i]["node"]
		# Keep existing height if not zero
		pos.y = node.position.y if abs(node.position.y) > 0.001 else 0.0
		node.position = pos
		# Face camera yaw-only for clear reading
		_yaw_face_camera(node)

func _yaw_face_camera(node: Node3D):
	if camera == null:
		return
	var cam_pos: Vector3 = camera.global_position
	var here: Vector3 = node.global_position
	var to_cam := Vector3(cam_pos.x - here.x, 0.0, cam_pos.z - here.z)
	if to_cam.length() < 0.001:
		return
	to_cam = to_cam.normalized()
	var target := here + to_cam
	node.look_at(target, Vector3.UP)

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

	# After applying changes, tighten taskmanager layout near its center
	reflow_taskmanager_layout()

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
	zones_state = zones
	print("GODOT: Received setup_zones message with ", zones.size(), " zones")
	for zone_name in zones.keys():
		var zone_data = zones[zone_name]
		print("GODOT: Zone '", zone_name, "' - angle: ", zone_data.get("angle", "N/A"), ", radius: ", zone_data.get("radius", "N/A"))
	# Set zone count for sector calculation
	set_meta("zone_count", zones.size())
	print("GODOT: Calling zone_visualizer.draw_zones()")
	zone_visualizer.draw_zones(zones)
	update_zone_hud(zones)
	# With zones established, reflow task grid closer to center
	reflow_taskmanager_layout()
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
		var entry_info: Dictionary = {}

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
		# Orient only non-task meshes toward center; tasks are cards and will face camera later
		if node is MeshInstance3D:
			var is_task := (properties.has("event_type") and str(properties["event_type"]) == "task")
			if not is_task:
				var dir = (Vector3(0, 0, 0) - node.position).normalized()
				var up = Vector3.UP
				var right = dir.cross(up).normalized()
				up = right.cross(dir).normalized()
				node.transform.basis = Basis(right, dir, up)
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
		var base_color = MONOKAI_BLUE
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
		elif (properties.has("event_type") and str(properties["event_type"]) == "task") \
				or properties.has("title") or properties.has("description") or properties.has("status"):
			_augment_task_card(node, properties)

	if properties.has("event_type"):
		node.set_meta("event_type", properties["event_type"])

	# Update task card UI if applicable
	if node.has_meta("task_card"):
		_refresh_task_card(node)

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
		particle_process_material.color = Color(MONOKAI_ORANGE.r, MONOKAI_ORANGE.g, MONOKAI_ORANGE.b, 0.55)	 # Warm amber smoke
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
			# If parent is a task card, suppress floating labels (card renders its own text)
			if node_type == "Label3D" and parent_node.has_meta("task_card"):
				node.visible = false
				node.set_meta("suppressed_by_task_card", true)
			else:
				# Set local position relative to parent (generic behavior)
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

	# Reflow grid to bring task cards near center (no-op if none)
	reflow_taskmanager_layout()

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
			var base_color = MONOKAI_BLUE
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
			elif (properties.has("event_type") and str(properties["event_type"]) == "task") \
					or properties.has("title") or properties.has("description") or properties.has("status"):
				if not node.has_meta("task_card"):
					_augment_task_card(node, properties)

			# Refresh card face if this is a task card
			if node.has_meta("task_card"):
				_refresh_task_card(node)

	# Reflow grid to ensure tasks stay near center
	reflow_taskmanager_layout()


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
					if target_node.has_meta("task_card"):
						_refresh_task_card(target_node)
				show_info_panel(target_node)
		)
		rename_field.text_submitted.connect(func(new_text):
			if dispatch_component_action(node_id, "rename", {"user_input": {"text": new_text}}) and target_node:
				var di = target_node.get_meta("display_info", {})
				if di is Dictionary:
					di["title"] = new_text
					target_node.set_meta("display_info", di)
					if target_node.has_meta("task_card"):
						_refresh_task_card(target_node)
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
					if target_desc_node.has_meta("task_card"):
						_refresh_task_card(target_desc_node)
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

	# Generic delete support
	if actions.has("delete"):
		var delete_btn = Button.new()
		var delete_label := "Delete Entry"
		var props_for_label: Dictionary = entry.get("component_properties", {})
		if props_for_label.has("status") or props_for_label.has("title") or props_for_label.has("description"):
			delete_label = "Delete Task"
		delete_btn.text = delete_label
		_apply_neon_button(delete_btn, 16)
		delete_btn.add_theme_color_override("font_color", Color(1.0, 0.4, 0.5, 1.0))
		delete_btn.pressed.connect(func():
			if dispatch_component_action(node_id, "delete"):
				info_panel.visible = false
		)
		info_actions_container.add_child(delete_btn)

	# Generic edit UI for plugin-spawned entries ("update" action)
	if actions.has("update"):
		var update_action = actions["update"]
		var command_desc: Dictionary = update_action.get("command_descriptor", {}) if typeof(update_action) == TYPE_DICTIONARY else {}
		var args_map: Dictionary = command_desc.get("arguments", {}) if typeof(command_desc) == TYPE_DICTIONARY else {}

		# Section label
		var edit_label = Label.new()
		edit_label.text = ":: edit fields"
		_apply_neon_label(edit_label, 16)
		info_actions_container.add_child(edit_label)

		# Build simple editors for any user_input-bound arguments
		var field_rows: Array = []  # Each: {"path": String, "control": Control, "key": String}
		for arg_name in args_map.keys():
			var binding = args_map[arg_name]
			if typeof(binding) != TYPE_DICTIONARY:
				continue
			if str(binding.get("source", "")) != "user_input":
				continue
			var path := str(binding.get("path", str(arg_name)))
			if path == "":
				path = str(arg_name)
			var last_key := path.split(".")[-1]

			var row = HBoxContainer.new()
			row.add_theme_constant_override("separation", 6)
			var label = Label.new()
			label.text = last_key.capitalize()
			label.custom_minimum_size = Vector2(90, 28)
			_apply_neon_label(label, 14, true)
			row.add_child(label)

			var editor = LineEdit.new()
			editor.size_flags_horizontal = Control.SIZE_EXPAND_FILL
			# Pre-populate using component properties when possible
			var comp_props: Dictionary = entry.get("component_properties", {})
			var initial = _lookup_path(comp_props, path)
			if initial == null and comp_props.has(last_key):
				initial = comp_props[last_key]
			editor.text = str(initial) if initial != null else ""
			# Pause movement while editing
			editor.focus_entered.connect(Callable(self, "_on_text_input_focus_entered"))
			editor.focus_exited.connect(Callable(self, "_on_text_input_focus_exited"))
			_apply_neon_line_edit(editor, 14)
			row.add_child(editor)

			info_actions_container.add_child(row)
			field_rows.append({
				"path": path,
				"control": editor,
				"key": last_key,
			})

		# Action buttons
		var btn_row = HBoxContainer.new()
		btn_row.add_theme_constant_override("separation", 10)
		var apply_btn = Button.new()
		apply_btn.text = "Save Changes"
		_apply_neon_button(apply_btn, 14)
		apply_btn.pressed.connect(func():
				var user_input: Dictionary = {}
				for item in field_rows:
					var ctrl: LineEdit = item["control"]
					var p: String = item["path"]
					_set_nested_path(user_input, p, ctrl.text)
				# Dispatch update
				if dispatch_component_action(node_id, "update", {"user_input": user_input}):
					# Optimistically update local properties for immediate feedback
					var props: Dictionary = entry.get("component_properties", {})
					for item in field_rows:
						var key: String = item["key"]
						var ctrl: LineEdit = item["control"]
						props[key] = ctrl.text
					entry["component_properties"] = props
					var target_node: Node = entry.get("node", null)
					if target_node and target_node.has_meta("task_card"):
						_refresh_task_card(target_node)
					# Re-show to reflect updated info
					var target: Node = entry.get("node", null)
					if target:
						show_info_panel(target)
		)

		var clear_btn = Button.new()
		clear_btn.text = "Clear Fields"
		_apply_neon_button(clear_btn, 14)
		clear_btn.pressed.connect(func():
				var user_input: Dictionary = {}
				for item in field_rows:
					var p: String = item["path"]
					_set_nested_path(user_input, p, "")
					var ctrl: LineEdit = item["control"]
					ctrl.text = ""
				if dispatch_component_action(node_id, "update", {"user_input": user_input}):
					var props: Dictionary = entry.get("component_properties", {})
					for item in field_rows:
						var key: String = item["key"]
						props.erase(key)
					entry["component_properties"] = props
		)

		btn_row.add_child(apply_btn)
		btn_row.add_child(clear_btn)
		info_actions_container.add_child(btn_row)

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

	var plugin_button = Button.new()
	plugin_button.text = "forge plugin"
	plugin_button.tooltip_text = "Launch the interview to scaffold a new MindPalace plugin."
	plugin_button.size = Vector2(220, 42)
	plugin_button.connect("pressed", Callable(self, "_on_open_plugin_interview"))
	_apply_neon_button(plugin_button, 16)
	actions_vbox.add_child(plugin_button)

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
			env.background_color = MONOKAI_BASE_DEEP
			env.fog_enabled = true
			env.fog_color = Color(MONOKAI_BASE.r, MONOKAI_BASE.g, MONOKAI_BASE.b, 0.95)
			env.fog_density = 0.015
			env.volumetric_fog_density = 0.025
			env.glow_intensity = 0.95
			env.ambient_light_energy = 0.24
		else:
			env.background_mode = Environment.BG_COLOR
			env.background_color = Color(0.22, 0.17, 0.13, 1.0)
			env.fog_enabled = true
			env.fog_color = Color(0.32, 0.24, 0.18, 0.6)
			env.fog_density = 0.006
			env.volumetric_fog_density = 0.015
			env.glow_intensity = 0.75
			env.ambient_light_energy = 0.42

	if fog_density_slider:
		fog_density_slider.value = env.fog_density if env else fog_density_slider.value
	if volumetric_fog_slider:
		volumetric_fog_slider.value = env.volumetric_fog_density if env else volumetric_fog_slider.value
	if ambient_light_slider:
		ambient_light_slider.value = env.ambient_light_energy if env else ambient_light_slider.value

	if sun_light:
		sun_light.light_color = MONOKAI_YELLOW if dark_mode else MONOKAI_ORANGE
		sun_light.light_energy = 2.0 if dark_mode else 2.6
	if core_light:
		core_light.light_color = MONOKAI_PINK if dark_mode else MONOKAI_GREEN
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

func _on_open_plugin_interview():
	if plugin_interview_flow:
		plugin_interview_flow.open()

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

# Plugin interview workflow
class PluginInterviewFlow extends RefCounted:
	var owner: Node3D
	var layer: CanvasLayer = null
	var panel: Panel = null
	var content: VBoxContainer = null
	var step_label: Label = null
	var error_label: Label = null
	var back_button: Button = null
	var next_button: Button = null
	var inputs := {}
	var field_rows: Array = []
	var command_checks = {}
	var state: Dictionary = {}
	var step_index: int = 0

	func _init(owner: Node3D) -> void:
		self.owner = owner

	func is_open() -> bool:
		return layer != null

	func open():
		if is_open():
			return
		state = {
			"plugin_name": "",
			"description": "",
			"entity_name": "",
			"fields": [],
			"commands": {
				"create": true,
				"list": true,
				"update": false,
				"delete": false,
			},
		}
		field_rows.clear()
		command_checks.clear()
		inputs.clear()
		step_index = 0

		layer = CanvasLayer.new()
		owner.add_child(layer)

		var bg = ColorRect.new()
		bg.size = owner.get_viewport().size
		bg.color = Color(0, 0, 0, 0.65)
		layer.add_child(bg)

		panel = Panel.new()
		panel.custom_minimum_size = Vector2(680, 540)
		panel.size = panel.custom_minimum_size
		panel.position = (owner.get_viewport().size - panel.size) * 0.5
		owner._apply_neon_panel(panel, UI_PANEL_BORDER, UI_BG_DEEP, 3, 12)
		layer.add_child(panel)

		var title_label = Label.new()
		title_label.text = "[PLUGIN FORGE]"
		title_label.position = Vector2(20, 16)
		title_label.size = Vector2(panel.size.x - 40, 28)
		owner._apply_neon_label(title_label, 24)
		panel.add_child(title_label)

		var close_button = Button.new()
		close_button.text = "close"
		close_button.size = Vector2(80, 28)
		close_button.position = Vector2(panel.size.x - close_button.size.x - 20, 16)
		close_button.connect("pressed", Callable(self, "close"))
		owner._apply_neon_button(close_button, 14)
		panel.add_child(close_button)

		step_label = Label.new()
		step_label.position = Vector2(20, 54)
		step_label.size = Vector2(panel.size.x - 40, 24)
		owner._apply_neon_label(step_label, 18, true)
		panel.add_child(step_label)

		var scroll = ScrollContainer.new()
		scroll.position = Vector2(20, 84)
		scroll.size = Vector2(panel.size.x - 40, panel.size.y - 190)
		scroll.horizontal_scroll_mode = ScrollContainer.SCROLL_MODE_DISABLED
		panel.add_child(scroll)

		content = VBoxContainer.new()
		content.size_flags_horizontal = Control.SIZE_EXPAND_FILL
		content.add_theme_constant_override("separation", 12)
		scroll.add_child(content)

		error_label = Label.new()
		error_label.position = Vector2(20, panel.size.y - 96)
		error_label.size = Vector2(panel.size.x - 40, 20)
		error_label.horizontal_alignment = HORIZONTAL_ALIGNMENT_LEFT
		error_label.add_theme_color_override("font_color", Color(1.0, 0.42, 0.42, 1.0))
		owner._apply_neon_label(error_label, 14, true)
		error_label.text = ""
		panel.add_child(error_label)

		var button_bar = HBoxContainer.new()
		button_bar.position = Vector2(20, panel.size.y - 64)
		button_bar.size = Vector2(panel.size.x - 40, 36)
		button_bar.add_theme_constant_override("separation", 12)
		panel.add_child(button_bar)

		back_button = Button.new()
		back_button.text = "cancel"
		back_button.size_flags_horizontal = Control.SIZE_EXPAND_FILL
		back_button.connect("pressed", Callable(self, "handle_back"))
		owner._apply_neon_button(back_button, 16)
		button_bar.add_child(back_button)

		next_button = Button.new()
		next_button.text = "next"
		next_button.size_flags_horizontal = Control.SIZE_EXPAND_FILL
		next_button.connect("pressed", Callable(self, "handle_next"))
		owner._apply_neon_button(next_button, 16)
		button_bar.add_child(next_button)

		var player = _player_node()
		if player:
			player.movement_disabled = true
		Input.mouse_mode = Input.MOUSE_MODE_VISIBLE

		render_step()

	func close():
		if layer:
			layer.queue_free()
		layer = null
		panel = null
		content = null
		step_label = null
		error_label = null
		back_button = null
		next_button = null
		inputs.clear()
		field_rows.clear()
		command_checks.clear()
		var player = _player_node()
		if player:
			player.movement_disabled = owner.settings_visible
		if not owner.settings_visible and owner.model_picker_layer == null:
			Input.mouse_mode = Input.MOUSE_MODE_CAPTURED

	func handle_back():
		if not panel:
			return
		if step_index == 0:
			close()
			return
		step_index = max(step_index - 1, 0)
		render_step()

	func handle_next():
		if not panel:
			return
		if not capture_step():
			return
		if step_index >= PLUGIN_INTERVIEW_STEPS.size() - 1:
			submit()
			return
		step_index += 1
		render_step()

	func render_step():
		if not content:
			return
		for child in content.get_children():
			child.queue_free()
		inputs.clear()
		field_rows.clear()
		command_checks.clear()
		error_label.text = ""

		var step: Dictionary = PLUGIN_INTERVIEW_STEPS[step_index]
		var step_title: String = step.get("title", "Step")
		step_label.text = "Step %d / %d — %s" % [
			step_index + 1,
			PLUGIN_INTERVIEW_STEPS.size(),
			step_title,
		]

		var prompt = Label.new()
		prompt.text = step.get("prompt", "")
		prompt.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
		owner._apply_neon_label(prompt, 16)
		content.add_child(prompt)

		match step.get("id", ""):
			"plugin_name":
				var input = LineEdit.new()
				input.placeholder_text = "e.g., dieseltracker"
				input.text = state.get("plugin_name", "")
				input.focus_entered.connect(Callable(owner, "_on_text_input_focus_entered"))
				input.focus_exited.connect(Callable(owner, "_on_text_input_focus_exited"))
				owner._apply_neon_line_edit(input, 18)
				content.add_child(input)
				inputs["plugin_name"] = input
				input.grab_focus()
			"description":
				var text_edit = TextEdit.new()
				text_edit.custom_minimum_size = Vector2(0, 160)
				text_edit.text = state.get("description", "")
				text_edit.wrap_mode = TextEdit.LINE_WRAPPING_BOUNDARY
				text_edit.focus_entered.connect(Callable(owner, "_on_text_input_focus_entered"))
				text_edit.focus_exited.connect(Callable(owner, "_on_text_input_focus_exited"))
				owner._apply_neon_line_edit(text_edit, 16)
				content.add_child(text_edit)
				inputs["description"] = text_edit
				text_edit.grab_focus()
			"entity_name":
				var entity_input = LineEdit.new()
				entity_input.placeholder_text = "e.g., FuelLog"
				entity_input.text = state.get("entity_name", "")
				entity_input.focus_entered.connect(Callable(owner, "_on_text_input_focus_entered"))
				entity_input.focus_exited.connect(Callable(owner, "_on_text_input_focus_exited"))
				owner._apply_neon_line_edit(entity_input, 18)
				content.add_child(entity_input)
				inputs["entity_name"] = entity_input
				entity_input.grab_focus()
			"fields":
				var helper = Label.new()
				helper.text = "Add one row per attribute. We'll automatically add the unique ID."
				helper.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
				owner._apply_neon_label(helper, 14, true)
				content.add_child(helper)

				var fields_container = VBoxContainer.new()
				fields_container.add_theme_constant_override("separation", 8)
				content.add_child(fields_container)

				var saved_fields: Array = state.get("fields", [])
				if saved_fields.is_empty():
					_add_field_row({}, fields_container)
				else:
					for field_data in saved_fields:
						_add_field_row(field_data, fields_container)

				var add_button = Button.new()
				add_button.text = "add another field"
				add_button.connect("pressed", Callable(self, "_on_field_add_pressed").bind(fields_container))
				owner._apply_neon_button(add_button, 14)
				content.add_child(add_button)
			"commands":
				var cmd_helper = Label.new()
				cmd_helper.text = "Select the operations MindPalace should scaffold."
				cmd_helper.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
				owner._apply_neon_label(cmd_helper, 14, true)
				content.add_child(cmd_helper)
				var grid = VBoxContainer.new()
				grid.add_theme_constant_override("separation", 6)
				content.add_child(grid)
				var defaults: Dictionary = state.get("commands", {})
				var command_labels = {
					"create": "Create entries",
					"update": "Update entries",
					"delete": "Delete entries",
					"list": "List entries",
				}
				for action in ["create", "update", "delete", "list"]:
					var check = CheckButton.new()
					check.text = command_labels.get(action, action)
					check.button_pressed = defaults.get(action, action in ["create", "list"])
					grid.add_child(check)
					command_checks[action] = check
			"review":
				var blueprint = build_blueprint()
				if not blueprint.get("success", false):
					error_label.text = blueprint.get("error", "Fix earlier steps.")
				else:
					var summary = RichTextLabel.new()
					summary.bbcode_enabled = true
					summary.fit_content = true
					summary.scroll_active = false
					var payload: Dictionary = blueprint.get("payload", {})
					var entities: Array = payload.get("entities", [])
					var entity_info: Dictionary = {}
					if entities.size() > 0:
						entity_info = entities[0]
					var fields: Array = entity_info.get("fields", [])
					var command_list: Array = payload.get("commands", [])
					var text := "[b]Plugin[/b]: %s\n[b]Entity[/b]: %s\n\n[b]Fields[/b]:\n" % [
						payload.get("name", ""),
						entity_info.get("name", ""),
					]
					for field in fields:
						text += " • %s (%s)\n" % [field.get("name", ""), field.get("type", "string")]
					text += "\n[b]Commands[/b]:\n"
					for cmd in command_list:
						text += " • %s (%s)\n" % [cmd.get("name", ""), cmd.get("action", "")]
					summary.text = text
					summary.add_theme_color_override("default_color", UI_TEXT)
					content.add_child(summary)
		back_button.text = "cancel" if step_index == 0 else "back"
		if step_index == PLUGIN_INTERVIEW_STEPS.size() - 1:
			next_button.text = "generate plugin"
		else:
			next_button.text = "next"

	func capture_step() -> bool:
		var step_id: String = PLUGIN_INTERVIEW_STEPS[step_index].get("id", "")
		match step_id:
			"plugin_name":
				var input: LineEdit = inputs.get("plugin_name", null)
				if input == null:
					return false
				var text = input.text.strip_edges()
				if text == "":
					error_label.text = "Name your plugin before proceeding."
					return false
				state["plugin_name"] = text
			"description":
				var text_edit: TextEdit = inputs.get("description", null)
				if text_edit == null:
					return false
				var desc = text_edit.text.strip_edges()
				if desc == "":
					error_label.text = "Describe what this plugin should achieve."
					return false
				state["description"] = desc
			"entity_name":
				var entity_input: LineEdit = inputs.get("entity_name", null)
				if entity_input == null:
					return false
				var entity_text = entity_input.text.strip_edges()
				if entity_text == "":
					error_label.text = "Give the entity a name (e.g., FuelLog)."
					return false
				state["entity_name"] = entity_text
			"fields":
				var field_result = _gather_field_rows()
				if field_result.get("error", "") != "":
					error_label.text = field_result["error"]
					return false
				state["fields"] = field_result.get("fields", [])
			"commands":
				var selected: Dictionary = {}
				var any_selected := false
				for action in command_checks.keys():
					var check: CheckButton = command_checks[action]
					var pressed = check and check.button_pressed
					selected[action] = pressed
					if pressed:
						any_selected = true
				if not any_selected:
					error_label.text = "Select at least one command to generate."
					return false
				state["commands"] = selected
		return true

	func _on_field_add_pressed(container: VBoxContainer):
		_add_field_row({}, container)

	func _add_field_row(initial_data: Dictionary, container: VBoxContainer):
		var row = HBoxContainer.new()
		row.add_theme_constant_override("separation", 10)
		container.add_child(row)

		var name_input = LineEdit.new()
		name_input.placeholder_text = "Field name (e.g., Amount)"
		name_input.size_flags_horizontal = Control.SIZE_EXPAND_FILL
		name_input.text = initial_data.get("label", "")
		name_input.focus_entered.connect(Callable(owner, "_on_text_input_focus_entered"))
		name_input.focus_exited.connect(Callable(owner, "_on_text_input_focus_exited"))
		owner._apply_neon_line_edit(name_input, 14)
		row.add_child(name_input)

		var type_option = OptionButton.new()
		type_option.size = Vector2(140, 32)
		for type_data in PLUGIN_FIELD_TYPES:
			var idx = type_option.get_item_count()
			type_option.add_item(type_data.get("label", ""), idx)
			type_option.set_item_metadata(idx, type_data.get("go", "string"))
		var selected_go = initial_data.get("type", "string")
		for idx in range(type_option.get_item_count()):
			if type_option.get_item_metadata(idx) == selected_go:
				type_option.select(idx)
				break
		row.add_child(type_option)

		var remove_button = Button.new()
		remove_button.text = "x"
		remove_button.tooltip_text = "Remove this field"
		remove_button.connect("pressed", Callable(self, "_remove_field_row").bind(row, container))
		owner._apply_neon_button(remove_button, 12)
		row.add_child(remove_button)

		field_rows.append({
			"row": row,
			"name_input": name_input,
			"type_option": type_option,
		})

	func _remove_field_row(row: HBoxContainer, container: VBoxContainer):
		for i in range(field_rows.size()):
			var entry: Dictionary = field_rows[i]
			if entry.get("row") == row:
				field_rows.remove_at(i)
				break
		row.queue_free()
		if field_rows.is_empty():
			_add_field_row({}, container)

	func _gather_field_rows() -> Dictionary:
		var result := {
			"fields": [],
			"error": "",
		}
		var used_names: Dictionary = {}
		for entry in field_rows:
			var name_input: LineEdit = entry.get("name_input")
			if name_input == null:
				continue
			var raw_name = name_input.text.strip_edges()
			if raw_name == "":
				continue
			var go_name = _to_pascal_case(raw_name)
			if go_name == "":
				result["error"] = "Field names must include letters."
				return result
			if used_names.has(go_name):
				result["error"] = "Duplicate field '%s' detected." % go_name
				return result
			used_names[go_name] = true
			var type_option: OptionButton = entry.get("type_option")
			var go_type = "string"
			if type_option:
				var idx = type_option.get_selected_id()
				go_type = type_option.get_item_metadata(idx)
			var json_tag = _to_snake_case(go_name)
			result["fields"].append({
				"label": raw_name,
				"go_name": go_name,
				"json": json_tag,
				"type": go_type,
			})
		if result["fields"].is_empty():
			result["error"] = "Add at least one field so the plugin tracks meaningful data."
		return result

	func build_blueprint() -> Dictionary:
		var response := {
			"success": false,
			"error": "",
			"payload": {},
		}
		var plugin_slug = _sanitize_plugin_name(state.get("plugin_name", ""))
		if plugin_slug == "":
			response["error"] = "Plugin name must contain letters or underscores."
			return response
		var entity_name = _to_pascal_case(state.get("entity_name", ""))
		if entity_name == "":
			response["error"] = "Provide a primary entity name."
			return response
		var entity_snake = _to_snake_case(entity_name)
		var fields: Array = state.get("fields", [])
		if fields.is_empty():
			response["error"] = "Add at least one field for the entity."
			return response
		var commands_cfg: Dictionary = state.get("commands", {})
		var command_names = []
		for action in commands_cfg.keys():
			if commands_cfg[action]:
				command_names.append(action)
		if command_names.is_empty():
			response["error"] = "Select at least one command."
			return response

		var entity_fields: Array = []
		entity_fields.append({
			"name": "%sID" % entity_name,
			"type": "string",
			"json": "%s_id" % entity_snake,
		})
		for field in fields:
			entity_fields.append({
				"name": field.get("go_name", ""),
				"type": field.get("type", "string"),
				"json": field.get("json", _to_snake_case(field.get("go_name", ""))),
			})

		var commands: Array = []
		if commands_cfg.get("create", false):
			var inputs: Array = []
			for i in range(1, entity_fields.size()):
				inputs.append(entity_fields[i].duplicate(true))
			commands.append({
				"name": "Create%s" % entity_name,
				"action": "create",
				"input": inputs,
			})
		if commands_cfg.get("update", false):
			var update_inputs: Array = []
			for field_dict in entity_fields:
				update_inputs.append(field_dict.duplicate(true))
			commands.append({
				"name": "Update%s" % entity_name,
				"action": "update",
				"input": update_inputs,
			})
		if commands_cfg.get("delete", false):
			var delete_inputs: Array = [entity_fields[0].duplicate(true)]
			commands.append({
				"name": "Delete%s" % entity_name,
				"action": "delete",
				"input": delete_inputs,
			})
		if commands_cfg.get("list", false):
			var plural = entity_name + "s"
			commands.append({
				"name": "List%s" % plural,
				"action": "list",
				"input": [],
			})

		var payload = {
			"name": plugin_slug,
			"description": state.get("description", ""),
			"entities": [
				{
					"name": entity_name,
					"fields": entity_fields,
				},
			],
			"commands": commands,
		}
		response["success"] = true
		response["payload"] = payload
		return response

	func submit():
		var blueprint = build_blueprint()
		if not blueprint.get("success", false):
			error_label.text = blueprint.get("error", "Unable to build plugin blueprint.")
			return
		var payload: Dictionary = {
			"Description": state.get("description", ""),
			"Requirements": blueprint.get("payload", {}),
		}
		if owner.send_ui_command("GeneratePlugin", payload):
			owner.log_message("Plugin forge dispatched for '%s'." % payload["Requirements"].get("name", "plugin"))
			close()
		else:
			error_label.text = "Failed to reach the backend. Ensure the websocket is connected."

	func _sanitize_plugin_name(input: String) -> String:
		var text = input.strip_edges()
		var builder = ""
		for i in text.length():
			var ch = text[i]
			if ch == " " or ch == "-":
				if builder != "" and not builder.ends_with("_"):
					builder += "_"
				continue
			if ch == "_":
				if builder != "" and not builder.ends_with("_"):
					builder += "_"
				continue
			var code = ch.unicode_at(0)
			var is_letter = (code >= 65 and code <= 90) or (code >= 97 and code <= 122)
			if is_letter:
				builder += ch.to_lower()
				continue
			var is_digit = code >= 48 and code <= 57
			if is_digit and builder != "":
				builder += ch
		while builder.begins_with("_") and builder.length() > 0:
			builder = builder.substr(1, builder.length() - 1)
		while builder.ends_with("_") and builder.length() > 0:
			builder = builder.substr(0, builder.length() - 1)
		return builder

	func _to_pascal_case(text: String) -> String:
		var cleaned = text.strip_edges()
		if cleaned == "":
			return ""
		cleaned = cleaned.replace("-", " ")
		cleaned = cleaned.replace("_", " ")
		var parts: PackedStringArray = cleaned.split(" ", false)
		var result = ""
		for part in parts:
			if part == "":
				continue
			var lower = part.to_lower()
			if lower.length() == 1:
				result += lower.to_upper()
			else:
				result += lower.substr(0, 1).to_upper() + lower.substr(1, lower.length() - 1)
		return result

	func _to_snake_case(text: String) -> String:
		var cleaned = text.strip_edges()
		if cleaned == "":
			return ""
		var result = ""
		for i in cleaned.length():
			var ch = cleaned[i]
			if ch == " " or ch == "-":
				if result != "" and not result.ends_with("_"):
					result += "_"
				continue
			if ch == "_":
				if result != "" and not result.ends_with("_"):
					result += "_"
				continue
			var code = ch.unicode_at(0)
			var is_upper = code >= 65 and code <= 90
			var is_lower = code >= 97 and code <= 122
			var is_digit = code >= 48 and code <= 57
			if is_upper:
				if result != "" and not result.ends_with("_"):
					result += "_"
				result += ch.to_lower()
			elif is_lower or is_digit:
				result += ch.to_lower()
		while result.begins_with("_"):
			result = result.substr(1, result.length() - 1)
		while result.ends_with("_"):
			result = result.substr(0, result.length() - 1)
		return result

	func _player_node() -> Node:
		return owner.get_node_or_null(NodePath("Player"))

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

	# Gentle float and shimmer: small vertical bob + light energy pulse
	var base_pos: Vector3 = orchestrator.position
	var up = base_pos + Vector3(0, 0.18, 0)
	var down = base_pos + Vector3(0, -0.12, 0)

	# Animate position in a loop
	orchestrator_tween.tween_property(orchestrator, "position", up, 1.6).set_trans(Tween.TRANS_SINE).set_ease(Tween.EASE_IN_OUT)
	orchestrator_tween.tween_property(orchestrator, "position", down, 1.6).set_trans(Tween.TRANS_SINE).set_ease(Tween.EASE_IN_OUT)
	orchestrator_tween.tween_property(orchestrator, "position", base_pos, 1.6).set_trans(Tween.TRANS_SINE).set_ease(Tween.EASE_IN_OUT)

	# In parallel, softly pulse the light energy
	var light: OmniLight3D = orchestrator.get_node_or_null("OrchestratorLight")
	if light:
		orchestrator_tween.parallel().tween_property(light, "light_energy", 16.0, 1.6).set_trans(Tween.TRANS_SINE).set_ease(Tween.EASE_IN_OUT)
		orchestrator_tween.parallel().tween_property(light, "light_energy", 12.0, 1.6).set_trans(Tween.TRANS_SINE).set_ease(Tween.EASE_IN_OUT)
		orchestrator_tween.parallel().tween_property(light, "light_energy", 14.0, 1.6).set_trans(Tween.TRANS_SINE).set_ease(Tween.EASE_IN_OUT)

	# When finished, restart to keep the shimmer perpetual
	orchestrator_tween.finished.connect(func():
		start_orchestrator_animation()
	)
