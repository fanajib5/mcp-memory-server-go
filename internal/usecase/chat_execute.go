// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mcp-memory-server/internal/entity"
	"mcp-memory-server/internal/gateway"
)

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
