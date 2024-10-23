package agents

import (
	"fmt"
	"strings"
)

type TaskManager struct {
	tasks map[string][]string
}

func NewTaskManager() *TaskManager {
	return &TaskManager{
		tasks: make(map[string][]string),
	}
}

func (tm *TaskManager) AddTask(args string) (string, error) {
	parts := strings.SplitN(args, ",", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid arguments for AddTask")
	}
	listName := strings.TrimSpace(parts[0])
	task := strings.TrimSpace(parts[1])

	tm.tasks[listName] = append(tm.tasks[listName], task)
	return fmt.Sprintf("Task '%s' added to list '%s'.", task, listName), nil
}

func (tm *TaskManager) ListTasks(listName string) (string, error) {
	tasks, exists := tm.tasks[listName]
	if !exists || len(tasks) == 0 {
		return fmt.Sprintf("No tasks in list '%s'.", listName), nil
	}

	return fmt.Sprintf("Tasks in '%s':\n- %s", listName, strings.Join(tasks, "\n- ")), nil
}
