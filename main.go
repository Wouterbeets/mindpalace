package main

import (
	"fmt"
	"html/template"
	"io"
	"mindpalace/usecase/orchestrate"
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
	Response template.HTML
}

func main() {
	e := echo.New()

	fmt.Println("checks if running")
	e.Renderer = newTemplate()
	e.GET("/", func(c echo.Context) error {
		fmt.Println("in index")
		return c.Render(http.StatusOK, "index", nil)
	})
	o := orchestrate.NewOrchestrator()
	o.AddAgent(
		"activeMode",
		"You're a helpful assistant in the project mindpalace in active mode, help the user as best you can. Delegate work to async agents processing by calling an agent with @agentname: task",
		"llama3",
	)
	o.AddAgent("htmxFormater", "You're a helpful htmx formatting assistant in the project mindpalace, help the user by formatting all the text that follows as pretty and usefull as possible, your output is diecrlty displayed as a subset of an html page, so output ONLY valid html", "codestral")

	e.POST("/send", func(c echo.Context) error {
		userMessage := c.FormValue("chatinput")
		resp, err := o.CallAgent("activeMode", userMessage)
		if err != nil {
			return err
		}
		resp, err = o.CallAgent("htmxFormater", resp)
		if err != nil {
			return err
		}
		return c.Render(http.StatusOK, "chat", LLMResponse{Request: userMessage, Response: template.HTML(resp)})
	})

	fmt.Println("Server started at :8080")
	e.Logger.Fatal(e.Start(":8080"))
}
