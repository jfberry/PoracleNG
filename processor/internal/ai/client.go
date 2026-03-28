// Package ai provides an OpenAI-compatible chat completion client for
// translating natural language into Poracle tracking commands.
//
// Supports any backend that implements the OpenAI chat completions API:
// Ollama (local), OpenRouter, OpenAI, Groq, Together AI, LM Studio, etc.
package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// Client talks to an OpenAI-compatible chat completions endpoint.
type Client struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// New creates a new AI client. Returns nil if not configured.
func New(providerURL, apiKey, model string) *Client {
	if providerURL == "" || model == "" {
		return nil
	}
	return &Client{
		baseURL: strings.TrimRight(providerURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// chatRequest is the OpenAI chat completions request format.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the OpenAI chat completions response format.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// TranslateCommand sends a natural language request to the AI model and
// returns the suggested Poracle command(s).
func (c *Client) TranslateCommand(userRequest string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("AI not configured")
	}

	req := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userRequest},
		},
		Temperature: 0.1, // low temperature for deterministic command output
		MaxTokens:   256,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	start := time.Now()
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	log.Debugf("AI request took %dms, model=%s", time.Since(start).Milliseconds(), c.model)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response from model")
	}

	return strings.TrimSpace(chatResp.Choices[0].Message.Content), nil
}
