package agents

import (
	"fmt"
	"os/exec"
	"strings"
)

type TaskManager struct{}

func NewTaskManager() *TaskManager {
	return &TaskManager{}
}

func (tm *TaskManager) PostProcess(query, input string) error {
	fmt.Println("---------------------------------------------------------------------- POST")
	parts := strings.SplitN(input, ",", 3)
	if len(parts) < 2 {
		return fmt.Errorf("invalid input format")
	}

	command := strings.TrimSpace(parts[0])
	project := strings.TrimSpace(parts[1])

	switch command {
	case "add":
		if len(parts) != 3 {
			return fmt.Errorf("invalid add command format")
		}
		description := strings.TrimSpace(parts[2])
		return tm.AddTask(project, description)
	case "update":
		if len(parts) != 3 {
			return fmt.Errorf("invalid update command format")
		}
		description := strings.TrimSpace(parts[2])
		return tm.UpdateTask(project, description)
	case "remove":
		return tm.RemoveTask(project)
	case "list":
		return tm.ListTasks(project)
	default:
		return fmt.Errorf("unknown command")
	}
}

func (tm *TaskManager) AddTask(project, description string) error {
	cmd := exec.Command("task", "add", fmt.Sprintf("project:%s", project), description)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error adding task: %w\nOutput: %s", err, output)
	}
	fmt.Printf("Task added to %s: %s\n", project, description)
	return nil
}

func (tm *TaskManager) UpdateTask(project, newDescription string) error {
	// First, we need to find the UUID of the task(s) in the project
	listCmd := exec.Command("task", fmt.Sprintf("project:%s", project), "export")
	listOutput, err := listCmd.Output()
	if err != nil {
		return fmt.Errorf("error listing tasks: %w", err)
	}

	// This is a simplification. In a real-world scenario, you'd want to parse the JSON output
	// and handle multiple tasks in a project more gracefully.
	if len(listOutput) == 0 {
		return fmt.Errorf("no tasks found in project %s", project)
	}

	// Update the most recently added task in the project
	updateCmd := exec.Command("task", fmt.Sprintf("project:%s", project), "modify", newDescription)
	updateOutput, err := updateCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error updating task: %w\nOutput: %s", err, updateOutput)
	}
	fmt.Printf("Task updated in %s: %s\n", project, newDescription)
	return nil
}

func (tm *TaskManager) RemoveTask(project string) error {
	cmd := exec.Command("task", fmt.Sprintf("project:%s", project), "delete")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error removing tasks: %w\nOutput: %s", err, output)
	}
	fmt.Printf("Tasks removed from %s\n", project)
	return nil
}

func (tm *TaskManager) ListTasks(project string) error {
	cmd := exec.Command("task", fmt.Sprintf("project:%s", project), "list")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error listing tasks: %w\nOutput: %s", err, output)
	}
	fmt.Printf("Tasks in %s:\n%s", project, string(output))
	return nil
}
