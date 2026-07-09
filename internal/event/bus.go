// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

// Package event provides a lightweight in-memory pub/sub event bus for
// cross-agent memory change notifications. Designed for single-container
// deployments (tool personal). Not suitable for multi-container scaling
// without adding Redis/NATS backing.
package event

import (
	"sync"
	"time"
)

// Event is one memory change notification.
type Event struct {
	Type      string    `json:"type"`
	Project   string    `json:"project"`
	Entity    string    `json:"entity,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Bus is an in-memory fan-out event bus keyed by project name. Subscribers
// register per-project; publishers broadcast to all subscribers of that
// project. Safe for concurrent use.
type Bus struct {
	mu     sync.RWMutex
	subs   map[string]map[uint64]chan Event // project -> subscriberID -> channel
	nextID uint64
}

// NewBus creates an empty event bus.
func NewBus() *Bus {
	return &Bus{subs: make(map[string]map[uint64]chan Event)}
}

// Subscribe registers for events on a project. Returns a receive-only channel
// (buffered, capacity 16) and an unsubscribe function. The channel is closed
// when unsubscribe is called or the Bus is shut down.
func (b *Bus) Subscribe(project string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	if b.subs[project] == nil {
		b.subs[project] = make(map[uint64]chan Event)
	}
	b.subs[project][id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if subs, ok := b.subs[project]; ok {
			if c, ok := subs[id]; ok {
				close(c)
				delete(subs, id)
			}
			if len(subs) == 0 {
				delete(b.subs, project)
			}
		}
	}
}

// Publish sends an event to all subscribers of the given project. It is
// non-blocking: if a subscriber's channel buffer is full, the event is
// dropped for that subscriber (avoids one slow consumer blocking all).
func (b *Bus) Publish(project string, e Event) {
	e.Project = project
	e.Timestamp = time.Now().UTC()

	b.mu.RLock()
	subs := b.subs[project]
	channels := make([]chan Event, 0, len(subs))
	for _, ch := range subs {
		channels = append(channels, ch)
	}
	b.mu.RUnlock()

	for _, ch := range channels {
		select {
		case ch <- e:
		default:
			// Drop: subscriber buffer full (slow consumer).
		}
	}
}

// SubCount returns the number of active subscribers for a project (for
// debugging/monitoring).
func (b *Bus) SubCount(project string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[project])
}
