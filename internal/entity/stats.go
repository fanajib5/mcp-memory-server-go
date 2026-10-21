// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package entity

// ---- UI / stats domain models ----

type EntityDetailObservation struct {
	ID         int      `json:"id"`
	Content    string   `json:"content"`
	Confidence *float64 `json:"confidence,omitempty"`
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
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	ObsCount     int     `json:"obsCount"`
	RelCount     int     `json:"relCount"`
	LastAccessed *string `json:"lastAccessed,omitempty"` // relative-time string for UI
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
