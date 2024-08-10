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

type LLMResponse struct {
	Request  string
	Response string
}

func NewOrchestrator() *Orchestrator {
	o := &Orchestrator{
		agents: make(map[string]*agents.Agent),
	}
	o.AddAgent(
		"activeMode",
		`You're a helpful assistant in the project mindpalace in active mode,
		help the user as best you can. Delegate work to async agents processing by calling an agent on a newline with <agent> @agentname: content</agent>
		avaiable agents:
		taskmanager - add, update, remove tasks.
		tasklister - list all tasks in a list, it will also output priority and labels
		updateself - read, and write sourcecode of mindpalace

		if no suitable agent is present in the list, invent one and it will be created dynamically
		`,
		"mixtral",
	)
	o.AddAgent("taskmanager", "You are the taskmanager, you will reveive commands add, update, remove tasks from todo lists. You manage this by calling functions like so on a newline:``` <name>, <todolist>, <task> ``` example: ```add, groceries, buy milk```", "mixtral")
	o.AddAgent("htmxFormater", "You're a helpful htmx formatting assistant in the project mindpalace, help the user by formatting all the text that follows as pretty and usefull as possible but keep the context identical. Add css inline of the html. The output is DIRECTLY INSERTED into the html page, OUTPUT ONLY html", "mixtral")
	return o
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
	var subCommandsExectued bool
	var agentOutputs string
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
					nextAgent = o.CreateAgent(agentName, "be consise", "mixtral")
				}
			} else {
				nextAgent = agent
			}

			subOutput, err := nextAgent.Call(parsedTask)
			if err != nil {
				return "", err
			}
			agentOutputs += "<" + nextAgent.Name + ">" + subOutput + "</" + nextAgent.Name + ">"
		}
	}
	if subCommandsExectued {
		fmt.Println("------------- evaluating results--------------------------------------")
		output, err = agent.Call("evaluate the results of the agents calls and integrate them into a coherent answer: " + agentOutputs)
		if err != nil {
			return "", err
		}
		return output, nil
	}
	return output, nil
}
