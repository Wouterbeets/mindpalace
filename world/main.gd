extends Node3D

@onready var mesh_instance: MeshInstance3D = $Floor/MeshInstance3D
@onready var camera: Camera3D = $Player/Camera

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
	"user_request_received": Color.WHITE,
	"task_created": Color.BLUE,
	"task_updated": Color.YELLOW,
	"task_completed": Color.GREEN,
	"task_deleted": Color.RED,
	"task": Color.BLUE,	 # For full state tasks
	"note_created": Color.LIME,
	"note_updated": Color.LIME_GREEN,
	"note_deleted": Color.DARK_GREEN,
	"note": Color.LIME,	 # For full state notes
	"calendar_event_created": Color.PINK,
	"calendar_event_updated": Color.HOT_PINK,
	"calendar_event_deleted": Color.DEEP_PINK,
	"calendar_event": Color.PINK,  # For full state calendar events
	"plugin_generated": Color.PURPLE,
	"request_completed": Color.TEAL,
	"agent_call_decided": Color.MAGENTA,
	"agent_execution_failed": Color.DARK_RED,
	"tool_call_failed": Color.DARK_ORANGE,
	"tool_call_started": Color.ORANGE,
	"tool_call_completed": Color.CYAN,
	"orchestrator_ai": Color.GOLD,
}

# Store cubes by event ID for updates/deletes
var event_cubes = {}

# UI for info panel
var info_panel: Panel
var info_label: Label

# Targeting HUD
var targeting_hud_panel: Panel
var targeting_hud_label: Label
var targeted_object = null
var targeting_reticle: ColorRect



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
var world_env = null
var env = null
var ambient_particles = null

# Underground scene for event animations
var underground_node = null

# Splash screen
var splash_finished = false

# Orchestrator AI animation
var orchestrator_tween = null
var orchestrator_animated = false

# Birdview camera mode
var birdview_active = false
# var birdview_camera_pos = Vector3(0, 80, 0)
# var birdview_camera_rot = Vector3(-PI/3, 0, 0)  # 60 degrees down
# var tween = null

