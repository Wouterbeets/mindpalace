package llmclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Message struct {
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	ToolCalls []FunctionBlock `json:"tool_calls"`
}

type FunctionDeclaration struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Parameters  FunctionDeclarationParam `json:"parameters"`
}

type FunctionDeclarationParam struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required"`
}

type FunctionBlock struct {
	FunctionRequestFromLLM `json:"function"`
}

type FunctionRequestFromLLM struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// Define a new Tool structure to match the API expectation
type Tool struct {
	Type     string              `json:"type"`
	Function FunctionDeclaration `json:"function"`
}

type Request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Options  *Options  `json:"options,omitempty"`
	Tools    []Tool    `json:"tools,omitempty"` // Updated to use "tools" instead of "functions"
}

type Options struct {
	ContextLength int `json:"num_ctx"`
}

// Updated Response struct to include FunctionCall
type Response struct {
	Content   string          `json:"content,omitempty"`
	ToolCalls []FunctionBlock `json:"function_call,omitempty"` // Add FunctionCall to capture function details
}

type Client struct {
	Endpoint string
	Model    string
}

func NewClient(endpoint, model string) *Client {
	return &Client{
		Endpoint: endpoint,
		Model:    model,
	}
}

func (c *Client) Prompt(conversation []Message, functions []FunctionDeclaration) (*Response, error) {
	// Convert each function to a tool with "type": "function"
	var tools []Tool
	for _, function := range functions {
		tools = append(tools, Tool{
			Type:     "function", // Ensure the type is "function"
			Function: function,
		})
	}

	requestData := Request{
		Model:    c.Model,
		Messages: conversation,
		Tools:    tools, // Updated to use "tools"
	}

	requestBody, err := json.Marshal(requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request data: %w", err)
	}

	req, err := http.NewRequest("POST", c.Endpoint, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close() // Ensure the response body is closed after reading

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected response status: %s", resp.Status)
	}

	// Read the entire response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	fmt.Println("bodyBytes", string(bodyBytes))

	// Parse the JSON response to extract content and function call
	var parsedResponse struct {
		Message Message `json:"message,omitempty"`
	}

	err = json.Unmarshal(bodyBytes, &parsedResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	fmt.Println("parsedResponse", parsedResponse)
	// Create the Response object
	response := &Response{
		Content:   parsedResponse.Message.Content, // Default content to empty unless explicitly provided
		ToolCalls: parsedResponse.Message.ToolCalls,
	}
	return response, nil
}
