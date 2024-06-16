package agents

import (
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
	client := llmclient.NewClient("http://192.168.1.49:8000/api/chat", modelName)
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
	a.conversation.Add("user", task)
	var conversation []llmclient.Message
	for _, m := range a.conversation.History {
		conversation = append(conversation, llmclient.Message{
			Role:    m.Participant,
			Content: m.Contribution,
		})
	}
	response := a.client.Prompt(conversation)

	var out string
	var err error
	var responseText string
	var done bool
	for responseText, done, err = response.ReadNext(); !done && err == nil; responseText, done, err = response.ReadNext() {
		out += responseText
	}
	return out, err
}