# Orchestrator menu
var orchestrator_menu = null



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



	# Set up info panel
	var canvas_layer = CanvasLayer.new()
	add_child(canvas_layer)
	info_panel = Panel.new()
	info_panel.size = Vector2(300, 200)
	info_panel.position = Vector2(10, 10)
	info_panel.visible = false
	canvas_layer.add_child(info_panel)

	info_label = Label.new()
	info_label.position = Vector2(10, 10)
	info_label.size = Vector2(280, 180)
	info_label.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	info_panel.add_child(info_label)

	# Set up targeting HUD (top-right)
	targeting_hud_panel = Panel.new()
	targeting_hud_panel.size = Vector2(400, 300)
	targeting_hud_panel.position = Vector2(get_viewport().size.x - 410, 10)

	# Create a proper dark background using StyleBoxFlat
	var style_box = StyleBoxFlat.new()
	style_box.bg_color = Color(0.1, 0.1, 0.1, 0.95)	 # Dark grey, very opaque
	targeting_hud_panel.add_theme_stylebox_override("panel", style_box)

	targeting_hud_panel.visible = false
	canvas_layer.add_child(targeting_hud_panel)

	targeting_hud_label = Label.new()
	targeting_hud_label.position = Vector2(10, 10)
	targeting_hud_label.size = Vector2(380, 280)
	targeting_hud_label.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	# Set font color directly for better contrast
	targeting_hud_label.add_theme_color_override("font_color", Color(1, 1, 1, 1))  # White text
	targeting_hud_panel.add_child(targeting_hud_label)

	# Add targeting reticle (center of screen)
	targeting_reticle = ColorRect.new()
	targeting_reticle.size = Vector2(6, 6)
	update_reticle_position()
	targeting_reticle.color = Color.WHITE
	canvas_layer.add_child(targeting_reticle)

	# Connect to viewport size changes
	get_viewport().size_changed.connect(_on_viewport_size_changed)

	# Use existing WorldEnvironment from scene
	env = $WorldEnvironment.environment
	env.fog_enabled = false	 # Disable fog to see test cube

	# Add directional light
	var dir_light = DirectionalLight3D.new()
	dir_light.rotation_degrees = Vector3(-30, 45, 0)
	dir_light.light_color = Color(1, 1, 0.9)
	dir_light.light_energy = 1.0
	add_child(dir_light)

	# Add ambient particle field
	ambient_particles = GPUParticles3D.new()
	var ambient_material = ParticleProcessMaterial.new()
	ambient_material.emission_shape = ParticleProcessMaterial.EMISSION_SHAPE_BOX
	ambient_material.emission_box_extents = Vector3(50, 20, 50)
	ambient_material.color = Color(0.5, 0.5, 1, 0.1)
	ambient_material.gravity = Vector3(0, -0.1, 0)
	ambient_particles.process_material = ambient_material
	ambient_particles.amount = 50
	ambient_particles.lifetime = 10.0
	add_child(ambient_particles)

	# Add transcription display
	var transcription_label = Label3D.new()
	transcription_label.name = "transcription_display"
	transcription_label.text = "🎤 Voice transcription will appear here when you speak to the orchestrator..."
	transcription_label.position = Vector3(0, 2, -3)
	transcription_label.font_size = 64
	transcription_label.modulate = Color(1, 1, 0)  # Yellow text
	transcription_label.outline_modulate = Color(0, 0, 0)  # Black outline
	transcription_label.outline_size = 4
	add_child(transcription_label)

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
var dragged_object = null
var dragged_node_id = ""
var is_dragging = false
var drag_plane_normal = Vector3(0, 1, 0)  # Drag along XZ plane
var drag_offset = Vector3()

func _input(event):
	if not settings_visible:
		if event is InputEventMouseButton:
			if event.button_index == MOUSE_BUTTON_LEFT:
				if event.pressed:
					# Mouse button pressed - start drag immediately
					start_drag(event.position)
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

		if event is InputEventMouseButton and event.pressed and event.button_index == MOUSE_BUTTON_LEFT and not mouse_pressed:
			var mouse_pos = event.position
			var ray_origin = camera.project_ray_origin(mouse_pos)
			var ray_dir = camera.project_ray_normal(mouse_pos)
			var ray_length = 1000.0

			var space_state = get_world_3d().direct_space_state
			var query = PhysicsRayQueryParameters3D.create(ray_origin, ray_origin + ray_dir * ray_length)
			var result = space_state.intersect_ray(query)

			if result:
				var clicked_node = result.collider
				while clicked_node and not event_cubes.values().any(func(v): return v.get("node", null) == clicked_node):
					clicked_node = clicked_node.get_parent()
				if clicked_node:
					if clicked_node.name == "orchestrator_ai":
						show_orchestrator_menu(mouse_pos)
					else:
						show_info_panel(clicked_node)

	# Handle Tab key for settings menu
	if event is InputEventKey and event.keycode == KEY_TAB and event.pressed:
		toggle_settings_menu()

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
				"platform": "godot"
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
	if not data.has("type") or (data["type"] != "delta" and data["type"] != "signal"):
		print("GODOT: Invalid or missing type, returning")
		return
	if not data.has("actions") or typeof(data["actions"]) != TYPE_ARRAY:
		print("GODOT: Invalid or missing actions, returning")
		return
	print("GODOT: Received event message with " + str(data["actions"].size()) + " actions")
	# Apply actions to underground for animations
	for action in data["actions"]:
		handle_underground_action(action)
	# Process create actions for above ground objects
	for action in data["actions"]:
		if action.get("type", "") == "create":
			create_node(action["node_id"], action.get("node_type", "MeshInstance3D"), action.get("properties", {}))

	# Send ACK if sequence_id is present
	print("GODOT: Checking for sequence_id in data: " + str(data.has("sequence_id")))
	if data.has("sequence_id"):
		send_delta_ack(data["sequence_id"])
		print("GODOT: Sent ACK for sequence " + str(data["sequence_id"]))

