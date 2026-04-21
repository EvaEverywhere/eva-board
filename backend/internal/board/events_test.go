package board

import (
	"sync"
	"testing"
	"time"
)

func waitFor(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatalf("channel closed before event arrived")
		}
		return ev
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for event")
	}
	return Event{}
}

func TestEventBrokerSubscribePublishReceive(t *testing.T) {
	b := NewBroker()
	ch, cleanup := b.Subscribe("user-1", "")
	defer cleanup()

	b.Publish(Event{Type: EventAgentStarted, UserID: "user-1", CardID: "card-1"})

	ev := waitFor(t, ch)
	if ev.Type != EventAgentStarted {
		t.Fatalf("expected %s, got %s", EventAgentStarted, ev.Type)
	}
	if ev.CardID != "card-1" {
		t.Fatalf("expected card-1, got %s", ev.CardID)
	}
	if ev.ID == "" {
		t.Fatalf("expected broker-assigned ID")
	}
	if ev.Timestamp.IsZero() {
		t.Fatalf("expected broker-assigned timestamp")
	}
}

func TestEventBrokerMultipleSubscribersSameUser(t *testing.T) {
	b := NewBroker()

	chA, cleanupA := b.Subscribe("user-1", "")
	defer cleanupA()
	chB, cleanupB := b.Subscribe("user-1", "")
	defer cleanupB()

	b.Publish(Event{Type: EventAgentProgress, UserID: "user-1", CardID: "card-1"})

	evA := waitFor(t, chA)
	evB := waitFor(t, chB)

	if evA.ID != evB.ID || evA.Type != EventAgentProgress {
		t.Fatalf("subscribers received divergent events: %+v vs %+v", evA, evB)
	}
}

func TestEventBrokerUserIsolation(t *testing.T) {
	b := NewBroker()

	chA, cleanupA := b.Subscribe("user-A", "")
	defer cleanupA()
	chB, cleanupB := b.Subscribe("user-B", "")
	defer cleanupB()

	b.Publish(Event{Type: EventAgentStarted, UserID: "user-A", CardID: "card-A"})

	evA := waitFor(t, chA)
	if evA.UserID != "user-A" {
		t.Fatalf("user A got wrong event: %+v", evA)
	}

	select {
	case ev := <-chB:
		t.Fatalf("user B should not have received event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEventBrokerSlowSubscriberDropsWithoutBlocking(t *testing.T) {
	b := NewBroker()

	_, cleanup := b.Subscribe("user-1", "")
	defer cleanup()

	done := make(chan struct{})
	go func() {
		for i := 0; i < subscriberBuffer*4; i++ {
			b.Publish(Event{Type: EventAgentProgress, UserID: "user-1", CardID: "card-1"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("publish blocked when subscriber was slow")
	}
}

func TestEventBrokerLastEventIDReplay(t *testing.T) {
	b := NewBroker()

	for i := 0; i < 5; i++ {
		b.Publish(Event{Type: EventAgentProgress, UserID: "user-1", CardID: "card-1"})
	}

	ch, cleanup := b.Subscribe("user-1", "2")
	defer cleanup()

	var ids []string
	for i := 0; i < 3; i++ {
		ev := waitFor(t, ch)
		ids = append(ids, ev.ID)
	}

	expected := []string{"3", "4", "5"}
	for i, want := range expected {
		if ids[i] != want {
			t.Fatalf("replay order wrong: got %v, want %v", ids, expected)
		}
	}

	b.Publish(Event{Type: EventAgentFinished, UserID: "user-1", CardID: "card-1"})
	ev := waitFor(t, ch)
	if ev.ID != "6" || ev.Type != EventAgentFinished {
		t.Fatalf("post-replay event wrong: %+v", ev)
	}
}

func TestEventBrokerCleanupRemovesSubscriber(t *testing.T) {
	b := NewBroker()
	_, cleanup := b.Subscribe("user-1", "")

	b.mu.RLock()
	if got := len(b.subscribers["user-1"]); got != 1 {
		b.mu.RUnlock()
		t.Fatalf("expected 1 subscriber, got %d", got)
	}
	b.mu.RUnlock()

	cleanup()

	b.mu.RLock()
	defer b.mu.RUnlock()
	if got := len(b.subscribers["user-1"]); got != 0 {
		t.Fatalf("expected 0 subscribers after cleanup, got %d", got)
	}
}

// Sanity: concurrent publishes from many goroutines do not race.
func TestEventBrokerConcurrentPublish(t *testing.T) {
	b := NewBroker()
	ch, cleanup := b.Subscribe("user-1", "")
	defer cleanup()

	var wg sync.WaitGroup
	const writers = 8
	const perWriter = 32
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				b.Publish(Event{Type: EventAgentProgress, UserID: "user-1", CardID: "card-1"})
			}
		}()
	}
	wg.Wait()

	// Drain whatever fit into the buffered channel; we only assert no panic / no deadlock.
	for {
		select {
		case <-ch:
		case <-time.After(20 * time.Millisecond):
			return
		}
	}
}
