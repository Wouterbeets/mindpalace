package main

import (
    "fmt"
    "net/http"
    "html/template"
    "mindpalace/adapter/llmclient"
)

var tmpl = `
<!DOCTYPE html>
<html>
<head>
    <title>MindPalace HTMX Demo</title>
    <script src="https://unpkg.com/htmx.org@1.7.0"></script>
</head>
<body>
    <div id="content" hx-swap="outerHTML" hx-get="/update" hx-trigger="load"></div>
</body>
</html>
`

type RequestData struct {
    Model string `json:"model"`
    Prompt string `json:"prompt"`
}

type ResponseData struct {
    Output string `json:"output"`
}


func runInference(input string) string {
    client:=llmclient.NewClient("http://localhost:11434/api/generate", "mixtral")
    var out string
    response := client.Prompt(input)
    for responseText, done, err := response.ReadNext(); !done && err == nil; responseText, done, err = response.ReadNext() {
    	out += responseText
    }
    return out
}

func main() {
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        t, _ := template.New("index").Parse(tmpl)
        t.Execute(w, nil)
    })

    http.HandleFunc("/update", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, `<div id="content">Welcome to MindPalace! <button hx-get="/chat">Start Chat</button></div>`)
    })

    http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, `<div id="content">
            <h2>Chat Window</h2>
            <div id="chatbox">
                <!-- Chat messages go here -->
            </div>
            <input type="text" id="chatinput" hx-post="/send" hx-swap="beforeend">
            </div>`)
    })

    http.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
        r.ParseForm()
        userMessage := r.FormValue("chatinput")
        aiResponse := runInference(userMessage)
        fmt.Fprintf(w, `<div class="message">You: %s</div><div class="message">AI: %s</div>`, userMessage, aiResponse)
    })

    fmt.Println("Server started at :8080")
    http.ListenAndServe(":8080", nil)
}
