package orchestrate

import (
	"encoding/json"
	"fmt"
	"log"
	"mindpalace/usecase/agents"
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
		agents.NewAgent("activeMode", `
                    You're a helpful assistant in the project mindpalace in active mode. Help the user as best you can. 
                    When you need to delegate work to other agents, include a "function_calls" array in your JSON response.
                    Each function call should have a "name" (the agent's name) and "arguments" (the task for that agent).

                    Available agents:
                    - taskmanager: add, update, remove, and list tasks
                    - updateself: read and write sourcecode of mindpalace

                    Your response should always be in this JSON format:
                    {
                        "content": "Your response to the user",
                        "function_calls": [
                            {
                                "name": "agentName",
                                "arguments": "task for the agent"
                            }
                        ]
                    }
                    `, "llama3.1", nil),
	)
	o.AddAgent(agents.NewAgent("taskmanager", "You are the taskmanager, you will receive requests to manage the task lists, you are able to preformthe following actions: add, update, remove, and list tasks. Your job is to interpret the request and transform it function calls like so: on a newline:``` <action>, <todolist>, <task> ``` example: ```add, groceries, buy milk```", "llama3.1", agents.NewTaskManager()))
	o.AddAgent(agents.NewAgent("htmxFormater", "You're a helpful htmx formatting assistant in the project mindpalace, help the user by formatting all the text that follows as pretty and usefull as possible but keep the context identical. Add css inline of the html. The output is DIRECTLY INSERTED into the html page, OUTPUT ONLY html", "llama3.1", nil))
	return o
}

func (o *Orchestrator) AddAgent(agent *agents.Agent) {
	o.agents[agent.Name] = agent
}

func (o *Orchestrator) GetAgent(name string) (*agents.Agent, bool) {
	agent, exists := o.agents[name]
	return agent, exists
}

func (o *Orchestrator) CreateAgent(name, systemPrompt, modelName string) *agents.Agent {
	agent := agents.NewAgent(name, systemPrompt, modelName, nil)
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

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type AIResponse struct {
	Content       string         `json:"content"`
	FunctionCalls []FunctionCall `json:"function_calls"`
}

func (o *Orchestrator) parseOutput(output string) (content string, tasks []string, agentNames []string, err error) {
	var response AIResponse
	err = json.Unmarshal([]byte(output), &response)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to parse AI output: %v", err)
	}

	content = response.Content
	for _, call := range response.FunctionCalls {
		tasks = append(tasks, call.Arguments)
		agentNames = append(agentNames, call.Name)
	}

	return content, tasks, agentNames, nil
}

func (o *Orchestrator) executeChain(agent *agents.Agent, task string) (string, error) {
	log.Println("Entering executeChain function")
	log.Printf("Initial task: %s", task)

	log.Println("Calling initial agent")
	output, err := agent.Call(task)
	if err != nil {
		log.Printf("Error in initial agent call: %v", err)
		return "", err
	}
	log.Printf("Initial agent output: %s", output)

	var subCommandsExecuted bool
	var agentOutputs string
	iterationCount := 0

	for {
		iterationCount++
		log.Printf("Starting iteration %d of main loop", iterationCount)

		log.Println("Parsing output")
		content, tasks, agentNames, err := o.parseOutput(output)
		if err != nil {
			log.Printf("Error parsing output: %v", err)
			return "", fmt.Errorf("failed to parse output: %v", err)
		}
		log.Printf("Parsed content: %s", content)
		log.Printf("Parsed tasks: %v", tasks)
		log.Printf("Parsed agent names: %v", agentNames)

		if len(tasks) == 0 {
			log.Println("No tasks found")
			if content != "" {
				log.Println("Returning content as no tasks were found")
				return content, nil
			}
			log.Println("Breaking main loop as no tasks or content were found")
			break
		}

		subCommandsExecuted = true
		for i, parsedTask := range tasks {
			log.Printf("Processing task %d: %s", i, parsedTask)
			agentName := agentNames[i]
			var nextAgent *agents.Agent
			var exists bool

			if agentName != "" {
				log.Printf("Looking for agent: %s", agentName)
				nextAgent, exists = o.GetAgent(agentName)
				if !exists {
					log.Printf("Agent %s not found, creating new agent", agentName)
					nextAgent = o.CreateAgent(agentName, "be concise", "llama3.1")
				}
			} else {
				log.Println("Using original agent for this task")
				nextAgent = agent
			}

			log.Printf("Calling agent %s with task: %s", nextAgent.Name, parsedTask)
			subOutput, err := nextAgent.Call(parsedTask)
			if err != nil {
				log.Printf("Error in agent %s call: %v", nextAgent.Name, err)
				return "", err
			}
			log.Printf("Agent %s output: %s", nextAgent.Name, subOutput)

			agentOutputs += fmt.Sprintf("<%s>%s</%s>", nextAgent.Name, subOutput, nextAgent.Name)
		}

		log.Println("Updating output with agent results")
		output = agentOutputs
		agentOutputs = "" // Reset agentOutputs for the next iteration
		log.Printf("Updated output: %s", output)
	}

	if subCommandsExecuted {
		log.Println("Sub-commands were executed, evaluating results")
		evaluationPrompt := "evaluate the results of the agents calls and integrate them into a coherent answer: " + output
		log.Printf("Evaluation prompt: %s", evaluationPrompt)

		output, err = agent.Call(evaluationPrompt)
		if err != nil {
			log.Printf("Error in final evaluation call: %v", err)
			return "", err
		}
		log.Printf("Final evaluation output: %s", output)
	} else {
		log.Println("No sub-commands were executed")
	}

	log.Println("Exiting executeChain function")
	return output, nil
}
