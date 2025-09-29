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
		yield(get_tree().create_timer(RECONNECT_DELAY), "timeout")
		websocket.connect_to_url(WEBSOCKET_URL)
		set_process(true)

func process_event_message(message: String):
	var json = JSON.new()
	var error = json.parse(message)
	if error == OK:
		var data = json.data
		if data is Array:
			# Assume initial load of all events as an array
			for event in data:
				handle_event(event)
		else:
			# Single event
			handle_event(data)
	else:
		print("Failed to parse JSON: ", message)

func handle_event(event: Dictionary):
	# Ensure we have an event ID to track updates/deletes
	if not event.has("event_id"):
		# If the event doesn't provide an ID, generate one from timestamp
		event["event_id"] = "anon_" + str(event_count)
	
	# Check if this event is a delete or update
	if event.has("event_type"):
		match event["event_type"]:
			"task_deleted":
				remove_cube(event["event_id"])
				return
			"task_updated":
				update_cube(event["event_id"], event)
				return
	
	# Default: spawn a new cube
	spawn_cube_for_event(event)

func spawn_cube_for_event(event: Dictionary):
	var cube = MeshInstance3D.new()
	cube.mesh = BoxMesh.new()
	cube.mesh.size = Vector3(1, 1, 1)
	
	# Position in a line along X-axis
	cube.position = Vector3(event_count * CUBE_SPACING, 0, 0)
	
	# Optionally, customize based on event type (e.g., color)
	if event.has("event_type"):
		var material = StandardMaterial3D.new()
		var color = EVENT_COLORS.get(event["event_type"], Color.GRAY)
		material.albedo_color = color
		cube.material_override = material
	
	add_child(cube)
	event_cubes[event["event_id"]] = cube
	event_count += 1
	print("Spawned cube for event: ", event)

func update_cube(event_id: String, event: Dictionary):
	var cube = event_cubes.get(event_id, null)
	if cube:
		# For simplicity, change color to indicate update
		var material = StandardMaterial3D.new()
		material.albedo_color = EVENT_COLORS.get("task_updated", Color.YELLOW)
		cube.material_override = material
		print("Updated cube for event_id: ", event_id)
	else:
		# If cube not found, spawn a new one
		spawn_cube_for_event(event)

func remove_cube(event_id: String):
	var cube = event_cubes.get(event_id, null)
	if cube:
		cube.queue_free()
		event_cubes.erase(event_id)
		print("Removed cube for event_id: ", event_id)
	else:
		print("Attempted to remove non-existent cube for event_id: ", event_id)
