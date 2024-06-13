package main

import (
	"fmt"
	"html/template"
	"io"
	"mindpalace/adapter/llmclient"
	"mindpalace/usecase/chat"
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
	convo := chat.NewConversation()

	e.POST("/send", func(c echo.Context) error {
		userMessage := c.FormValue("chatinput")

		var conversation []llmclient.Message
		convo.AddUserQuery(userMessage)

		for _, m := range convo.History {
			role := "user"
			if m.Sender != "user" {
				role = "assistant"
			}
			conversation = append(conversation, llmclient.Message{
				Role:    role,
				Content: m.Content,
			})
		}
		aiResponse := runInference(conversation)
		return c.Render(http.StatusOK, "chat", aiResponse)
	})

	fmt.Println("Server started at :8080")
	e.Logger.Fatal(e.Start(":8080"))
}
