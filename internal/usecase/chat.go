// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package usecase

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"mcp-memory-server/internal/gateway"
)

type Conversation struct {
	mu           sync.Mutex
	Project      string
	Messages     []gateway.ChatMessage
	LastAccessed time.Time
}

type ChatUseCase struct {
	llm   gateway.LLMClient
	memUC *MemoryUseCase
	mu    sync.RWMutex
	convs map[string]*Conversation
}

func NewChatUseCase(llm gateway.LLMClient, memUC *MemoryUseCase) *ChatUseCase {
	uc := &ChatUseCase{
		llm:   llm,
		memUC: memUC,
		convs: make(map[string]*Conversation),
	}
	go uc.evictStaleSessions()
	return uc
}

const sessionTTL = 1 * time.Hour
const cleanupInterval = 10 * time.Minute

func (uc *ChatUseCase) evictStaleSessions() {
	for {
		time.Sleep(cleanupInterval)
		uc.mu.Lock()
		for key, conv := range uc.convs {
			if time.Since(conv.LastAccessed) > sessionTTL {
				delete(uc.convs, key)
			}
		}
		uc.mu.Unlock()
	}
}

func (uc *ChatUseCase) getOrCreateConversation(project, sessionID string) *Conversation {
	key := project + ":" + sessionID
	uc.mu.Lock()
	defer uc.mu.Unlock()
	conv, ok := uc.convs[key]
	if ok {
		return conv
	}
	conv = &Conversation{Project: project, LastAccessed: time.Now()}
	uc.convs[key] = conv
	return conv
}

func (uc *ChatUseCase) SendMessage(ctx context.Context, project, sessionID, message, model string) (string, error) {
	conv := uc.getOrCreateConversation(project, sessionID)

	conv.mu.Lock()
	if len(conv.Messages) == 0 {
		conv.Messages = append(conv.Messages, gateway.ChatMessage{Role: "system", Content: systemPrompt()})
	}
	conv.Messages = append(conv.Messages, gateway.ChatMessage{Role: "user", Content: message})
	conv.LastAccessed = time.Now()
	messages := make([]gateway.ChatMessage, len(conv.Messages))
	copy(messages, conv.Messages)
	conv.mu.Unlock()

	tools := toolDefinitions()
	maxTurns := 10
	for turn := 0; turn < maxTurns; turn++ {
		resp, err := uc.llm.Chat(ctx, gateway.ChatRequest{
			Model:    model,
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			return "", fmt.Errorf("llm chat: %w", err)
		}

		conv.mu.Lock()
		conv.Messages = append(conv.Messages, resp.Message)
		conv.LastAccessed = time.Now()

		if len(resp.Message.ToolCalls) == 0 {
			finalContent := resp.Message.Content
			conv.mu.Unlock()
			return finalContent, nil
		}

		for _, tc := range resp.Message.ToolCalls {
			toolMsg, err := uc.executeTool(ctx, project, tc)
			if err != nil {
				toolMsg = fmt.Sprintf("Error executing %s: %v", tc.Function.Name, err)
			}
			conv.Messages = append(conv.Messages, gateway.ChatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    toolMsg,
			})
		}
		conv.LastAccessed = time.Now()
		conv.mu.Unlock()

		messages = nil
		conv.mu.Lock()
		messages = make([]gateway.ChatMessage, len(conv.Messages))
		copy(messages, conv.Messages)
		conv.mu.Unlock()
	}

	return "I've reached the maximum number of operations for this conversation. The last batch of changes has been applied. What would you like to do next?", nil
}

func (uc *ChatUseCase) ClearConversation(project, sessionID string) {
	key := project + ":" + sessionID
	uc.mu.Lock()
	delete(uc.convs, key)
	uc.mu.Unlock()
}

func (uc *ChatUseCase) GetHistory(project, sessionID string) []gateway.ChatMessage {
	key := project + ":" + sessionID
	uc.mu.RLock()
	conv, ok := uc.convs[key]
	uc.mu.RUnlock()
	if !ok {
		return nil
	}

	conv.mu.Lock()
	messages := make([]gateway.ChatMessage, 0, len(conv.Messages))
	for _, m := range conv.Messages {
		if m.Role == "system" || m.Role == "tool" || (m.Role == "assistant" && m.Content == "" && len(m.ToolCalls) > 0) {
			continue
		}
		messages = append(messages, m)
	}
	conv.LastAccessed = time.Now()
	conv.mu.Unlock()
	return messages
}

func (uc *ChatUseCase) ListModels(ctx context.Context) ([]string, error) {
	models, err := uc.llm.ListModels(ctx)
	if err != nil {
		log.Printf("list models error (non-fatal): %v", err)
		return []string{"kilo-auto/free"}, nil
	}
	return models, nil
}
