package main

import (
	"fmt"
	"html/template"
	"mindpalace/adapter/llmclient"
	"net/http"
)

const tmpl = `
<!DOCTYPE html>
<html>
<head>
    <title>MindPalace</title>
    <script src="https://unpkg.com/htmx.org@1.6.1"></script>
    <script>htmx.logAll();</script>
</head>
<body hx-boost="true">
    <div id="content">
        <h2>Chat Window</h2>
        <div id="chatbox">
            <!-- Chat messages go here -->
        </div>
        <form id="chatForm" hx-post="/send" hx-trigger="keyup[enter]" hx-swap="beforeend" hx-target="#chatbox">
            <input type="text" id="chatinput" name="chatinput">
        </form>
    </div>
</body>
</html>
`

func runInference(input string) string {
	client := llmclient.NewClient("http://localhost:11434/api/generate", "llama3")
	fmt.Println("called run inference")
	var out string
	response := client.Prompt(input)
	for responseText, done, err := response.ReadNext(); !done && err == nil; responseText, done, err = response.ReadNext() {
		fmt.Println("out", out)
		out += responseText
	}
	return out
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t, _ := template.New("index").Parse(tmpl)
		t.Execute(w, nil)
	})

	http.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		fmt.Println("send called")
		userMessage := r.FormValue("chatinput")
		fmt.Println("User message:", userMessage) // Added logging
		aiResponse := runInference(userMessage)
		fmt.Println("AI response:", aiResponse) // Added logging
		fmt.Fprintf(w, `<div class="message">You: %s</div><div class="message">AI: %s</div>`, userMessage, aiResponse)
	})

	fmt.Println("Server started at :8080")
	http.ListenAndServe(":8080", nil)
}
