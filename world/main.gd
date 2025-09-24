extends Node3D

@onready var websocket = WebSocketPeer.new()
var event_count = 0
const CUBE_SPACING = 2.0
const WEBSOCKET_URL = "ws://localhost:8080/events"  # Update this to your actual websocket URL

func _ready():
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

func process_event_message(message: String):
	var json = JSON.new()
	var error = json.parse(message)
	if error == OK:
		var data = json.data
		if data is Array:
			# Assume initial load of all events as an array
			for event in data:
				spawn_cube_for_event(event)
		else:
			# Single event
			spawn_cube_for_event(data)
	else:
		print("Failed to parse JSON: ", message)

func spawn_cube_for_event(event: Dictionary):
	var cube = MeshInstance3D.new()
	cube.mesh = BoxMesh.new()
	cube.mesh.size = Vector3(1, 1, 1)
	
	# Position in a line along X-axis
	cube.position = Vector3(event_count * CUBE_SPACING, 0, 0)
	
	# Optionally, customize based on event type (e.g., color)
	if event.has("event_type"):
		var material = StandardMaterial3D.new()
		if event["event_type"] == "task_created":
			material.albedo_color = Color.BLUE
		elif event["event_type"] == "task_completed":
			material.albedo_color = Color.GREEN
		else:
			material.albedo_color = Color.GRAY
		cube.material_override = material
	
	add_child(cube)
	event_count += 1
	print("Spawned cube for event: ", event.get("event_type", "unknown"))
