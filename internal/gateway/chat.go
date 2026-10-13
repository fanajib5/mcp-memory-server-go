// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

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
