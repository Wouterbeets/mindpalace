package orchestrate

import (
	"fmt"
	"log"
	"mindpalace/internal/adapter/llmclient"
	"mindpalace/internal/usecase/agents"
)

type Orchestrator struct {
	agents map[string]*agents.Agent
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
		"add_task": o.addTask,
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
		// Add more functions as needed
	}

	o.AddAgent(
		agents.NewAgent("activeMode", `
You're a helpful assistant in the project MindPalace in active mode. Help the user as best you can. After having executed a function successfully please make a summary of the actions taken so the user get feedback
`, "llama3.1", functionMap, functions),
	)
	o.AddAgent(agents.NewAgent("htmlformatter", "repeat the input with html and inline css for pretty formatting", "llama3.1", nil, nil))
	// Add other agents as needed
	return o
}

func (o *Orchestrator) addTask(args map[string]interface{}) (string, error) {
	task := args["task"]
	// Implement your task addition logic here
	log.Printf("Adding task: %s", task)
	// For simplicity, we'll just return a confirmation message
	return fmt.Sprintf("Task '%s' added successfully.", task), nil
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
	return o.executeChain(agent, task)
}

func (o *Orchestrator) executeChain(agent *agents.Agent, task string) (string, error) {
	output, err := agent.Call(task)
	if err != nil {
		return "", err
	}
	log.Printf("Raw LLM Output: %s", output)
	return output, nil
}

func (o *Orchestrator) handleFunctionCall(functionName string, arguments map[string]interface{}) (string, error) {
	// Implement your function handling logic here
	// For example, call a specific agent or perform an action
	switch functionName {
	case "add_task":
		// Handle adding a task
		taskDescription := arguments["task"].(string)
		// Perform the task addition logic
		return fmt.Sprintf("Task '%s' added successfully.", taskDescription), nil
	default:
		return "", fmt.Errorf("unknown function: %s", functionName)
	}
}