func process_transcription_update(data: Dictionary):
	if not data.has("actions") or typeof(data["actions"]) != TYPE_ARRAY:
		push_error("Invalid transcription_update message: missing or invalid 'actions' field")
		return
	print("GODOT: Processing transcription update with " + str(data["actions"].size()) + " actions")
	for action in data["actions"]:
		handle_underground_action(action)

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
	var result = space_state.intersect_ray(query)

	if result:
		var clicked_node = result.collider
		while clicked_node and not (clicked_node in event_cubes.values()):
			clicked_node = clicked_node.get_parent()
		if clicked_node:
			show_info_panel(clicked_node)

func start_drag(mouse_pos: Vector2):
	# Try to find object to drag
	var ray_origin = camera.project_ray_origin(mouse_pos)
	var ray_dir = camera.project_ray_normal(mouse_pos)
	var ray_length = 1000.0

	var space_state = get_world_3d().direct_space_state
	var query = PhysicsRayQueryParameters3D.create(ray_origin, ray_origin + ray_dir * ray_length)
	var result = space_state.intersect_ray(query)

	if result:
		var clicked_node = result.collider

		while clicked_node and not (clicked_node in event_cubes.values()):
			clicked_node = clicked_node.get_parent()

		if clicked_node:
			dragged_object = clicked_node
			# Find the node_id
			for id in event_cubes:
				if event_cubes[id].get("node", null) == clicked_node:
					dragged_node_id = id
					break
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
	if dragged_object and is_dragging:
		# Send position update event to backend
		if dragged_node_id != "":
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

	# Special handling for transcription updates
	if node_id == "transcription_display" and action_type == "update" and properties.has("text"):
		print("GODOT: Updating transcription_display with text: '" + properties["text"] + "'")
		var transcription_node = get_node_or_null("transcription_display")
		if transcription_node:
			transcription_node.text = format_with_line_breaks(properties["text"], 80)
			print("GODOT: Updated 3D transcription label")
		else:
			print("GODOT: transcription_display node not found")
		if user_request_input:
			user_request_input.text = properties["text"]
			print("GODOT: Updated user_request_input textbox")
		else:
			print("GODOT: user_request_input is null")
		return



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

	# If backend provides a specific position, use Y only and add to computed XZ (for height variations)
	log_message("About to place node " + node_id + " at position from properties: " + str(properties.get("position", "no pos")))
	if properties.has("position") and properties["position"] is Array and properties["position"].size() >= 3:
		var backend_pos = properties["position"]
		node.position.x = clamp(float(backend_pos[0]), -1000.0, 1000.0)
		node.position.y = clamp(float(backend_pos[1]), -1000.0, 1000.0)
		node.position.z = clamp(float(backend_pos[2]), -1000.0, 1000.0)
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
	
	if properties.has("text"):
		if node is Label3D:
			node.text = properties["text"]
			# Set defaults for readability
			node.add_theme_font_size_override("font_size", 64)
			node.outline_size = 4
			node.outline_modulate = Color.BLACK
			if properties.has("event_type") and EVENT_COLORS.has(properties["event_type"]):
				node.modulate = EVENT_COLORS[properties["event_type"]]
	
	# Add material if color specified (only for MeshInstance3D)
	if node is MeshInstance3D:
		var material = StandardMaterial3D.new()
		var has_color = false
		if properties.has("event_type") and EVENT_COLORS.has(properties["event_type"]):
			material.albedo_color = EVENT_COLORS[properties["event_type"]]
			has_color = true
		elif properties.has("color"):
			var c = properties["color"]
			if c is Array and c.size() >= 3:
				var r = clamp(float(c[0]), 0.0, 1.0)
				var g = clamp(float(c[1]), 0.0, 1.0)
				var b = clamp(float(c[2]), 0.0, 1.0)
				var a = 1.0
				if c.size() > 3:
					a = clamp(float(c[3]), 0.0, 1.0)
				material.albedo_color = Color(r, g, b, a)
				has_color = true
		if properties.has("material_override"):
			var mo = properties["material_override"]
			if mo is Dictionary and mo.has("albedo_color"):
				var c = mo["albedo_color"]
				if c is Array and c.size() >= 3:
					var r = clamp(float(c[0]), 0.0, 1.0)
					var g = clamp(float(c[1]), 0.0, 1.0)
					var b = clamp(float(c[2]), 0.0, 1.0)
					var a = 1.0
					if c.size() > 3:
						a = clamp(float(c[3]), 0.0, 1.0)
					material.albedo_color = Color(r, g, b, a)
					has_color = true
		if properties.has("emissive_color"):
			var ec = properties["emissive_color"]
			if ec is Array and ec.size() >= 3:
				var r = clamp(float(ec[0]), 0.0, 1.0)
				var g = clamp(float(ec[1]), 0.0, 1.0)
				var b = clamp(float(ec[2]), 0.0, 1.0)
				var a = 1.0
				if ec.size() > 3:
					a = clamp(float(ec[3]), 0.0, 1.0)
				material.emission = Color(r, g, b, a)
				material.emission_enabled = true
				has_color = true

		if has_color:
			node.material_override = material

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
	event_cubes[node_id] = {"node": node}
	node.set_meta("display_info", properties.get("display_info", {}))

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
	if node_id == "transcription_display":
		# Special handling for transcription display
		var transcription_node = get_node_or_null("transcription_display")
		if transcription_node and properties.has("text"):
			transcription_node.text = format_with_line_breaks(properties["text"], 80)
			# Ensure readability
			if not transcription_node.has_theme_override("font_size"):
				transcription_node.add_theme_font_size_override("font_size", 64)
			transcription_node.outline_size = 4
			transcription_node.outline_modulate = Color.BLACK

		return
	if node:
		if node is Label3D:
			if properties.has("text"):
				node.text = properties["text"]
			if properties.has("event_type") and EVENT_COLORS.has(properties["event_type"]):
				node.modulate = EVENT_COLORS[properties["event_type"]]
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
		if node is MeshInstance3D and properties.has("color"):
			var c = properties["color"]
			if c is Array and c.size() >= 3:
				var r = clamp(float(c[0]), 0.0, 1.0)
				var g = clamp(float(c[1]), 0.0, 1.0)
				var b = clamp(float(c[2]), 0.0, 1.0)
				var a = 1.0
				if c.size() > 3:
					a = clamp(float(c[3]), 0.0, 1.0)
				var material = StandardMaterial3D.new()
				material.albedo_color = Color(r, g, b, a)
				node.material_override = material


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
	var node_id = ""
	for id in event_cubes:
		if event_cubes[id].get("node", null) == node:
			node_id = id
			break
	info_label.text = "Node ID: " + node_id + "\nType: " + node.get_class() + "\nPosition: " + str(node.position)


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
	var details = "🎯 TARGETED OBJECT\n\n"
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
		details += "[b]Event Info:[/b]\n"
		for key in det.keys():
			details += "- %s: %s\n" % [key.capitalize(), str(det[key])]
		details += "\n"

	# Add visual properties
	if obj is MeshInstance3D and obj.material_override:
		var mat = obj.material_override
		if mat is StandardMaterial3D:
			details += "\nColor: %s" % str(mat.albedo_color).substr(0, 20)

	details += "\n\n[Click to interact]"

	return details


