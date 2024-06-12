package llmclient

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// Message represents a single message in the conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is a struct for encoding the JSON request body.
type Request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

// Response represents a stream of responses from the LLM.
type Response struct {
	Model     string
	CreatedAt string
	Reader    *bufio.Reader
	stream    io.ReadCloser
}

// Client handles communication with the LLM server.
type Client struct {
	Endpoint string
	Model    string
}

// NewClient creates a new Client with the specified endpoint and model.
func NewClient(endpoint, model string) *Client {
	return &Client{
		Endpoint: endpoint,
		Model:    model,
	}
}

// Prompt sends a conversation history to the LLM and returns a Response.
func (c *Client) Prompt(conversation []Message) *Response {
	requestData := Request{
		Model:    c.Model,
		Messages: conversation,
	}

	requestBody, err := json.Marshal(requestData)
	if err != nil {
		log.Fatalf("Failed to marshal request data: %v", err)
	}
	fmt.Println("prompt", string(requestBody))

	req, err := http.NewRequest("POST", c.Endpoint, bytes.NewBuffer(requestBody))
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}

	return &Response{
		Reader: bufio.NewReader(resp.Body),
		stream: resp.Body,
	}
}

// ReadNext reads the next part of the response stream.
func (r *Response) ReadNext() (string, bool, error) {
	line, err := r.Reader.ReadBytes('\n')
	if err == io.EOF {
		r.stream.Close()
		return "", false, nil // end of stream
	}
	if err != nil {
		return "", false, err
	}

	var response struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		return "", false, err
	}

	return response.Response, response.Done, nil
}
