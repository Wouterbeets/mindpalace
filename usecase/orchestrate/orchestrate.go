package orchestrate

import (
	"fmt"
	"mindpalace/usecase/agents"
	"regexp"
	"strings"
)

type Orchestrator struct {
	agents map[string]*agents.Agent
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		agents: make(map[string]*agents.Agent),
	}
}

func (o *Orchestrator) AddAgent(name, systemPrompt, modelName string) {
	agent := agents.NewAgent(name, systemPrompt, modelName)
	o.agents[name] = agent
}

func (o *Orchestrator) GetAgent(name string) (*agents.Agent, bool) {
	agent, exists := o.agents[name]
	return agent, exists
}

func (o *Orchestrator) CreateAgent(name, systemPrompt, modelName string) *agents.Agent {
	agent := agents.NewAgent(name, systemPrompt, modelName)
	o.agents[name] = agent
	return agent
}

func (o *Orchestrator) CallAgent(agentName, task string) (string, error) {
	agent, exists := o.agents[agentName]
	if !exists {
		return "", fmt.Errorf("agent %s not found", agentName)
	}
	return o.executeChain(agent, task)
}

// parseOutput parses the output of an LLM call to extract tasks and agent names
// The expected format is one or more lines in the format "<agent> @agentName: task</agent>"
func (o *Orchestrator) parseOutput(output string) (tasks []string, agentNames []string) {
	lines := strings.Split(output, "\n")
	agentPattern := regexp.MustCompile(`<agent>\s*@(\w+):\s*(.*?)\s*</agent>`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		matches := agentPattern.FindStringSubmatch(line)
		if len(matches) == 3 {
			agentName := strings.TrimSpace(matches[1])
			task := strings.TrimSpace(matches[2])
			tasks = append(tasks, task)
			agentNames = append(agentNames, agentName)
		}
	}
	return tasks, agentNames
}

func (o *Orchestrator) executeChain(agent *agents.Agent, task string) (string, error) {
	output, err := agent.Call(task)
	if err != nil {
		return "", err
	}
	fmt.Println("agent output:", output)
	var subCommandsExectued bool
	for {
		tasks, agentNames := o.parseOutput(output)
		if len(tasks) == 0 {
			break
		}

		for i, parsedTask := range tasks {
			subCommandsExectued = true
			agentName := agentNames[i]
			var nextAgent *agents.Agent
			var exists bool

			if agentName != "" {
				nextAgent, exists = o.GetAgent(agentName)
				if !exists {
					// Create a new agent with default system prompt and model name
					nextAgent = o.CreateAgent(agentName, "as short as possible output", "llama3:70b")
				}
			} else {
				nextAgent = agent
			}

			subCommandOut, err := nextAgent.Call(parsedTask)
			if err != nil {
				return "", err
			}
			output = subCommandOut
			agent.AddSubCommandResult(agentName, subCommandOut)
		}
	}
	if subCommandsExectued {
		finalOutput, err := agent.Call("evaluate the results of the subcommands and integrate them results in a coherent answer")
		if err != nil {
			return "", err
		}
		return finalOutput, nil
	}
	return output, nil
}
