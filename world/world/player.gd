extends CharacterBody3D

@export var speed: float = 5.0
@export var jump_speed: float = 5.0
@export var mouse_sensitivity: float = 0.002

var gravity: float = ProjectSettings.get_setting("physics/3d/default_gravity")

func _ready():
	Input.mouse_mode = Input.MOUSE_MODE_CAPTURED  # Capture mouse for looking

func _input(event):
	if event is InputEventMouseMotion:
		rotate_y(-event.relative.x * mouse_sensitivity)
		$Camera.rotate_x(-event.relative.y * mouse_sensitivity)
		$Camera.rotation.x = clamp($Camera.rotation.x, -deg_to_rad(70), deg_to_rad(70))

func _physics_process(delta):
	# Add gravity
	if not is_on_floor():
		velocity.y -= gravity * delta

	# Handle jump
	if Input.is_action_just_pressed("ui_accept") and is_on_floor():  # Space for jump
		velocity.y = jump_speed

	# Get input direction (WASD)
	var input_dir := Input.get_vector("ui_left", "ui_right", "ui_up", "ui_down")
	var direction := (transform.basis * Vector3(input_dir.x, 0, input_dir.y)).normalized()
	if direction:
		velocity.x = direction.x * speed
		velocity.z = direction.z * speed
	else:
		velocity.x = move_toward(velocity.x, 0, speed)
		velocity.z = move_toward(velocity.z, 0, speed)

	move_and_slide()
