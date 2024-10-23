package agents

import (
	"errors"
	"fmt"
	"log"
	"mindpalace/internal/adapter/llmclient"
	"mindpalace/internal/usecase/chat"
)

type Agent struct {
	Name         string
	SystemPrompt string
	conversation *chat.Conversation
	client       *llmclient.Client
	functions    []llmclient.FunctionDeclaration
	functionMap  map[string]func(map[string]interface{}) (string, error)
}

func NewAgent(name, systemPrompt, modelName string, functionMap map[string]func(map[string]interface{}) (string, error), functions []llmclient.FunctionDeclaration) *Agent {
	client := llmclient.NewClient("http://127.0.0.1:11434/api/chat", modelName)
	conversation := chat.NewConversation()
	conversation.Add("system", systemPrompt)
	return &Agent{
		Name:         name,
		SystemPrompt: systemPrompt,
		client:       client,
		conversation: conversation,
		functions:    functions,
		functionMap:  functionMap,
	}
}

func (a *Agent) Call(task string) (string, error) {
	log.Printf("Agent '%s' processing task: %s", a.Name, task)
	a.conversation.Add("user", task)

	for {
		fmt.Println("looping")
		conversation := a.conversation.ToLLMMessages()
		response, err := a.client.Prompt(conversation, a.functions)
		if err != nil {
			return "", fmt.Errorf("LLM client error: %w", err)
		}
		fmt.Println(response)

		if response.ToolCalls != nil {
			for _, call := range response.ToolCalls {
				fmt.Println("in tool call loop", call.Name, call.Arguments)
				toolName := call.Name
				arguments := call.Arguments
				if fn, exists := a.functionMap[toolName]; exists {
					result, err := fn(arguments)
					if err != nil {
						log.Printf("Function '%s' error: %v", toolName, err)
						return "", err
					}
					// Add function result to conversation
					a.conversation.Add("tool", result)
					continue // Break to outer loop to continue the conversation
				} else {
					log.Printf("Function '%s' not found", toolName)
					return "", fmt.Errorf("function '%s' not found", toolName)
				}
			}
			// Handle function call
		} else if response.Content != "" {
			// Assistant provided a content response
			a.conversation.Add("assistant", response.Content)
			return response.Content, nil
		} else {
			return "", errors.New("No response from llm")
		}
	}
}
