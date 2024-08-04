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
	e.POST("/send", func(c echo.Context) error {
		userMessage := c.FormValue("chatinput")
		resp, err := o.CallAgent("activeMode", userMessage)
		if err != nil {
			return err
		}
		fmt.Println("resp from activemode:", resp)
		resp, err = o.CallAgent("htmxFormater", resp)
		if err != nil {
			return err
		}
		fmt.Println("finished")
		return c.Render(http.StatusOK, "chat", LLMResponse{Request: userMessage, Response: template.HTML(resp)})
	})

	fmt.Println("Server started at :8080")
	e.Logger.Fatal(e.Start(":8080"))
}
