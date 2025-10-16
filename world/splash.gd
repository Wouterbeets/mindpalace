extends Node2D

func _ready():
	# Wait for input to continue
	set_process_input(true)

func _input(event):
	if event is InputEventMouseButton and event.pressed and event.button_index == MOUSE_BUTTON_LEFT:
		continue_to_main()
	elif event is InputEventKey and event.pressed and event.keycode == KEY_ENTER:
		continue_to_main()

func continue_to_main():
	# Change to main scene
	get_tree().change_scene_to_file("res://world.tscn")