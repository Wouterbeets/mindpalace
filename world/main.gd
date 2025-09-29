extends Node3D


@onready var mesh_instance: MeshInstance3D = $Floor/MeshInstance3D

@onready var websocket = WebSocketPeer.new()

var event_count = 0
const CUBE_SPACING = 2.0
const WEBSOCKET_URL = "ws://localhost:8081/godot"  # Update this to your actual websocket URL
const RECONNECT_DELAY = 5.0  # seconds

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

func _ready():
	mesh_instance.extra_cull_margin = 2.0
	print("main ready call")
	websocket.connect_to_url(WEBSOCKET_URL)
	print("Connecting to websocket...")

func _process(delta):
	websocket.poll()
	var state = websocket.get_ready_state()
	if state == WebSocketPeer.STATE_OPEN:
		while websocket.get_available_packet_count() > 0:
			var packet = websocket.get_packet()
			var message = packet.get_string_from_utf8()
			process_event_message(message)
	elif state == WebSocketPeer.STATE_CLOSED:
		print("WebSocket closed")
		set_process(false)
		# Attempt reconnection after a delay
		await get_tree().create_timer(RECONNECT_DELAY).timeout
		websocket.connect_to_url(WEBSOCKET_URL)
		set_process(true)

func process_event_message(message: String):
	var json = JSON.new()
	var error = json.parse(message)
	if error == OK:
		var data = json.data
		if data.has("type") and data["type"] == "delta":
			# Handle DeltaEnvelope
			for action in data["actions"]:
				handle_action(action)
		else:
			print("Unknown message format: ", data)
	else:
		print("Failed to parse JSON: ", message)

func handle_action(action: Dictionary):
	var action_type = action.get("type", "")
	var node_id = action.get("node_id", "")
	var node_type = action.get("node_type", "")
	var properties = action.get("properties", {})
	
	match action_type:
		"create":
			create_node(node_id, node_type, properties)
		"update":
			update_node(node_id, properties)
		"delete":
			delete_node(node_id)
		"animate":
			animate_node(node_id, action.get("animation", {}))
		_:
			print("Unknown action type: ", action_type)

func create_node(node_id: String, node_type: String, properties: Dictionary):
	if event_cubes.has(node_id):
		print("Node ", node_id, " already exists, skipping create")
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
		"CharacterBody3D":
			node = CharacterBody3D.new()
		"Label3D":
			node = Label3D.new()
		_:
			print("Unknown node_type: ", node_type)
			return
	
	if properties.has("position"):
		var pos = properties["position"]
		if pos is Array and pos.size() >= 3:
			var target_pos = Vector3(pos[0], pos[1], pos[2])
			if target_pos.y < 0:  # Underground
				node.position = Vector3(pos[0], -5.0, pos[2])  # Start deeper
				var rise_tween = create_tween()
				rise_tween.tween_property(node, "position", target_pos, 2.0).set_trans(Tween.TRANS_QUAD).set_ease(Tween.EASE_OUT)
			else:
				node.position = target_pos
	
	if properties.has("text"):
		if node is Label3D:
			node.text = properties["text"]
	
	# Add material if color specified
	var material = StandardMaterial3D.new()
	var has_color = false
	if properties.has("color"):
		var c = properties["color"]
		if c is Array and c.size() >= 3:
			material.albedo_color = Color(c[0], c[1], c[2], c[3] if c.size() > 3 else 1.0)
			has_color = true
	if properties.has("material_override"):
		var mo = properties["material_override"]
		if mo is Dictionary and mo.has("albedo_color"):
			var c = mo["albedo_color"]
			if c is Array and c.size() >= 3:
				material.albedo_color = Color(c[0], c[1], c[2], c[3] if c.size() > 3 else 1.0)
				has_color = true
	if properties.has("emissive_color"):
		var ec = properties["emissive_color"]
		if ec is Array and ec.size() >= 3:
			material.emission = Color(ec[0], ec[1], ec[2], ec[3] if ec.size() > 3 else 1.0)
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
	
	add_child(node)
	event_cubes[node_id] = node
	print("Created node ", node_id, " of type ", node_type)

	# Start floating animation for orchestrator
	if node_id == "orchestrator_ai":
		var tween = create_tween()
		tween.set_loops()
		tween.tween_property(node, "position:y", 6.0, 2.0).set_trans(Tween.TRANS_SINE)
		tween.tween_property(node, "position:y", 4.0, 2.0).set_trans(Tween.TRANS_SINE)

	# Start stream animation for underground nodes
	if properties.has("stream") and properties["stream"] == true and node.position.y < 0:
		var stream_tween = create_tween()
		stream_tween.set_loops()
		stream_tween.tween_property(node, "position:z", 0.0, 15.0).from(-20.0).set_trans(Tween.TRANS_LINEAR)

func update_node(node_id: String, properties: Dictionary):
	var node = event_cubes.get(node_id, null)
	if node:
		if properties.has("position"):
			var pos = properties["position"]
			if pos is Array and pos.size() >= 3:
				node.position = Vector3(pos[0], pos[1], pos[2])
		if properties.has("color"):
			var material = StandardMaterial3D.new()
			material.albedo_color = Color(properties["color"])
			node.material_override = material
		print("Updated node ", node_id)
	else:
		print("Node ", node_id, " not found for update")

func delete_node(node_id: String):
	var node = event_cubes.get(node_id, null)
	if node:
		node.queue_free()
		event_cubes.erase(node_id)
		print("Deleted node ", node_id)
	else:
		print("Node ", node_id, " not found for delete")

func animate_node(node_id: String, animation: Dictionary):
	var node = event_cubes.get(node_id, null)
	if node:
		var tween = create_tween()
		var property = animation.get("property", "")
		var to_value = animation.get("to", null)
		var duration = animation.get("duration", 1.0)
		if property == "position" and to_value is Array and to_value.size() >= 3:
			tween.tween_property(node, "position", Vector3(to_value[0], to_value[1], to_value[2]), duration)
		print("Animated node ", node_id)
	else:
		print("Node ", node_id, " not found for animation")
