extends Node3D

@onready var mesh_instance: MeshInstance3D = $Floor/MeshInstance3D
@onready var camera: Camera3D = $Player/Camera

var websocket = WebSocketPeer.new()
const WS_URL = "ws://localhost:8081/godot"
var connected = false
var sent_start_signal = false

var event_count = 0
const CUBE_SPACING = 2.0

# Mapping of event types to colors
const EVENT_COLORS = {
	"task_created": Color.BLUE,
	"task_updated": Color.YELLOW,
	"task_completed": Color.GREEN,
	"task_deleted": Color.RED,
	"plugin_created": Color.PURPLE,
	"tool_call_started": Color.ORANGE,
	"tool_call_completed": Color.CYAN,
	"agent_call_decided": Color.MAGENTA,
	"agent_execution_failed": Color.DARK_RED,
	"tool_call_failed": Color.DARK_ORANGE,
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

# Audio capture removed - backend handles it

# Microphone settings menu (simplified - no audio level since backend captures)
var settings_panel: Panel
var settings_label: Label
var mic_device_option: OptionButton
var settings_visible: bool = false



func _ready():
	mesh_instance.extra_cull_margin = 2.0


	# Audio capture handled by backend - signal sent on connect

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
	style_box.bg_color = Color(0.1, 0.1, 0.1, 0.95)  # Dark grey, very opaque
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

	# Add ambient light and fog for atmosphere
	var world_env = WorldEnvironment.new()
	var env = Environment.new()
	env.background_mode = Environment.BG_COLOR
	env.background_color = Color(0.1, 0.1, 0.2)  # Dark blue sky
	env.fog_enabled = true
	env.fog_color = Color(0.2, 0.2, 0.3)
	env.fog_density = 0.01
	env.ambient_light_color = Color(0.3, 0.3, 0.5)
	env.ambient_light_energy = 0.5
	world_env.environment = env
	add_child(world_env)

	# Add directional light
	var dir_light = DirectionalLight3D.new()
	dir_light.rotation_degrees = Vector3(-30, 45, 0)
	dir_light.light_color = Color(1, 1, 0.9)
	dir_light.light_energy = 1.0
	add_child(dir_light)

	# Add ambient particle field
	var ambient_particles = GPUParticles3D.new()
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

	# Create settings menu
	create_settings_menu()

	# No local audio level monitoring

	# Set up WebSocket connection
	var err = websocket.connect_to_url(WS_URL)
	if err != OK:
		push_error("Failed to connect to WebSocket: ", err)
	else:
	
		# Send ready signal once connected
		send_ready_signal()

	# Position camera to see objects
	$Player.position.z = 10
	$Player/Camera.rotation_degrees.x = -45

func _process(delta):
	# Handle WebSocket connection and messages
	websocket.poll()
	var state = websocket.get_ready_state()

	if state == WebSocketPeer.STATE_OPEN:
		if not connected:
			connected = true
			
			send_ready_signal()
		
		# if connected and not sent_start_signal:
		# 	var start_msg = {
		# 		"type": "start_audio_capture"
		# 	}
		# 	var json_string = JSON.stringify(start_msg)
		# 	var err = websocket.send_text(json_string)
		# 	if err == OK:
		# 		sent_start_signal = true
		# 		print("Sent start audio capture signal to backend")
		# 	else:
		# 		print("Failed to send start signal: ", err)
		# Audio capture starts on Go backend startup - no signal needed

		# Process all available messages
		while websocket.get_available_packet_count() > 0:
			var packet = websocket.get_packet()
			var message = packet.get_string_from_utf8()
			_on_websocket_message(message)

	elif state == WebSocketPeer.STATE_CLOSED:
		if connected:
			connected = false
		
			var err = websocket.connect_to_url(WS_URL)
		

	# Update targeting HUD
	update_targeting()

var mouse_pressed = false
var last_mouse_pos = Vector2()

func _input(event):
	if not settings_visible:
		if event is InputEventMouseButton:
			if event.button_index == MOUSE_BUTTON_LEFT:
				mouse_pressed = event.pressed
				if not event.pressed:
					info_panel.visible = false
			elif event.button_index == MOUSE_BUTTON_WHEEL_UP:
				camera.position.y -= 1
			elif event.button_index == MOUSE_BUTTON_WHEEL_DOWN:
				camera.position.y += 1
		elif event is InputEventMouseMotion and mouse_pressed:
			var delta = event.relative
			camera.rotation_degrees.y -= delta.x * 0.5
			camera.rotation_degrees.x -= delta.y * 0.5
			camera.rotation_degrees.x = clamp(camera.rotation_degrees.x, -90, 90)

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
				while clicked_node and not (clicked_node in event_cubes.values()):
					clicked_node = clicked_node.get_parent()
				if clicked_node:
					show_info_panel(clicked_node)

	# Handle Escape key for settings menu
	if event.is_action_pressed("ui_cancel"):
		toggle_settings_menu()

func _on_websocket_message(message: String):
	var json = JSON.new()
	var error = json.parse(message)
	if error == OK:
		var data = json.data
		if data.has("type"):
			if data["type"] == "keypresses":
				process_keypresses(data)
			else:
				process_event_message(data)

func send_ready_signal():
	if websocket.get_ready_state() == WebSocketPeer.STATE_OPEN:
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
	

func process_event_message(data: Dictionary):
	if not data or typeof(data) != TYPE_DICTIONARY:
	
		return
	if not data.has("type") or data["type"] != "delta":
	
		return
	if not data.has("actions") or typeof(data["actions"]) != TYPE_ARRAY:
	
		return

	# Handle DeltaEnvelope
	for action in data["actions"]:
		handle_action(action)

func process_keypresses(data: Dictionary):
	if not data.has("keys") or typeof(data["keys"]) != TYPE_STRING:
		push_error("Invalid keypresses message: missing or invalid 'keys' field")
		return
	var key_string = data["keys"]

	simulate_keypresses(key_string)
	send_state_update()

func simulate_keypresses(key_string: String):
	if settings_visible:
		handle_menu_keypresses(key_string)
	else:
		handle_player_keypresses(key_string)

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
		if char == "w" or char == "up":
			# Move up in menu (previous item)
			if mic_device_option.selected > 0:
				mic_device_option.selected -= 1
				_on_mic_device_selected(mic_device_option.selected)
		elif char == "s" or char == "down":
			# Move down in menu (next item)
			if mic_device_option.selected < mic_device_option.item_count - 1:
				mic_device_option.selected += 1
				_on_mic_device_selected(mic_device_option.selected)
		elif char == " " or char == "enter":
			# Confirm selection (already handled by _on_mic_device_selected)
			pass
		elif char == "escape":
			# Close menu
			toggle_settings_menu()

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

func handle_action(action: Dictionary):
	if typeof(action) != TYPE_DICTIONARY:
	
		return
	var action_type = action.get("type", "")
	if action_type == "":
	
		return
	var node_id = action.get("node_id", "")
	if node_id == "":
	
		return
	var node_type = action.get("node_type", "")
	var properties = action.get("properties", {})

	match action_type:
		"create":
			if node_type == "":
			
				return
			create_node(node_id, node_type, properties)
		"update":
			update_node(node_id, properties)
		"delete":
			delete_node(node_id)
		_:
			pass

func create_node(node_id: String, node_type: String, properties: Dictionary):
	if event_cubes.has(node_id):
	
		return
	
	var node
	match node_type:
		"MeshInstance3D":
			node = MeshInstance3D.new()
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
		"CharacterBody3D":
			node = CharacterBody3D.new()
		"Label3D":
			node = Label3D.new()
		_:
		
			return
	
	if properties.has("position"):
		var pos = properties["position"]
		if pos is Array and pos.size() >= 3:
			var x = clamp(float(pos[0]), -1000.0, 1000.0)
			var y = clamp(float(pos[1]), -1000.0, 1000.0)
			var z = clamp(float(pos[2]), -1000.0, 1000.0)
			node.position = Vector3(x, y, z)

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
	
	# Add material if color specified
	var material = StandardMaterial3D.new()
	var has_color = false
	if properties.has("color"):
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
		particle_process_material.direction = Vector3(0, 1, 0)  # Upward
		particle_process_material.spread = 180.0  # Full spread
		particle_process_material.gravity = Vector3(0, -1, 0)  # Slight downward gravity for smoke
		particle_process_material.initial_velocity_min = 1.0
		particle_process_material.initial_velocity_max = 3.0
		particle_process_material.color = Color(1.0, 0.8, 0.4, 0.6)  # Warm yellow smoke
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
	event_cubes[node_id] = node


func update_node(node_id: String, properties: Dictionary):
	var node = event_cubes.get(node_id, null)
	if node_id == "transcription_display":
		# Special handling for transcription display
		var transcription_node = get_node_or_null("transcription_display")
		if transcription_node and properties.has("text"):
			transcription_node.text = properties["text"]
		
		
		return
	if node:
		if properties.has("position"):
			var pos = properties["position"]
			if pos is Array and pos.size() >= 3:
				var x = clamp(float(pos[0]), -1000.0, 1000.0)
				var y = clamp(float(pos[1]), -1000.0, 1000.0)
				var z = clamp(float(pos[2]), -1000.0, 1000.0)
				node.position = Vector3(x, y, z)
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
		if properties.has("color"):
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
	var node = event_cubes.get(node_id, null)
	if node:
		node.queue_free()
		event_cubes.erase(node_id)

func show_info_panel(node: Node):
	info_panel.visible = true
	var node_id = ""
	for id in event_cubes:
		if event_cubes[id] == node:
			node_id = id
			break
	info_label.text = "Node ID: " + node_id + "\nType: " + node.get_class() + "\nPosition: " + str(node.position)
	# Add metadata if available (from properties, but since not stored, basic info)
	if node_id.begins_with("task_"):
		info_label.text += "\nCategory: Task"
	elif node_id.begins_with("request_"):
		info_label.text += "\nCategory: User Request"
	elif node_id.begins_with("calendar_"):
		info_label.text += "\nCategory: Calendar Event"
	elif node_id == "orchestrator_ai":
		info_label.text += "\nCategory: AI Orchestrator"


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
				targeted_object = hit_object
				update_hud_for_object(event_id, hit_object)
				targeting_reticle.color = Color.GREEN
		else:
			# Looking at non-event object
			if targeted_object != null:
				targeted_object = null
				clear_targeting_hud()
				targeting_reticle.color = Color.YELLOW
	else:
		# Looking at nothing
		if targeted_object != null:
			targeted_object = null
			clear_targeting_hud()
			targeting_reticle.color = Color.WHITE


func get_event_id_from_object(obj: Node) -> String:
	# Walk up the hierarchy to find event objects
	var current = obj
	while current:
		for event_id in event_cubes:
			if event_cubes[event_id] == current:
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

	# Add event-specific details
	if event_id.begins_with("task_"):
		details += "📋 Category: Task\n"
		details += "Status: Active\n"
	elif event_id.begins_with("request_"):
		details += "💬 Category: User Request\n"
		details += "Status: Processing\n"
	elif event_id.begins_with("calendar_"):
		details += "📅 Category: Calendar Event\n"
		details += "Status: Scheduled\n"
	elif event_id == "orchestrator_ai":
		details += "🤖 Category: AI Orchestrator\n"
		details += "Status: Active\n"
	else:
		details += "📦 Category: Event Object\n"

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
	update_reticle_position()


func update_reticle_position():
	if targeting_reticle:
		targeting_reticle.position = get_viewport().size / 2.0 - Vector2(3, 3)


# Settings menu functions
func create_settings_menu():
	var canvas_layer = CanvasLayer.new()
	add_child(canvas_layer)

	# Main settings panel
	settings_panel = Panel.new()
	settings_panel.size = Vector2(400, 200)
	settings_panel.position = Vector2(get_viewport().size.x / 2 - 200, get_viewport().size.y / 2 - 100)
	settings_panel.visible = false

	# Dark background
	var style_box = StyleBoxFlat.new()
	style_box.bg_color = Color(0.1, 0.1, 0.1, 0.95)
	settings_panel.add_theme_stylebox_override("panel", style_box)

	canvas_layer.add_child(settings_panel)

	# Title label
	var title_label = Label.new()
	title_label.text = "🎙️ Audio Settings"
	title_label.position = Vector2(10, 10)
	title_label.add_theme_font_size_override("font_size", 20)
	settings_panel.add_child(title_label)

	# Microphone device selection (informational - backend uses default)
	var mic_label = Label.new()
	mic_label.text = "Microphone (Backend uses default):"
	mic_label.position = Vector2(10, 50)
	settings_panel.add_child(mic_label)

	mic_device_option = OptionButton.new()
	mic_device_option.position = Vector2(10, 75)
	mic_device_option.size = Vector2(380, 30)
	settings_panel.add_child(mic_device_option)
	mic_device_option.disabled = true  # Disabled since backend handles capture

	# Instructions
	var instructions = Label.new()
	instructions.text = "Press ESC to close\n\nAudio capture is handled by the backend.\nMicrophone selection is for reference only."
	instructions.position = Vector2(10, 110)
	instructions.size = Vector2(380, 80)
	instructions.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	settings_panel.add_child(instructions)

	# Populate microphone devices (read-only)
	populate_mic_devices()


func populate_mic_devices():
	mic_device_option.clear()
	var input_devices = AudioServer.get_input_device_list()
	var current_device = AudioServer.input_device

	for i in range(input_devices.size()):
		var device = input_devices[i]
		mic_device_option.add_item(device, i)
		if device == current_device:
			mic_device_option.selected = i


func toggle_settings_menu():
	settings_visible = !settings_visible
	settings_panel.visible = settings_visible

	if settings_visible:
		populate_mic_devices()  # Refresh device list
		Input.mouse_mode = Input.MOUSE_MODE_VISIBLE  # Release mouse for GUI interaction
	else:
		Input.mouse_mode = Input.MOUSE_MODE_CAPTURED  # Capture mouse for camera control
	
	send_state_update()


func _on_mic_device_selected(index: int):
	# Disabled - backend uses default device
	pass


# Removed _update_audio_level - no local audio monitoring


# Removed create_wav_header - no local audio processing

# Removed _send_audio_chunk - backend handles capture
