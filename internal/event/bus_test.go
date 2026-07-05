package event

import (
	"testing"
	"time"
)

func TestSubscribePublish(t *testing.T) {
	b := NewBus()
	ch, unsub := b.Subscribe("projA")
	defer unsub()

	b.Publish("projA", Event{Type: "entity_created", Entity: "X"})

	select {
	case e := <-ch:
		if e.Type != "entity_created" || e.Entity != "X" {
			t.Fatalf("got %+v", e)
		}
		if e.Project != "projA" {
			t.Fatalf("project = %q, want projA", e.Project)
		}
		if e.Timestamp.IsZero() {
			t.Fatal("timestamp not set")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestProjectIsolation(t *testing.T) {
	b := NewBus()
	chA, _ := b.Subscribe("projA")
	chB, _ := b.Subscribe("projB")

	b.Publish("projA", Event{Type: "entity_created"})

	select {
	case <-chA:
	case <-time.After(time.Second):
		t.Fatal("projA subscriber did not receive event")
	}

	select {
	case e := <-chB:
		t.Fatalf("projB subscriber received event from projA: %+v", e)
	case <-time.After(100 * time.Millisecond):
		// Good: no event for projB.
	}
}

func TestUnsubscribe(t *testing.T) {
	b := NewBus()
	ch, unsub := b.Subscribe("projA")

	unsub()

	b.Publish("projA", Event{Type: "entity_created"})

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("received event after unsubscribe")
		}
		// Channel closed — good.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("channel not closed after unsubscribe")
	}
}

func TestBufferFullDropsEvent(t *testing.T) {
	b := NewBus()
	ch, unsub := b.Subscribe("projA")
	defer unsub()

	// Fill the 16-buffer channel without reading.
	for i := 0; i < 20; i++ {
		b.Publish("projA", Event{Type: "entity_created"})
	}

	// Should have received exactly 16 (buffer cap), not blocked.
	received := 0
drainLoop:
	for {
		select {
		case <-ch:
			received++
		default:
			break drainLoop
		}
	}
	if received != 16 {
		t.Fatalf("received %d events, want 16 (buffer cap, rest dropped)", received)
	}
}

func TestSubCount(t *testing.T) {
	b := NewBus()
	if b.SubCount("projA") != 0 {
		t.Fatal("expected 0 subs initially")
	}
	_, unsub1 := b.Subscribe("projA")
	b.Subscribe("projA")
	if b.SubCount("projA") != 2 {
		t.Fatalf("expected 2 subs, got %d", b.SubCount("projA"))
	}
	unsub1()
	if b.SubCount("projA") != 1 {
		t.Fatalf("expected 1 sub after unsub, got %d", b.SubCount("projA"))
	}
}
