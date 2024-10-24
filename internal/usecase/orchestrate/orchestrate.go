package orchestrate

import (
	"fmt"
	"mindpalace/internal/adapter/llmclient"
	"mindpalace/internal/usecase/agents"
)

type Orchestrator struct {
	agents map[string]*agents.Agent
	tasks  []string
}

type LLMResponse struct {
	Request  string
	Response string
}

func NewOrchestrator() *Orchestrator {
	o := &Orchestrator{
		agents: make(map[string]*agents.Agent),
	}

	// Define function implementations
	functionMap := map[string]func(map[string]interface{}) (string, error){
		"add_task":   o.addTask,
		"list_tasks": o.listTasks,
		// Add more functions as needed
	}

	// Define functions for the agent
	functions := []llmclient.FunctionDeclaration{
		{
			Name:        "add_task",
			Description: "Add a task to the task manager",
			Parameters: llmclient.FunctionDeclarationParam{
				Type: "object",
				Properties: map[string]interface{}{
					"task": map[string]interface{}{
						"type":        "string",
						"description": "The task description",
					},
				},
				Required: []string{"task"},
			},
		},
		{
			Name:        "list_tasks",
			Description: "list tasks in the task manager",
			Parameters: llmclient.FunctionDeclarationParam{
				Type: "object",
				Properties: map[string]interface{}{
					"search": map[string]interface{}{
						"type":        "string",
						"description": "task contains string",
					},
				},
				Required: []string{""},
			},
		},
	}

	o.AddAgent(
		agents.NewAgent("mindpalace", `
You're a helpful assistant in the project MindPalace in active mode. Help the user as best you can. You are interact with the user in the most natural way you can, normal chit chat is encouraged but remember that you aren't capable of doing external actions outside of outputting text and calling the configured functions`, "llama3.1", functionMap, functions),
	)
	o.AddAgent(agents.NewAgent("htmlformatter", "repeat back the input with html and inline css for pretty formatting", "llama3.1", nil, nil))
	return o
}

func (o *Orchestrator) addTask(args map[string]interface{}) (string, error) {
	if args != nil {
		var task string
		var ok bool
		task, ok = args["task"].(string)
		if ok {
			o.tasks = append(o.tasks, task)
		}
		return fmt.Sprintf("Task '%s' added successfully. task id = %d", o.tasks[len(o.tasks)-1], len(o.tasks)), nil
	}
	return "no task added", nil
}

func (o *Orchestrator) listTasks(args map[string]interface{}) (string, error) {
	var taskList string
	for i, s := range o.tasks {
		taskList += fmt.Sprintf("%d:%s\n", i, s)
	}
	return taskList, nil
}

func (o *Orchestrator) AddAgent(agent *agents.Agent) {
	o.agents[agent.Name] = agent
}

func (o *Orchestrator) GetAgent(name string) (*agents.Agent, bool) {
	agent, exists := o.agents[name]
	return agent, exists
}

func (o *Orchestrator) CallAgent(agentName, task string) (string, error) {
	agent, exists := o.agents[agentName]
	if !exists {
		return "", fmt.Errorf("agent %s not found", agentName)
	}
	output, err := agent.Call(task)
	if err != nil {
		return "", err
	}
	return output, nil
}
