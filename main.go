package main

import (
	"fmt"
	"html/template"
	"io"
	"mindpalace/adapter/llmclient"
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

func runInference(input string) LLMResponse {
	client := llmclient.NewClient("http://localhost:11434/api/generate", "llama3")
	fmt.Println("called run inference")
	var out string
	response := client.Prompt(input)
	for responseText, done, err := response.ReadNext(); !done && err == nil; responseText, done, err = response.ReadNext() {
		out += responseText
	}
	return LLMResponse{Request: input, Response: out}
}

func main() {
	e := echo.New()

	fmt.Println("checks if running")
	e.Renderer = newTemplate()
	e.GET("/", func(c echo.Context) error {
		fmt.Println("in index")
		return c.Render(http.StatusOK, "index", nil)
	})

	e.POST("/send", func(c echo.Context) error {
		userMessage := c.FormValue("chatinput")
		aiResponse := runInference(userMessage)
		return c.Render(http.StatusOK, "index", aiResponse)
	})

	fmt.Println("Server started at :8080")
	e.Logger.Fatal(e.Start(":8080"))
}
