package main

import (
	"fmt"
	"strings"
	"time"
)

// Task represents a single task's state (mirrored from plugin.go for Templ)
type Task struct {
	TaskID          string    `json:"task_id"`
	Title           string    `json:"title"`
	Description     string    `json:"description,omitempty"`
	Status          string    `json:"status"`
	Priority        string    `json:"priority"`
	Deadline        time.Time `json:"deadline,omitempty"`
	Dependencies    []string  `json:"dependencies,omitempty"`
	Tags            []string  `json:"tags,omitempty"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	CompletionNotes string    `json:"completion_notes,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type templComponent struct {
	content string
}

func (t templComponent) String() string {
	return t.content
}

func TasksPage(tasks []*Task) templComponent {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Tasks - MindPalace Web</title>
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
</head>
<body>
    <h1>Tasks</h1>
    <button hx-get="/tasks" hx-target="#task-list">Refresh Tasks</button>
    <div id="task-list">`)
	if len(tasks) == 0 {
		sb.WriteString(`        <p>No tasks available.</p>`)
	} else {
		sb.WriteString(`        <ul>`)
		for _, task := range tasks {
			sb.WriteString(fmt.Sprintf(`            <li><strong>%s</strong> - %s (Status: %s, Priority: %s)</li>`, task.Title, task.Description, task.Status, task.Priority))
		}
		sb.WriteString(`        </ul>`)
	}
	sb.WriteString(`    </div>
    <a href="/">Back to Home</a>
</body>
</html>`)
	return templComponent{content: sb.String()}
}
