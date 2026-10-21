// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package usecase

import (
	"context"
	"fmt"
	"strings"

	"mcp-memory-server/internal/entity"
)

func (u *MemoryUseCase) CreateEntities(ctx context.Context, project string, entities []entity.EntityInput) ([]string, error) {
	project = defaultProject(project)
	for i := range entities {
		entities[i].Type = normalizeEntityType(entities[i].Type)
		entities[i].Confidences = clampConfidences(entities[i].Confidences)
		if len(entities[i].Observations) > 0 {
			entities[i].Embeddings = u.embedTexts(ctx, entities[i].Observations)
		}
	}
	created, err := u.repo.CreateEntities(ctx, project, entities)
	if err != nil {
		return nil, err
	}
	for _, name := range created {
		u.publish(project, name, "entity_created")
	}
	return created, nil
}

func (u *MemoryUseCase) AddObservations(ctx context.Context, project, entityName string, observations []string, confidences []float64, attr ...entity.Attribution) error {
	project = defaultProject(project)
	embeddings := u.embedTexts(ctx, observations)
	if err := u.repo.AddObservations(ctx, project, entityName, observations, clampConfidences(confidences), embeddings, attr...); err != nil {
		return err
	}
	u.publish(project, entityName, "observation_added")
	return nil
}

func (u *MemoryUseCase) CreateRelations(ctx context.Context, project string, relations []entity.RelationInput) ([]string, error) {
	project = defaultProject(project)
	for i := range relations {
		relations[i].RelationType = normalizeRelationType(relations[i].RelationType)
		if relations[i].RelationType == "" {
			return nil, fmt.Errorf("relation has empty type after normalization")
		}
	}
	created, err := u.repo.CreateRelations(ctx, project, relations)
	if err != nil {
		return nil, err
	}
	for _, r := range relations {
		u.publish(project, r.From, "relation_created")
	}
	return created, nil
}

func (u *MemoryUseCase) DeleteEntities(ctx context.Context, project string, names []string) error {
	project = defaultProject(project)
	if err := u.repo.DeleteEntities(ctx, project, names); err != nil {
		return err
	}
	for _, name := range names {
		u.publish(project, name, "entity_deleted")
	}
	return nil
}

func (u *MemoryUseCase) UpdateEntity(ctx context.Context, project, oldName, newName, entityType string) error {
	project = defaultProject(project)
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("entity name is required")
	}
	et := normalizeEntityType(entityType)
	if err := u.repo.UpdateEntity(ctx, project, oldName, newName, et); err != nil {
		return err
	}
	if newName != oldName {
		u.publish(project, newName, "entity_renamed")
	} else {
		u.publish(project, newName, "entity_type_changed")
	}
	return nil
}

func (u *MemoryUseCase) DeleteObservation(ctx context.Context, project string, id int) error {
	project = defaultProject(project)
	if err := u.repo.DeleteObservation(ctx, project, id); err != nil {
		return err
	}
	u.publish(project, "", "observation_deleted")
	return nil
}

func (u *MemoryUseCase) UpdateObservation(ctx context.Context, project string, id int, content string, newConfidence *float64) error {
	project = defaultProject(project)
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("observation content is required")
	}
	var conf *float64
	if newConfidence != nil {
		c := clampConfidence(*newConfidence)
		conf = &c
	}
	emb := u.embedTexts(ctx, []string{content})
	var vec []float32
	if len(emb) > 0 {
		vec = emb[0]
	}
	if err := u.repo.UpdateObservation(ctx, project, id, content, conf, vec); err != nil {
		return err
	}
	u.publish(project, "", "observation_updated")
	return nil
}

func (u *MemoryUseCase) DeleteRelation(ctx context.Context, project string, id int) error {
	project = defaultProject(project)
	if err := u.repo.DeleteRelation(ctx, project, id); err != nil {
		return err
	}
	u.publish(project, "", "relation_deleted")
	return nil
}

func (u *MemoryUseCase) DeleteObservationByContent(ctx context.Context, project, entityName, content string) error {
	project = defaultProject(project)
	if err := u.repo.DeleteObservationByContent(ctx, project, entityName, content); err != nil {
		return err
	}
	u.publish(project, entityName, "observation_deleted")
	return nil
}

func (u *MemoryUseCase) UpdateObservationByContent(ctx context.Context, project, entityName, oldContent, newContent string, newConfidence *float64) error {
	project = defaultProject(project)
	if strings.TrimSpace(newContent) == "" {
		return fmt.Errorf("new content is required")
	}
	var conf *float64
	if newConfidence != nil {
		c := clampConfidence(*newConfidence)
		conf = &c
	}
	emb := u.embedTexts(ctx, []string{newContent})
	var vec []float32
	if len(emb) > 0 {
		vec = emb[0]
	}
	if err := u.repo.UpdateObservationByContent(ctx, project, entityName, oldContent, newContent, conf, vec); err != nil {
		return err
	}
	u.publish(project, entityName, "observation_updated")
	return nil
}

func (u *MemoryUseCase) DeleteRelationByTriple(ctx context.Context, project, from, to, relationType string) error {
	project = defaultProject(project)
	relType := normalizeRelationType(relationType)
	if relType == "" {
		return fmt.Errorf("relation type is required")
	}
	if err := u.repo.DeleteRelationByTriple(ctx, project, from, to, relType); err != nil {
		return err
	}
	u.publish(project, from, "relation_deleted")
	return nil
}