func _on_viewport_size_changed():
	# Update HUD panel position for new viewport size
	if targeting_hud_panel:
		targeting_hud_panel.position = Vector2(get_viewport().size.x - 410, 10)
	if game_log_panel:
		game_log_panel.position = Vector2(get_viewport().size.x - 410, get_viewport().size.y - 210)
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
	update_reticle_position()


func update_reticle_position():
	if targeting_reticle:
		targeting_reticle.position = get_viewport().size / 2.0 - Vector2(3, 3)


# Settings menu functions
func create_game_log():
	var canvas_layer = CanvasLayer.new()
	add_child(canvas_layer)

	game_log_panel = Panel.new()
	game_log_panel.size = Vector2(400, 200)
	game_log_panel.position = Vector2(get_viewport().size.x - 410, get_viewport().size.y - 210)

	# Dark background
	var style_box = StyleBoxFlat.new()
	style_box.bg_color = Color(0.1, 0.1, 0.1, 0.8)
	game_log_panel.add_theme_stylebox_override("panel", style_box)

	canvas_layer.add_child(game_log_panel)

	game_log_label = Label.new()
	game_log_label.position = Vector2(10, 10)
	game_log_label.size = Vector2(380, 180)
	game_log_label.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	game_log_label.add_theme_color_override("font_color", Color(1, 1, 1, 1))
	game_log_label.add_theme_font_size_override("font_size", 12)
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

	# Main settings panel - full screen overlay
	settings_panel = Panel.new()
	settings_panel.size = get_viewport().size
	settings_panel.position = Vector2(0, 0)
	settings_panel.visible = false

	# Dark background with full opacity and border
	var style_box = StyleBoxFlat.new()
	style_box.bg_color = Color(0.05, 0.05, 0.05, 0.95)
	style_box.border_color = Color(0.3, 0.3, 0.3, 1.0)
	style_box.border_width_left = 2
	style_box.border_width_right = 2
	style_box.border_width_top = 2
	style_box.border_width_bottom = 2
	settings_panel.add_theme_stylebox_override("panel", style_box)

	canvas_layer.add_child(settings_panel)

	# Header with title and close button
	var header_hbox = HBoxContainer.new()
	header_hbox.size = Vector2(settings_panel.size.x - 40, 60)
	header_hbox.position = Vector2(20, 20)
	header_hbox.add_theme_constant_override("separation", 20)
	settings_panel.add_child(header_hbox)

	var title_label = Label.new()
	title_label.text = "🧠 MindPalace Control Panel"
	title_label.add_theme_font_size_override("font_size", 28)
	title_label.vertical_alignment = VERTICAL_ALIGNMENT_CENTER
	title_label.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	header_hbox.add_child(title_label)

	var close_button = Button.new()
	close_button.text = "❌ Close (Tab)"
	close_button.size = Vector2(150, 40)
	close_button.tooltip_text = "Close the control panel (Tab key also works)"
	close_button.connect("pressed", Callable(self, "_on_close_settings"))
	header_hbox.add_child(close_button)

	# Tab container for organized sections
	var tab_container = TabContainer.new()
	tab_container.size = Vector2(settings_panel.size.x - 40, settings_panel.size.y - 100)
	tab_container.position = Vector2(20, 80)
	settings_panel.add_child(tab_container)

	# Environment Tab
	var env_tab = ScrollContainer.new()
	env_tab.name = "Environment"
	env_tab.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	env_tab.size_flags_vertical = Control.SIZE_EXPAND_FILL
	tab_container.add_child(env_tab)

	var env_vbox = VBoxContainer.new()
	env_vbox.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	env_vbox.add_theme_constant_override("separation", 15)
	env_tab.add_child(env_vbox)

	# Environment section title
	var env_title = Label.new()
	env_title.text = "🌫️ Environment Settings"
	env_title.add_theme_font_size_override("font_size", 20)
	env_title.add_theme_color_override("font_color", Color(0.8, 0.8, 1.0, 1.0))
	env_vbox.add_child(env_title)

	# Fog Density
	var fog_hbox = HBoxContainer.new()
	fog_hbox.add_theme_constant_override("separation", 15)
	var fog_label = Label.new()
	fog_label.text = "Fog Density:"
	fog_label.custom_minimum_size = Vector2(120, 30)
	fog_hbox.add_child(fog_label)
	var fog_slider = HSlider.new()
	fog_slider.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	fog_slider.min_value = 0.0
	fog_slider.max_value = 0.1
	fog_slider.value = 0.01
	fog_slider.connect("value_changed", Callable(self, "_on_fog_density_changed"))
	fog_hbox.add_child(fog_slider)
	var fog_value_label = Label.new()
	fog_value_label.text = "0.01"
	fog_value_label.custom_minimum_size = Vector2(50, 30)
	fog_slider.connect("value_changed", Callable(fog_value_label, "set_text").bind("%.3f"))
	fog_hbox.add_child(fog_value_label)
	env_vbox.add_child(fog_hbox)

	# Ambient Light
	var light_hbox = HBoxContainer.new()
	light_hbox.add_theme_constant_override("separation", 15)
	var light_label = Label.new()
	light_label.text = "Ambient Light:"
	light_label.custom_minimum_size = Vector2(120, 30)
	light_hbox.add_child(light_label)
	var light_slider = HSlider.new()
	light_slider.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	light_slider.min_value = 0.0
	light_slider.max_value = 1.0
	light_slider.value = 0.5
	light_slider.connect("value_changed", Callable(self, "_on_ambient_light_changed"))
	light_hbox.add_child(light_slider)
	var light_value_label = Label.new()
	light_value_label.text = "0.50"
	light_value_label.custom_minimum_size = Vector2(50, 30)
	light_slider.connect("value_changed", Callable(light_value_label, "set_text").bind("%.2f"))
	light_hbox.add_child(light_value_label)
	env_vbox.add_child(light_hbox)

	# Dark Mode Toggle
	dark_mode_button = Button.new()
	dark_mode_button.text = "🌙 Matrix Mode"
	dark_mode_button.size = Vector2(200, 45)
	dark_mode_button.add_theme_font_size_override("font_size", 16)
	dark_mode_button.connect("pressed", Callable(self, "_on_toggle_dark_mode"))
	env_vbox.add_child(dark_mode_button)

	# Controls Tab
	var controls_tab = ScrollContainer.new()
	controls_tab.name = "Controls"
	controls_tab.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	controls_tab.size_flags_vertical = Control.SIZE_EXPAND_FILL
	tab_container.add_child(controls_tab)

	var controls_vbox = VBoxContainer.new()
	controls_vbox.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	controls_vbox.add_theme_constant_override("separation", 15)
	controls_tab.add_child(controls_vbox)

	var controls_title = Label.new()
	controls_title.text = "🎮 Control Settings"
	controls_title.add_theme_font_size_override("font_size", 20)
	controls_title.add_theme_color_override("font_color", Color(0.8, 0.8, 1.0, 1.0))
	controls_vbox.add_child(controls_title)

	# Movement controls info
	var movement_info = Label.new()
	movement_info.text = "Movement: WASD or Arrow Keys\nMouse: Look around\nWheel: Zoom in/out\nTab: Toggle this menu"
	movement_info.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	controls_vbox.add_child(movement_info)

	# Audio controls
	var audio_title = Label.new()
	audio_title.text = "🎤 Audio Controls"
	audio_title.add_theme_font_size_override("font_size", 16)
	controls_vbox.add_child(audio_title)

	var audio_grid = GridContainer.new()
	audio_grid.columns = 2
	audio_grid.add_theme_constant_override("h_separation", 10)
	audio_grid.add_theme_constant_override("v_separation", 10)

	var toggle_mic_button = Button.new()
	toggle_mic_button.text = "🎤 Toggle Mic"
	toggle_mic_button.size = Vector2(150, 45)
	toggle_mic_button.tooltip_text = "Enable/disable voice input for AI requests"
	toggle_mic_button.connect("pressed", Callable(self, "_on_toggle_mic"))
	audio_grid.add_child(toggle_mic_button)

	var test_transcription_button = Button.new()
	test_transcription_button.text = "🧪 Test Transcription"
	test_transcription_button.size = Vector2(180, 45)
	test_transcription_button.tooltip_text = "Send a test transcription to verify voice input"
	test_transcription_button.connect("pressed", Callable(self, "_on_test_transcription"))
	audio_grid.add_child(test_transcription_button)

	controls_vbox.add_child(audio_grid)

	# Actions Tab
	var actions_tab = ScrollContainer.new()
	actions_tab.name = "Actions"
	actions_tab.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	actions_tab.size_flags_vertical = Control.SIZE_EXPAND_FILL
	tab_container.add_child(actions_tab)

	var actions_vbox = VBoxContainer.new()
	actions_vbox.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	actions_vbox.add_theme_constant_override("separation", 15)
	actions_tab.add_child(actions_vbox)

	var actions_title = Label.new()
	actions_title.text = "⚡ Quick Actions"
	actions_title.add_theme_font_size_override("font_size", 20)
	actions_title.add_theme_color_override("font_color", Color(0.8, 0.8, 1.0, 1.0))
	actions_vbox.add_child(actions_title)

	# Action buttons in grid
	var actions_grid = GridContainer.new()
	actions_grid.columns = 2
	actions_grid.add_theme_constant_override("h_separation", 10)
	actions_grid.add_theme_constant_override("v_separation", 10)

	var list_tasks_button = Button.new()
	list_tasks_button.text = "📋 List Tasks"
	list_tasks_button.size = Vector2(150, 45)
	list_tasks_button.tooltip_text = "Display all current tasks in the 3D space"
	list_tasks_button.connect("pressed", Callable(self, "_on_list_tasks"))
	actions_grid.add_child(list_tasks_button)

	var clear_button = Button.new()
	clear_button.text = "🗑️ Clear Objects"
	clear_button.size = Vector2(150, 45)
	clear_button.tooltip_text = "Remove all objects from the 3D world"
	clear_button.connect("pressed", Callable(self, "_on_clear_objects"))
	actions_grid.add_child(clear_button)

	var particles_button = Button.new()
	particles_button.text = "✨ Toggle Particles"
	particles_button.size = Vector2(180, 45)
	particles_button.tooltip_text = "Show/hide ambient particle effects"
	# particles_button.connect("pressed", Callable(self, "_on_toggle_particles"))
	actions_grid.add_child(particles_button)

	actions_vbox.add_child(actions_grid)

	# AI Request Section
	var request_title = Label.new()
	request_title.text = "🤖 AI Request"
	request_title.add_theme_font_size_override("font_size", 18)
	actions_vbox.add_child(request_title)

	var request_hbox = HBoxContainer.new()
	request_hbox.add_theme_constant_override("separation", 10)
	user_request_input = LineEdit.new()
	user_request_input.custom_minimum_size = Vector2(500, 45)
	user_request_input.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	user_request_input.placeholder_text = "Ask MindPalace anything..."
	user_request_input.add_theme_font_size_override("font_size", 14)
	request_hbox.add_child(user_request_input)
	send_request_button = Button.new()
	send_request_button.text = "🚀 Send"
	send_request_button.size = Vector2(100, 45)
	send_request_button.connect("pressed", Callable(self, "_on_send_request"))
	request_hbox.add_child(send_request_button)
	actions_vbox.add_child(request_hbox)

	# System Tab
	var system_tab = ScrollContainer.new()
	system_tab.name = "System"
	system_tab.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	system_tab.size_flags_vertical = Control.SIZE_EXPAND_FILL
	tab_container.add_child(system_tab)

	var system_vbox = VBoxContainer.new()
	system_vbox.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	system_vbox.add_theme_constant_override("separation", 15)
	system_tab.add_child(system_vbox)

	var system_title = Label.new()
	system_title.text = "⚙️ System"
	system_title.add_theme_font_size_override("font_size", 20)
	system_title.add_theme_color_override("font_color", Color(0.8, 0.8, 1.0, 1.0))
	system_vbox.add_child(system_title)

	# Debug info
	var debug_info = Label.new()
	debug_info.name = "debug_info"
	debug_info.text = "Debug Info:\nObjects: " + str(event_cubes.size()) + "\nUnderground: " + str(underground_node.get_child_count() if underground_node else 0)
	system_vbox.add_child(debug_info)

	# Exit button
	var exit_button = Button.new()
	exit_button.text = "🚪 Exit MindPalace"
	exit_button.size = Vector2(200, 50)
	exit_button.add_theme_font_size_override("font_size", 16)
	exit_button.tooltip_text = "Exit the application (unsaved data will be lost)"
	var exit_style = StyleBoxFlat.new()
	exit_style.bg_color = Color(0.8, 0.2, 0.2, 1.0)
	exit_button.add_theme_stylebox_override("normal", exit_style)
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

