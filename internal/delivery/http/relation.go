// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"fmt"
	"net/http"

	"mcp-memory-server/internal/entity"
)

func (u *UI) HandleRelationCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := r.FormValue("p")
	if _, err := u.UC.CreateRelations(r.Context(), p, []entity.RelationInput{
		{From: r.FormValue("from"), To: r.FormValue("to"), RelationType: r.FormValue("type")},
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u.renderObsOrRel(w, r, "relations")
}

func (u *UI) HandleRelationDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var id int
	fmt.Sscanf(r.FormValue("id"), "%d", &id)
	_ = u.UC.DeleteRelation(r.Context(), r.FormValue("p"), id)
	u.renderObsOrRel(w, r, "relations")
}
