package mcp

import "mcp-memory-server/internal/entity"

// Tool input types. jsonschema tags give the LLM field descriptions; the SDK
// reflects these (including nested entity types) to build each tool's schema.

type CreateEntitiesInput struct {
	Project  string               `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	Entities []entity.EntityInput `json:"entities" jsonschema:"Entities to create or update"`
}

type AddObservationsInput struct {
	Project      string    `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	EntityName   string    `json:"entityName"`
	Observations []string  `json:"observations" jsonschema:"New facts to attach to this entity"`
	Confidences  []float64 `json:"confidences,omitempty" jsonschema:"Optional AI confidence 0.0-1.0 per observation, parallel to observations. Omit/empty = neutral (treated as 1.0)."`
}

type CreateRelationsInput struct {
	Project   string                 `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	Relations []entity.RelationInput `json:"relations" jsonschema:"Directed relations to create between entities"`
}

type DeleteEntitiesInput struct {
	Project string   `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	Names   []string `json:"names" jsonschema:"Entity names to delete, including their observations and relations"`
}

type SearchInput struct {
	Project string `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	Query   string `json:"query"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Max results, default 20, max 100"`
}

type ReadGraphInput struct {
	Project string `json:"project,omitempty" jsonschema:"Optional project to scope to; blank = all projects (debug view)"`
}

type ExportInput struct {
	Project string `json:"project,omitempty" jsonschema:"Project to export; blank = all projects"`
}

type ImportInput struct {
	Project   string                  `json:"project,omitempty" jsonschema:"Target project; defaults to 'default'"`
	Entities  []entity.ExportEntity   `json:"entities,omitempty" jsonschema:"Entities to import"`
	Relations []entity.ExportRelation `json:"relations,omitempty" jsonschema:"Relations to import"`
}

type RenameEntityInput struct {
	Project    string `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	OldName    string `json:"oldName" jsonschema:"Current entity name"`
	NewName    string `json:"newName" jsonschema:"New entity name"`
	EntityType string `json:"entityType,omitempty" jsonschema:"Optional new type: project, person, decision, tool, concept, place"`
}

type UpdateObservationInput struct {
	Project       string   `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	EntityName    string   `json:"entityName" jsonschema:"Entity the observation belongs to"`
	OldContent    string   `json:"oldContent" jsonschema:"Exact current observation text to match"`
	NewContent    string   `json:"newContent" jsonschema:"Replacement text"`
	NewConfidence *float64 `json:"newConfidence,omitempty" jsonschema:"Optional new confidence 0.0-1.0; omit to leave unchanged."`
}

type DeleteObservationInput struct {
	Project    string `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	EntityName string `json:"entityName"`
	Content    string `json:"content" jsonschema:"Exact observation text to delete"`
}

type DeleteRelationInput struct {
	Project      string `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	From         string `json:"from"`
	To           string `json:"to"`
	RelationType string `json:"relationType" jsonschema:"Active voice, UPPER_SNAKE_CASE, e.g. DEPLOYED_VIA"`
}
