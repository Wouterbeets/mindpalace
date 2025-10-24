extends Node3D

# Zone visualizer - paints floors and borders for zones
# Drops shader floor, draws shapes instead

var zone_nodes = []  # Keep track of created nodes for cleanup

func draw_zones(zones: Dictionary):
	print("GODOT: ZoneVisualizer.draw_zones() called with ", zones.size(), " zones")
	# Clear previous zones
	clear_zones()
	print("GODOT: Cleared previous zones")

	# Disable shader floor if exists
	var floor = get_node_or_null("../Floor/MeshInstance3D")
	if floor:
		floor.visible = false
		print("GODOT: Disabled shader floor")
	else:
		print("GODOT: Shader floor not found")

	# Sort zone names by angle for border drawing
	var sorted_names = zones.keys()
	sorted_names.sort_custom(func(a, b): return zones[a].get("angle", 0) < zones[b].get("angle", 0))
	print("GODOT: Sorted zones: ", sorted_names)

	# Draw each zone floor
	for zone_name in sorted_names:
		var zone_data = zones[zone_name]
		var angle = zone_data.get("angle", 0.0)
		var radius = zone_data.get("radius", 10.0)
		print("GODOT: Drawing floor for zone '", zone_name, "' at angle ", angle, " radius ", radius)

		# Create floor mesh for this zone
		var floor_mesh = create_zone_floor(angle, radius, zone_name)
		if floor_mesh:
			add_child(floor_mesh)
			zone_nodes.append(floor_mesh)
			print("GODOT: Added floor mesh for ", zone_name)
		else:
			print("GODOT: Failed to create floor mesh for ", zone_name)

		# Create floating label for this zone
		var label = create_zone_label(angle, radius, zone_name)
		if label:
			add_child(label)
			zone_nodes.append(label)
			print("GODOT: Added label for ", zone_name)
		else:
			print("GODOT: Failed to create label for ", zone_name)

	# Draw borders between zones
	for i in range(sorted_names.size()):
		var current_name = sorted_names[i]
		var next_name = sorted_names[(i + 1) % sorted_names.size()]
		var current = zones[current_name]
		var next = zones[next_name]
		print("GODOT: Drawing border between ", current_name, " and ", next_name)

		var border_line = create_zone_border(current, next)
		if border_line:
			add_child(border_line)
			zone_nodes.append(border_line)
			print("GODOT: Added border between ", current_name, " and ", next_name)
		else:
			print("GODOT: Failed to create border between ", current_name, " and ", next_name)

	print("GODOT: Zone visualization complete, ", zone_nodes.size(), " nodes added")

func create_zone_floor(angle: float, radius: float, zone_name: String) -> MeshInstance3D:
	var mesh_instance = MeshInstance3D.new()
	mesh_instance.name = "zone_floor_" + zone_name

	# Create sector mesh using SurfaceTool with triangles
	var surface_tool = SurfaceTool.new()
	surface_tool.begin(Mesh.PRIMITIVE_TRIANGLES)

	# Sector width - assume 360 / num_zones degrees
	var sector_width = 360.0 / get_parent().get_meta("zone_count", 1)

	# Calculate sector angles
	var start_angle = deg_to_rad(angle - sector_width / 2.0)
	var end_angle = deg_to_rad(angle + sector_width / 2.0)

	# Add triangles for the sector
	var segments = 16
	var center = Vector3(0, 0, 0)
	for i in range(segments):
		var t1 = float(i) / segments
		var t2 = float(i + 1) / segments
		var angle1 = lerp(start_angle, end_angle, t1)
		var angle2 = lerp(start_angle, end_angle, t2)
		var p1 = Vector3(radius * cos(angle1), 0, radius * sin(angle1))
		var p2 = Vector3(radius * cos(angle2), 0, radius * sin(angle2))

		# Triangle: center, p1, p2
		surface_tool.add_vertex(center)
		surface_tool.add_vertex(p1)
		surface_tool.add_vertex(p2)

	# Create mesh
	var mesh = surface_tool.commit()
	mesh_instance.mesh = mesh

	# Add material with zone color
	var material = StandardMaterial3D.new()
	material.albedo_color = get_zone_color(zone_name)
	material.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
	material.albedo_color.a = 0.3  # Semi-transparent
	mesh_instance.material_override = material

	return mesh_instance

func create_zone_border(zone1: Dictionary, zone2: Dictionary) -> MeshInstance3D:
	var line_mesh = MeshInstance3D.new()
	line_mesh.name = "zone_border"

	var surface_tool = SurfaceTool.new()
	surface_tool.begin(Mesh.PRIMITIVE_LINES)

	var angle1 = deg_to_rad(zone1.get("angle", 0))
	var angle2 = deg_to_rad(zone2.get("angle", 0))
	var radius = max(zone1.get("radius", 10), zone2.get("radius", 10))

	# Draw line from center to edge at angle1
	surface_tool.add_vertex(Vector3(0, 0.1, 0))  # Slightly above ground
	surface_tool.add_vertex(Vector3(radius * cos(angle1), 0.1, radius * sin(angle1)))

	# Draw line from center to edge at angle2
	surface_tool.add_vertex(Vector3(0, 0.1, 0))
	surface_tool.add_vertex(Vector3(radius * cos(angle2), 0.1, radius * sin(angle2)))

	var mesh = surface_tool.commit()
	line_mesh.mesh = mesh

	# Add black border material
	var material = StandardMaterial3D.new()
	material.albedo_color = Color.BLACK
	line_mesh.material_override = material

	return line_mesh

func get_zone_color(zone_name: String) -> Color:
	# Assign colors based on zone name
	var colors = {
		"calendar": Color.BLUE,
		"taskmanager": Color.GREEN,
		"notes": Color.YELLOW,
		"unknown": Color.GRAY
	}
	return colors.get(zone_name.to_lower(), Color.GRAY)

func create_zone_label(angle: float, radius: float, zone_name: String) -> Label3D:
	var label = Label3D.new()
	label.name = "zone_label_" + zone_name
	label.text = zone_name.to_upper()
	label.font_size = 200  # Massive size
	label.outline_size = 20
	label.outline_modulate = Color.BLACK
	label.modulate = Color.WHITE
	label.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	label.vertical_alignment = VERTICAL_ALIGNMENT_CENTER

	# Position at the middle of the zone sector
	var mid_radius = radius / 2.0
	var x = mid_radius * cos(deg_to_rad(angle))
	var z = mid_radius * sin(deg_to_rad(angle))
	label.position = Vector3(x, 5.0, z)  # Floating above ground

	# Make label face the orchestrator (center)
	print("GODOT: Label position: ", label.position)
	label.look_at_from_position(label.position, Vector3(0, 0, 0), Vector3.UP)
	label.rotate_y(PI)
	print("GODOT: Label rotation after rotate_y(PI): ", label.rotation)

	return label

func clear_zones():
	for node in zone_nodes:
		if is_instance_valid(node):
			node.queue_free()
	zone_nodes.clear()

	# Re-enable shader floor if it exists
	var floor = get_node_or_null("../Floor/MeshInstance3D")
	if floor:
		floor.visible = true