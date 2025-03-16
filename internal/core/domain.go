package core

import (
	"regexp"
	"strings"
)

// ChatMessage represents a single entry in the chat history
type ChatMessage struct {
	Role              string
	Content           string
	RequestID         string // To associate messages with requests
	StreamingComplete bool   // Indicates if streaming for this message is complete
}

// parseResponseText extracts think tags and regular text from LLM responses
func ParseResponseText(responseText string) (thinks []string, regular string) {
	re := regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	matches := re.FindAllStringSubmatch(responseText, -1)
	for _, match := range matches {
		thinks = append(thinks, match[1])
	}
	regular = re.ReplaceAllString(responseText, "")
	return thinks, strings.TrimSpace(regular)
}