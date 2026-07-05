package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mcp-memory-server/internal/event"
)

// SSEHandler serves Server-Sent Events for per-project memory changes.
type SSEHandler struct {
	bus *event.Bus
}

func NewSSEHandler(bus *event.Bus) *SSEHandler {
	return &SSEHandler{bus: bus}
}

// ServeHTTP implements the SSE streaming endpoint GET /events?p=project.
// Subscribers receive JSON-encoded event frames. The stream stays open until
// the client disconnects (context cancellation).
func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("p")
	if project == "" {
		project = "default"
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable Nginx buffering

	// Send a comment heartbeat so the client knows the stream is alive.
	fmt.Fprintf(w, ": connected to project %s\n\n", project)
	flusher.Flush()

	ch, unsub := h.bus.Subscribe(project)
	defer unsub()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-ticker.C:
			// Heartbeat comment keeps connection alive through proxies.
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
