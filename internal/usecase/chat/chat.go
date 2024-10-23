package chat

import "mindpalace/internal/adapter/llmclient"

type Message struct {
	Participant  string
	Contribution string
}

type Conversation struct {
	History []Message
}

func NewConversation() *Conversation {
	return &Conversation{
		History: []Message{},
	}
}

func (c *Conversation) Add(participant, contribution string) {
	if contribution == "" {
		return
	}
	c.History = append(c.History, Message{
		Participant:  participant,
		Contribution: contribution,
	})
}

func (c *Conversation) ToLLMMessages() []llmclient.Message {
	messages := make([]llmclient.Message, len(c.History))
	for i, msg := range c.History {
		messages[i] = llmclient.Message{
			Role:    msg.Participant,
			Content: msg.Contribution,
		}
	}
	return messages
}
