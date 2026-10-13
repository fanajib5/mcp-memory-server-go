// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package mcp

import (
	"encoding/json"

	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"

	"mcp-memory-server/internal/usecase"
)

// Handlers binds MCP tool calls to the memory usecase.
type Handlers struct {
	uc *usecase.MemoryUseCase
}

// NewHandlers builds MCP tool handlers over a memory usecase.
func NewHandlers(uc *usecase.MemoryUseCase) *Handlers {
	return &Handlers{uc: uc}
}

func textResult(s string) *mcpgo.CallToolResult {
	return &mcpgo.CallToolResult{Content: []mcpgo.Content{&mcpgo.TextContent{Text: s}}}
}

func jsonResult(v any) *mcpgo.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return textResult("error encoding result: " + err.Error())
	}
	return textResult(string(b))
}
