// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"encoding/json"
	"net/http"
)

func (u *UI) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	p := activeProject(r)
	m, err := u.SU.DashboardMetrics(r.Context(), p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	u.renderUI(w, r, "dashboard", map[string]any{"Project": p, "Metrics": m})
}

func (u *UI) HandleEntities(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	tf := r.URL.Query().Get("type")
	q := r.URL.Query().Get("q")
	rows, err := u.SU.ListEntities(r.Context(), p, tf, q, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := map[string]any{"Project": p, "TypeFilter": tf, "Query": q, "Rows": rows}
	if r.Header.Get("HX-Request") == "true" {
		u.renderUIFragment(w, r, "entities_rows", data)
		return
	}
	u.renderUI(w, r, "entities", data)
}

func (u *UI) HandleGraph(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	u.renderUI(w, r, "graph", map[string]any{"Project": p})
}

func (u *UI) HandleGraphJSON(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	g, err := u.SU.GraphData(r.Context(), p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
}
