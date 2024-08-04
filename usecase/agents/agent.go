package agents

import (
	"fmt"
	"log"
	"mindpalace/adapter/llmclient"
	"mindpalace/usecase/chat"
)

type Agent struct {
	Name         string
	SystemPrompt string
	conversation *chat.Conversation
	client       *llmclient.Client
}

func NewAgent(name, systemPrompt string, modelName string) *Agent {
	client := llmclient.NewClient("http://mindpalace.hopto.org/api/chat", modelName)
	conversation := chat.NewConversation()
	conversation.Add("system", systemPrompt)
	return &Agent{
		Name:         name,
		SystemPrompt: systemPrompt,
		client:       client,
		conversation: conversation,
	}
}

func (a *Agent) Call(task string) (string, error) {
	fmt.Println("in", a.Name, task)
	a.conversation.Add("user", task)
	var conversation []llmclient.Message
	for _, m := range a.conversation.History {
		conversation = append(conversation, llmclient.Message{
			Role:    m.Participant,
			Content: m.Contribution,
		})
	}
	response, err := a.client.Prompt(conversation)
	if err != nil {
		return "", err
	}

	var out string
	var responseText string
	var done bool
	fmt.Println("starting output read", a.Name)
	for responseText, done, err = response.ReadNext(); !done && err == nil; responseText, done, err = response.ReadNext() {
		out += responseText
		fmt.Printf("%s", responseText)
	}
	if err != nil {
		log.Println(err)
		return "", err
	}
	a.conversation.Add("assistant", out)
	return out, err
}

func (a *Agent) AddSubCommandResult(name, result string) {
	a.conversation.Add(name, result)
}
