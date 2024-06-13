package main

import (
	"fmt"
	"html/template"
	"io"
	"mindpalace/adapter/llmclient"
	"mindpalace/usecase/agents"
	"net/http"

	"github.com/labstack/echo/v4"
)

type Templates struct {
	templates *template.Template
}

func (t Templates) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func newTemplate() *Templates {
	return &Templates{
		templates: template.Must(template.ParseGlob("views/*.html")),
	}
}

type LLMResponse struct {
	Request  string
	Response string
}

func runInference(input []llmclient.Message) LLMResponse {
	client := llmclient.NewClient("http://localhost:11434/api/chat", "llama3")
	fmt.Println("called run inference")
	var out string
	response := client.Prompt(input)
	for responseText, done, err := response.ReadNext(); !done && err == nil; responseText, done, err = response.ReadNext() {
		out += responseText
	}
	lastResponse := input[len(input)-1]
	return LLMResponse{Request: lastResponse.Content, Response: out}
}

func main() {
	e := echo.New()

	fmt.Println("checks if running")
	e.Renderer = newTemplate()
	e.GET("/", func(c echo.Context) error {
		fmt.Println("in index")
		return c.Render(http.StatusOK, "index", nil)
	})
	activeMode := agents.NewAgent(
		"activeMode",
		"You're a helpful assistant in the project mindpalace in active mode, help the user as best you can. Delegate work to async agents processing by calling an agent with @agentname: task",
		"llama3",
	)

	e.POST("/send", func(c echo.Context) error {
		userMessage := c.FormValue("chatinput")
		resp, err := activeMode.Call(userMessage)
		if err != nil {
			return err
		}
		return c.Render(http.StatusOK, "chat", LLMResponse{Request: userMessage, Response: resp})
	})

	fmt.Println("Server started at :8080")
	e.Logger.Fatal(e.Start(":8080"))
}
