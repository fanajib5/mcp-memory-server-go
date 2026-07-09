package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"mcp-memory-server/internal/entity"
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

func entityTypeList() string {
	types := []string{"project", "person", "decision", "tool", "concept", "place"}
	return strings.Join(types, ", ")
}

var systemPromptTmpl = `You are an AI assistant managing a knowledge graph memory system. You can help users create, read, update, and delete memories (entities, observations, and relations).

## Entity Types
- %s

## Rules
- Relation types must be UPPER_SNAKE_CASE and active voice (e.g. DEPLOYED_VIA, USES, DEPENDS_ON)
- Entity names are case-sensitive and unique within a project
- All operations are scoped to the current project
- Observations are individual facts about an entity
- Confidence values are 0.0-1.0 (optional)
- Use memory_search before delete/update operations to find the correct entity and observation IDs

When you need to perform a memory operation, use the available tools. Explain what you're doing to the user. Keep responses concise.`

func systemPrompt() string {
	return fmt.Sprintf(systemPromptTmpl, entityTypeList())
}

func toolDefinitions() []gateway.ToolDefinition {
	return []gateway.ToolDefinition{
		{
			Name:        "memory_search",
			Description: "Search entities by query text across observations, names, and relations",
			Parameters: gateway.ToolParamSchema{
				Type: "object",
				Properties: map[string]gateway.ToolParamProp{
					"query": {Type: "string", Description: "Search query"},
					"limit": {Type: "integer", Description: "Max results (default 20)"},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "memory_read_graph",
			Description: "Read the entire knowledge graph for the current project",
			Parameters: gateway.ToolParamSchema{
				Type:       "object",
				Properties: map[string]gateway.ToolParamProp{},
			},
		},
		{
			Name:        "memory_create_entities",
			Description: "Create one or more entities with optional observations",
			Parameters: gateway.ToolParamSchema{
				Type: "object",
				Properties: map[string]gateway.ToolParamProp{
					"entities": {
						Type:        "array",
						Description: `Array of entities. Each: {"name":"...", "entityType":"...", "observations":[...]}`,
					},
				},
				Required: []string{"entities"},
			},
		},
		{
			Name:        "memory_add_observations",
			Description: "Add observations to an existing entity",
			Parameters: gateway.ToolParamSchema{
				Type: "object",
				Properties: map[string]gateway.ToolParamProp{
					"entityName":   {Type: "string", Description: "Name of the entity"},
					"observations": {Type: "array", Description: "Facts to add"},
					"confidences":  {Type: "array", Description: "Optional confidence 0.0-1.0 per observation"},
				},
				Required: []string{"entityName", "observations"},
			},
		},
		{
			Name:        "memory_create_relations",
			Description: "Create directed relations between entities",
			Parameters: gateway.ToolParamSchema{
				Type: "object",
				Properties: map[string]gateway.ToolParamProp{
					"relations": {
						Type:        "array",
						Description: `Array: {"from":"...", "relationType":"DEPLOYS_VIA", "to":"..."}`,
					},
				},
				Required: []string{"relations"},
			},
		},
		{
			Name:        "memory_delete_entities",
			Description: "Delete entities and all their observations and relations",
			Parameters: gateway.ToolParamSchema{
				Type: "object",
				Properties: map[string]gateway.ToolParamProp{
					"names": {Type: "array", Description: "Entity names to delete"},
				},
				Required: []string{"names"},
			},
		},
		{
			Name:        "memory_rename_entity",
			Description: "Rename an entity and/or change its type",
			Parameters: gateway.ToolParamSchema{
				Type: "object",
				Properties: map[string]gateway.ToolParamProp{
					"oldName":    {Type: "string", Description: "Current entity name"},
					"newName":    {Type: "string", Description: "New entity name"},
					"entityType": {Type: "string", Description: "Optional new type: " + entityTypeList()},
				},
				Required: []string{"oldName", "newName"},
			},
		},
		{
			Name:        "memory_update_observation",
			Description: "Update an observation's content by its database ID. Use memory_search first to find the observation ID.",
			Parameters: gateway.ToolParamSchema{
				Type: "object",
				Properties: map[string]gateway.ToolParamProp{
					"id":            {Type: "integer", Description: "Observation database ID"},
					"content":       {Type: "string", Description: "New content text"},
					"newConfidence": {Type: "number", Description: "Optional new confidence 0.0-1.0"},
				},
				Required: []string{"id", "content"},
			},
		},
		{
			Name:        "memory_delete_observation",
			Description: "Delete an observation by its database ID. Use memory_search first to find the observation ID.",
			Parameters: gateway.ToolParamSchema{
				Type: "object",
				Properties: map[string]gateway.ToolParamProp{
					"entityName": {Type: "string", Description: "Entity name"},
					"id":         {Type: "integer", Description: "Observation database ID"},
				},
				Required: []string{"entityName", "id"},
			},
		},
		{
			Name:        "memory_delete_relation",
			Description: "Delete a relation by its database ID or by (from, to, relationType) triple",
			Parameters: gateway.ToolParamSchema{
				Type: "object",
				Properties: map[string]gateway.ToolParamProp{
					"from":         {Type: "string", Description: "Source entity name"},
					"to":           {Type: "string", Description: "Target entity name"},
					"relationType": {Type: "string", Description: "Relation type in UPPER_SNAKE_CASE"},
				},
			},
		},
		{
			Name:        "memory_get_history",
			Description: "Get the change history/audit trail for an entity",
			Parameters: gateway.ToolParamSchema{
				Type: "object",
				Properties: map[string]gateway.ToolParamProp{
					"entityName": {Type: "string", Description: "Entity name"},
					"limit":      {Type: "integer", Description: "Max entries"},
				},
				Required: []string{"entityName"},
			},
		},
		{
			Name:        "memory_export",
			Description: "Export the entire project graph as structured JSON",
			Parameters: gateway.ToolParamSchema{
				Type:       "object",
				Properties: map[string]gateway.ToolParamProp{},
			},
		},
		{
			Name:        "memory_import",
			Description: "Import entities and relations from structured JSON",
			Parameters: gateway.ToolParamSchema{
				Type: "object",
				Properties: map[string]gateway.ToolParamProp{
					"entities": {
						Type:        "array",
						Description: `Array: {"name":"...", "type":"...", "observations":[...]}`,
					},
					"relations": {
						Type:        "array",
						Description: `Array: {"from":"...", "relationType":"...", "to":"..."}`,
					},
				},
				Required: []string{"entities"},
			},
		},
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

func (uc *ChatUseCase) executeTool(ctx context.Context, project string, tc gateway.ToolCall) (string, error) {
	handlers := map[string]func(context.Context, string, string) (string, error){
		"memory_search": func(ctx context.Context, project string, args string) (string, error) {
			var a struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal([]byte(args), &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			if a.Limit <= 0 {
				a.Limit = 20
			}
			results, err := uc.memUC.Search(ctx, project, a.Query, a.Limit)
			if err != nil {
				return "", err
			}
			if len(results) == 0 {
				return "No results found.", nil
			}
			var b strings.Builder
			for _, r := range results {
				fmt.Fprintf(&b, "- **%s** (%s)\n", r.Name, r.Type)
				for _, o := range r.Observations {
					fmt.Fprintf(&b, "  - %s\n", o)
				}
				for _, rel := range r.Relations {
					fmt.Fprintf(&b, "  - %s\n", rel)
				}
			}
			return b.String(), nil
		},
		"memory_read_graph": func(ctx context.Context, project string, _ string) (string, error) {
			graph, err := uc.memUC.ReadGraph(ctx, project)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Project has %d entities and %d relations.\n\n", len(graph.Entities), len(graph.Relations))
			for _, e := range graph.Entities {
				fmt.Fprintf(&b, "- **%s** (%s)\n", e.Name, e.EntityType)
				for _, o := range e.Observations {
					fmt.Fprintf(&b, "  - %s\n", o)
				}
			}
			if len(graph.Relations) > 0 {
				fmt.Fprintf(&b, "\nRelations:\n")
				for _, r := range graph.Relations {
					fmt.Fprintf(&b, "- %s\n", r)
				}
			}
			return b.String(), nil
		},
		"memory_create_entities": func(ctx context.Context, project string, args string) (string, error) {
			var a struct {
				Entities []entity.EntityInput `json:"entities"`
			}
			if err := json.Unmarshal([]byte(args), &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			created, err := uc.memUC.CreateEntities(ctx, project, a.Entities)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Created entities: %s", strings.Join(created, ", ")), nil
		},
		"memory_add_observations": func(ctx context.Context, project string, args string) (string, error) {
			var a struct {
				EntityName   string    `json:"entityName"`
				Observations []string  `json:"observations"`
				Confidences  []float64 `json:"confidences"`
			}
			if err := json.Unmarshal([]byte(args), &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			if err := uc.memUC.AddObservations(ctx, project, a.EntityName, a.Observations, a.Confidences); err != nil {
				return "", err
			}
			return fmt.Sprintf("Added %d observations to %s.", len(a.Observations), a.EntityName), nil
		},
		"memory_create_relations": func(ctx context.Context, project string, args string) (string, error) {
			var a struct {
				Relations []entity.RelationInput `json:"relations"`
			}
			if err := json.Unmarshal([]byte(args), &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			created, err := uc.memUC.CreateRelations(ctx, project, a.Relations)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Created relations: %s", strings.Join(created, ", ")), nil
		},
		"memory_delete_entities": func(ctx context.Context, project string, args string) (string, error) {
			var a struct {
				Names []string `json:"names"`
			}
			if err := json.Unmarshal([]byte(args), &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			if err := uc.memUC.DeleteEntities(ctx, project, a.Names); err != nil {
				return "", err
			}
			return fmt.Sprintf("Deleted entities: %s", strings.Join(a.Names, ", ")), nil
		},
		"memory_rename_entity": func(ctx context.Context, project string, args string) (string, error) {
			var a struct {
				OldName    string `json:"oldName"`
				NewName    string `json:"newName"`
				EntityType string `json:"entityType"`
			}
			if err := json.Unmarshal([]byte(args), &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			if err := uc.memUC.UpdateEntity(ctx, project, a.OldName, a.NewName, a.EntityType); err != nil {
				return "", err
			}
			return fmt.Sprintf("Renamed %s -> %s", a.OldName, a.NewName), nil
		},
		"memory_update_observation": func(ctx context.Context, project string, args string) (string, error) {
			var a struct {
				ID            int      `json:"id"`
				Content       string   `json:"content"`
				NewConfidence *float64 `json:"newConfidence"`
			}
			if err := json.Unmarshal([]byte(args), &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			if err := uc.memUC.UpdateObservation(ctx, project, a.ID, a.Content, a.NewConfidence); err != nil {
				return "", err
			}
			return "Observation updated.", nil
		},
		"memory_delete_observation": func(ctx context.Context, project string, args string) (string, error) {
			var a struct {
				ID int `json:"id"`
			}
			if err := json.Unmarshal([]byte(args), &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			if err := uc.memUC.DeleteObservation(ctx, project, a.ID); err != nil {
				return "", err
			}
			return "Observation deleted.", nil
		},
		"memory_delete_relation": func(ctx context.Context, project string, args string) (string, error) {
			var a struct {
				ID           int    `json:"id"`
				From         string `json:"from"`
				To           string `json:"to"`
				RelationType string `json:"relationType"`
			}
			if err := json.Unmarshal([]byte(args), &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			if a.ID > 0 {
				if err := uc.memUC.DeleteRelation(ctx, project, a.ID); err != nil {
					return "", err
				}
			} else {
				if err := uc.memUC.DeleteRelationByTriple(ctx, project, a.From, a.To, a.RelationType); err != nil {
					return "", err
				}
			}
			return "Relation deleted.", nil
		},
		"memory_get_history": func(ctx context.Context, project string, args string) (string, error) {
			var a struct {
				EntityName string `json:"entityName"`
				Limit      int    `json:"limit"`
			}
			if err := json.Unmarshal([]byte(args), &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			if a.Limit <= 0 {
				a.Limit = 50
			}
			history, err := uc.memUC.GetHistory(ctx, project, a.EntityName, a.Limit)
			if err != nil {
				return "", err
			}
			if len(history) == 0 {
				return "No history found for " + a.EntityName, nil
			}
			var b strings.Builder
			for _, h := range history {
				fmt.Fprintf(&b, "- [%s] %s", h.HappenedAt, h.Action)
				if h.OldValue != nil {
					fmt.Fprintf(&b, " old=%q", *h.OldValue)
				}
				if h.NewValue != nil {
					fmt.Fprintf(&b, " new=%q", *h.NewValue)
				}
				b.WriteString("\n")
			}
			return b.String(), nil
		},
		"memory_export": func(ctx context.Context, project string, _ string) (string, error) {
			exp, err := uc.memUC.Export(ctx, project)
			if err != nil {
				return "", err
			}
			raw, _ := json.MarshalIndent(exp, "", "  ")
			return fmt.Sprintf("Export (%d entities, %d relations):\n```json\n%s\n```",
				len(exp.Entities), len(exp.Relations), string(raw)), nil
		},
		"memory_import": func(ctx context.Context, project string, args string) (string, error) {
			var a struct {
				Entities  []entity.ExportEntity   `json:"entities"`
				Relations []entity.ExportRelation `json:"relations"`
			}
			if err := json.Unmarshal([]byte(args), &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			payload := &entity.ExportPayload{Entities: a.Entities, Relations: a.Relations}
			result, err := uc.memUC.Import(ctx, project, payload)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Imported %d entities and %d relations.", result.EntitiesCreated, result.RelationsCreated), nil
		},
	}

	h, ok := handlers[tc.Function.Name]
	if !ok {
		return fmt.Sprintf("Unknown tool: %s", tc.Function.Name), nil
	}
	return h(ctx, project, tc.Function.Arguments)
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
		return []string{"claude-sonnet-4-20250514"}, nil
	}
	return models, nil
}
