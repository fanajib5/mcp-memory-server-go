package entity

// EntityInput is the normalized domain input for creating/updating an entity.
// jsonschema tags feed the MCP tool input schema (harmless outside MCP).
type EntityInput struct {
	Name         string   `json:"name" jsonschema:"Unique entity name within its project, e.g. 'MIS-APAR' or 'Faiq'"`
	Type         string   `json:"entityType,omitempty" jsonschema:"Registered type: project, person, decision, tool, concept, place"`
	Observations []string `json:"observations,omitempty" jsonschema:"Facts about this entity"`
}

// RelationInput is the normalized domain input for creating a directed relation.
type RelationInput struct {
	From         string `json:"from"`
	To           string `json:"to"`
	RelationType string `json:"relationType" jsonschema:"Active voice, UPPER_SNAKE_CASE, e.g. DEPLOYED_VIA"`
}

// Entity is a knowledge-graph entity (name + type + its observations).
type Entity struct {
	Name         string   `json:"name"`
	EntityType   string   `json:"type"`
	Observations []string `json:"observations"`
}

// SearchResult is one entity matched by a search, with its observations and
// relations formatted as "A --R--> B" strings.
type SearchResult struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Observations []string `json:"observations"`
	Relations    []string `json:"relations"`
}

// FullGraph is the whole graph for a project (or all projects), with relations
// as "A --R--> B" strings.
type FullGraph struct {
	Entities  []Entity `json:"entities"`
	Relations []string `json:"relations"`
}

// ExportEntity / ExportRelation / ExportPayload form the structured (lossless)
// export/import shape: relations carry their parts explicitly so import needs
// no fragile string parsing.
type ExportEntity struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Observations []string `json:"observations"`
}

type ExportRelation struct {
	From         string `json:"from"`
	RelationType string `json:"relationType"`
	To           string `json:"to"`
}

type ExportPayload struct {
	Project   string           `json:"project,omitempty"`
	Entities  []ExportEntity   `json:"entities"`
	Relations []ExportRelation `json:"relations"`
}

// ImportResult counts what an import created.
type ImportResult struct {
	EntitiesCreated  int `json:"entitiesCreated"`
	RelationsCreated int `json:"relationsCreated"`
}

// ---- UI / stats domain models ----

type EntityDetailObservation struct {
	ID      int    `json:"id"`
	Content string `json:"content"`
}

type EntityDetailRelation struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	Other     string `json:"other"`
	Direction string `json:"direction"` // "out" or "in"
}

type EntityDetail struct {
	Name         string                    `json:"name"`
	Type         string                    `json:"type"`
	Observations []EntityDetailObservation `json:"observations"`
	Relations    []EntityDetailRelation    `json:"relations"`
}

type EntitySummary struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	ObsCount int    `json:"obsCount"`
	RelCount int    `json:"relCount"`
}

type DayCount struct {
	Day          string `json:"day"`
	Entities     int    `json:"entities"`
	Observations int    `json:"observations"`
}

type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type Metrics struct {
	Entities        int             `json:"entities"`
	Observations    int             `json:"observations"`
	Relations       int             `json:"relations"`
	RelationTypes   int             `json:"relationTypes"`
	AllEntities     int             `json:"allEntities"`
	AllObservations int             `json:"allObservations"`
	ByType          []TypeCount     `json:"byType"`
	AvgObs          float64         `json:"avgObs"`
	AvgRel          float64         `json:"avgRel"`
	Orphans         int             `json:"orphans"`
	Sparse          int             `json:"sparse"`
	TopByObs        []EntitySummary `json:"topByObs"`
	TopByRel        []EntitySummary `json:"topByRel"`
	Growth          []DayCount      `json:"growth"`
	RelationTypesX  []TypeCount     `json:"relationTypeCounts"`
	RecentEntities  []EntitySummary `json:"recentEntities"`
}

// ---- Graph visualization models ----

type GraphNode struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
	Group string `json:"group"` // entity_type, drives vis-network node color
}

type GraphEdge struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	Label string `json:"label"` // relation_type
}

type GraphPayload struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}