func _on_toggle_particles():
	if ambient_particles:
		ambient_particles.visible = !ambient_particles.visible

func _on_toggle_mic():
	send_toggle_mic()

func _on_toggle_dark_mode():
	dark_mode = !dark_mode
	if dark_mode_button:
		dark_mode_button.text = "🌙 Toggle Dark Mode" if dark_mode else "☀️ Toggle Light Mode"
	if env:
		if dark_mode:
			# Spooky shadow realm
			env.background_energy_multiplier = 0.4
			env.fog_enabled = true
			env.fog_density = 0.1
			env.fog_color = Color(1, 1, 1, 1)
			env.background_mode = 2	 # Sky
		else:
			# Matrix construct vibes - very bright
			env.background_energy_multiplier = 1.5
			env.fog_enabled = false
			env.background_mode = 1	 # Solid color
			env.background_color = Color(0.8, 0.9, 1.0, 1)	# Bright sky blue

func _on_create_task():
	send_request("Create a new task titled 'New Task from Control Panel'")

func _on_list_tasks():
	send_request("List all tasks")

func _on_clear_objects():
	send_request("Clear all objects in the 3D world")

func _on_send_request():
	var text = user_request_input.text.strip_edges()
	if text != "":
		send_request(text)
		user_request_input.text = ""

