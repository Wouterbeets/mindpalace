package chat

import (
	"errors"
	"fmt"
)

// Message represents a message in the conversation.
type Message struct {
	Participant  string
	Contribution string
}

// Conversation holds the history of a chat conversation.
type Conversation struct {
	History []Message
}

// NewConversation creates a new conversation instance.
func NewConversation() *Conversation {
	return &Conversation{
		History: []Message{},
	}
}

// AddResponse adds a response to the conversation history.
func (c *Conversation) Add(participant, contribution string) error {
	if contribution == "" {
		return errors.New("response cannot be empty")
	}
	message := Message{
		Participant:  participant,
		Contribution: contribution,
	}
	c.History = append(c.History, message)
	return nil
}

// PrintHistory prints the conversation history.
func (c *Conversation) PrintHistory() {
	for _, message := range c.History {
		fmt.Printf("[%s]: %s\n", message.Participant, message.Contribution)
	}
}
