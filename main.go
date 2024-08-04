package main

import (
	"fmt"
	"mindpalace/usecase/orchestrate"
	"mindpalace/views"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

func main() {
	e := echo.New()

	fmt.Println("checks if running")

	e.GET("/", func(c echo.Context) error {
		fmt.Println("in index")
		return render(c, views.Index())
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
		return render(c, views.ChatContent(orchestrate.LLMResponse{Request: userMessage, Response: resp}))
	})

	fmt.Println("Server started at :8080")
	e.Logger.Fatal(e.Start(":8080"))
}

func render(c echo.Context, component templ.Component) error {
	return component.Render(c.Request().Context(), c.Response().Writer)
}

type LLMResponse struct {
	Request  string
	Response templ.Raw
}
