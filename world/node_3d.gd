extends HTTPRequest

func _ready():
	print("Server is ready to handle requests")

func _on_request_completed(result, response_code, headers, body):
	if response_code == 200:
		print("Request received successfully.")
	else:
		print("Request failed: ", response_code)

# Function to handle incoming requests, for example on port 8080
func _process(delta):
	var server = HTTPServer.new()
	server.listen(8080)
	if server.is_connection_available():
		var client = server.take_connection()
		var request = client.read_request()
		if request:
			client.send_response(200, "OK", [], "Hello from Godot")
