package main

import (
	"log"
	"mindpalace/internal/usecase/orchestrate"
	"mindpalace/views"
	"net/http"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	orchestrator := orchestrate.NewOrchestrator()

	e.GET("/", handleIndex)
	e.POST("/send", func(c echo.Context) error {
		return handleSend(c, orchestrator)
	})

	log.Println("Server started at :8080")
	if err := e.Start(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

func handleIndex(c echo.Context) error {
	return render(c, views.Index())
}

func handleSend(c echo.Context, orchestrator *orchestrate.Orchestrator) error {
	userMessage := c.FormValue("chatinput")
	if userMessage == "" {
		return c.String(http.StatusBadRequest, "Message cannot be empty.")
	}

	response, err := orchestrator.CallAgent("activeMode", userMessage)
	if err != nil {
		log.Println("Error calling agent:", err)
		return c.String(http.StatusInternalServerError, "Internal Server Error")
	}

	//response, err = orchestrator.CallAgent("htmlformatter", response)
	//if err != nil {
	//log.Println("Error calling agent:", err)
	//return c.String(http.StatusInternalServerError, "Internal Server Error")
	//}

	return render(c, views.ChatContent(orchestrate.LLMResponse{Request: userMessage, Response: response}))
}

func render(c echo.Context, component templ.Component) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	return component.Render(c.Request().Context(), c.Response())
}
