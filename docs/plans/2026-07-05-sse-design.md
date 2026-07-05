# Design: Cross-Agent Sync (SSE)

Tanggal: 2026-07-05
Status: VALIDATED

## Tujuan

Real-time event stream via Server-Sent Events. Multiple AI agents subscribe
to per-project changes. In-memory event bus, no external dependency.

## Keputusan design (validated)

1. **Event bus**: in-memory channel per-project (map[project][]chan Event).
2. **Scope**: per-project subscription (GET /events?p=project).
3. **Auth**: Bearer token/JWT (reuse authMiddleware, same as /mcp).
4. **Reconnection**: no event replay (YAGNI). On reconnect, subscriber gets
   new events from that point forward.

## Layer changes

### Event bus (`internal/event/bus.go`) — BARU
```go
type Event struct {
    Type      string    `json:"type"`
    Project   string    `json:"project"`
    Entity    string    `json:"entity,omitempty"`
    Timestamp time.Time `json:"timestamp"`
}
type Bus struct { ... }
func (b *Bus) Subscribe(project string) (<-chan Event, func())
func (b *Bus) Publish(project string, e Event)
```
- Non-blocking publish (drop if subscriber buffer full).
- Unsubscribe via cleanup func (closes channel, removes from map).

### UseCase
- Field `bus *event.Bus` (nil = events disabled).
- After each successful mutation, publish event.
  Events: entity_created, entity_deleted, observation_added, observation_updated,
  observation_deleted, entity_renamed, entity_type_changed, relation_created,
  relation_deleted.

### Delivery HTTP (`internal/delivery/http/events.go`) — BARU
- GET /events (with ?p=project query)
- Auth via authMiddleware (Bearer/JWT)
- SSE headers: Content-Type: text/event-stream, Cache-Control: no-cache, Connection: keep-alive
- Loop: read from channel, write SSE frame, flush
- Context cancellation on client disconnect

### Routes
- Add `mux.Handle("/events", authMiddleware(cfg, sseHandler))`

## Testing
- Unit: bus subscribe/publish/unsubscribe, buffer-full drop.
- Integration: mutation → event received via SSE.
