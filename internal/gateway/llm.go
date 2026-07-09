// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ToolParamSchema struct {
	Type       string                   `json:"type"`
	Properties map[string]ToolParamProp `json:"properties,omitempty"`
	Required   []string                 `json:"required,omitempty"`
}

type ToolParamProp struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  ToolParamSchema `json:"parameters,omitempty"`
}

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatRequest struct {
	Model    string           `json:"model"`
	Messages []ChatMessage    `json:"messages"`
	Stream   bool             `json:"stream"`
	Tools    []ToolDefinition `json:"tools,omitempty"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type streamChunk struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []streamChunkChoice `json:"choices"`
}

type streamChunkChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type streamDelta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type modelsResponse struct {
	Object string      `json:"object"`
	Data   []modelInfo `json:"data"`
}

type modelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type LLMClient interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
	ListModels(ctx context.Context) ([]string, error)
}

type ChatRequest struct {
	Model    string
	Messages []ChatMessage
	Tools    []ToolDefinition
}

type ChatResponse struct {
	ID      string
	Model   string
	Message ChatMessage
	Usage   struct {
		PromptTokens     int
		CompletionTokens int
		TotalTokens      int
	}
}

type StreamEvent struct {
	Token     string
	Done      bool
	ToolCalls []ToolCall
	Err       error
}

type KiloGatewayClient struct {
	baseURL      string
	apiKey       string
	defaultModel string
	client       *http.Client
}

func NewKiloGatewayClient(baseURL, apiKey, defaultModel string) *KiloGatewayClient {
	if baseURL == "" {
		baseURL = "https://gateway.kilo.ai"
	}
	if defaultModel == "" {
		defaultModel = "claude-sonnet-4-20250514"
	}
	return &KiloGatewayClient{
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiKey:       apiKey,
		defaultModel: defaultModel,
		client:       &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *KiloGatewayClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = c.defaultModel
	}

	body := chatRequest{
		Model:    model,
		Messages: req.Messages,
		Stream:   false,
	}
	if len(req.Tools) > 0 {
		body.Tools = req.Tools
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("llm chat: HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}

	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("llm chat: no choices in response")
	}

	result := &ChatResponse{
		ID:      out.ID,
		Model:   out.Model,
		Message: out.Choices[0].Message,
	}
	if out.Usage != nil {
		result.Usage.PromptTokens = out.Usage.PromptTokens
		result.Usage.CompletionTokens = out.Usage.CompletionTokens
		result.Usage.TotalTokens = out.Usage.TotalTokens
	}
	return result, nil
}

func (c *KiloGatewayClient) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	model := req.Model
	if model == "" {
		model = c.defaultModel
	}

	body := chatRequest{
		Model:    model,
		Messages: req.Messages,
		Stream:   true,
	}
	if len(req.Tools) > 0 {
		body.Tools = req.Tools
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("llm stream: HTTP %d: %s", resp.StatusCode, string(raw))
	}

	ch := make(chan StreamEvent, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				ch <- StreamEvent{Done: true}
				return
			}

			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			if len(chunk.Choices) == 0 {
				continue
			}
			choice := chunk.Choices[0]
			ch <- StreamEvent{
				Token:     choice.Delta.Content,
				ToolCalls: choice.Delta.ToolCalls,
				Done:      choice.FinishReason == "stop" || choice.FinishReason == "tool_calls",
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamEvent{Err: fmt.Errorf("stream read: %w", err)}
		}
	}()

	return ch, nil
}

func (c *KiloGatewayClient) ListModels(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("build models request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list models: HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var out modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		models = append(models, m.ID)
	}
	return models, nil
}