func send_request(text: String):
	if websocket.get_ready_state() != WebSocketPeer.STATE_OPEN:
		return
	var req_msg = {
		"type": "request",
		"text": text
	}
	var json_string = JSON.stringify(req_msg)
	var err = websocket.send_text(json_string)
	if err != OK:
		push_error("Failed to send request: ", err)

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
	bg.color = Color(0, 0, 0, 0.1)  # Semi-transparent
	canvas_layer.add_child(bg)
	bg.connect("gui_input", Callable(self, "_on_menu_bg_input"))

	# Menu container
	var menu_control = Control.new()
	menu_control.position = mouse_pos
	canvas_layer.add_child(menu_control)

	# Button
	var button = Button.new()
	button.text = "🎤 Activate Transcription"
	button.size = Vector2(200, 40)
	button.position = Vector2(-100, 20)  # Center below cursor
	var style = StyleBoxFlat.new()
	style.bg_color = Color(0.1, 0.1, 0.1, 0.9)
	style.border_color = Color(0.5, 0.5, 0.5, 1.0)
	style.border_width_left = 1
	style.border_width_right = 1
	style.border_width_top = 1
	style.border_width_bottom = 1
	button.add_theme_stylebox_override("normal", style)
	button.add_theme_color_override("font_color", Color.WHITE)
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
