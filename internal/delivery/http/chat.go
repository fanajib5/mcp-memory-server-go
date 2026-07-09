// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"

	"mcp-memory-server/internal/usecase"
)

const chatSessionCookie = "mem_chat_session"

type ChatHandler struct {
	uc     *usecase.ChatUseCase
	nextID atomic.Int64
}

func NewChatHandler(uc *usecase.ChatUseCase) *ChatHandler {
	return &ChatHandler{uc: uc}
}

func (h *ChatHandler) sessionID(r *http.Request) string {
	c, err := r.Cookie(chatSessionCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	return c.Value
}

func (h *ChatHandler) HandlePage(ui *UI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := activeProject(r)
		sid := h.sessionID(r)

		history := h.uc.GetHistory(p, sid)
		models, _ := h.uc.ListModels(r.Context())

		data := map[string]any{
			"Project":  p,
			"History":  history,
			"Models":   models,
			"Disabled": ui.Cfg.KiloGatewayAPIKey == "",
		}
		ui.renderUI(w, r, "chat", data)
	}
}

func (h *ChatHandler) HandleSend(ui *UI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		message := r.FormValue("message")
		model := r.FormValue("model")
		p := r.FormValue("p")
		if p == "" {
			p = activeProject(r)
		}
		if message == "" {
			http.Error(w, "message required", http.StatusBadRequest)
			return
		}
		if ui.Cfg.KiloGatewayAPIKey == "" {
			ui.renderUIFragment(w, r, "chat_error", map[string]any{
				"Error": "LLM chat is disabled. Set KILO_GATEWAY_API_KEY environment variable.",
			})
			return
		}

		sid := h.sessionID(r)
		if sid == "" {
			sid = fmt.Sprintf("chat_%d", h.nextID.Add(1))
			http.SetCookie(w, &http.Cookie{
				Name:     chatSessionCookie,
				Value:    sid,
				Path:     "/ui",
				HttpOnly: true,
				Secure:   !ui.Cfg.CookieInsecure,
				SameSite: http.SameSiteLaxMode,
			})
		}

		_, err := h.uc.SendMessage(r.Context(), p, sid, message, model)
		if err != nil {
			log.Printf("chat error: %v", err)
			ui.renderUIFragment(w, r, "chat_error", map[string]any{
				"Error": fmt.Sprintf("Error: %v", err),
			})
			return
		}

		history := h.uc.GetHistory(p, sid)
		ui.renderUIFragment(w, r, "chat_messages", map[string]any{
			"Project": p,
			"History": history,
		})
	}
}

func (h *ChatHandler) HandleModels(ui *UI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		models, err := h.uc.ListModels(r.Context())
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"models": []string{"claude-sonnet-4-20250514"}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"models": models})
	}
}

func (h *ChatHandler) HandleClear(ui *UI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.FormValue("p")
		if p == "" {
			p = activeProject(r)
		}
		sid := h.sessionID(r)
		if sid != "" {
			h.uc.ClearConversation(p, sid)
		}
		http.Redirect(w, r, "/ui/chat?p="+p, http.StatusSeeOther)
	}
}
