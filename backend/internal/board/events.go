package board

import (
	"strconv"
	"sync"
	"time"
)

// EventType enumerates the lifecycle stages emitted by the board.
type EventType string

const (
	EventAgentStarted        EventType = "agent_started"
	EventAgentProgress       EventType = "agent_progress"
	EventAgentFinished       EventType = "agent_finished"
	EventVerificationStarted EventType = "verification_started"
	EventVerificationResult  EventType = "verification_result"
	EventReviewStarted       EventType = "review_started"
	EventReviewResult        EventType = "review_result"
	EventPRCreated           EventType = "pr_created"
	EventCardMoved           EventType = "card_moved"
	EventError               EventType = "error"
)

// Event is one update for one card.
type Event struct {
	ID        string         `json:"id"`
	Type      EventType      `json:"type"`
	UserID    string         `json:"user_id"`
	CardID    string         `json:"card_id"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

const (
	subscriberBuffer = 64
	historyCapacity  = 256
)

type subscription struct {
	ch chan Event
}

// Broker is an in-memory pub/sub for board events. One process only — no
// Redis/cluster support in v1. Subscribers buffer up to 64 events; if a
// subscriber falls behind, the oldest events are dropped silently.
type Broker struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[string][]*subscription
	history     []Event
}

// NewBroker constructs an empty Broker ready to publish and subscribe.
func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[string][]*subscription),
		history:     make([]Event, 0, historyCapacity),
	}
}

// Publish emits an event to all subscribers for the user.
//
// The event's ID and Timestamp are assigned by the broker if unset, so callers
// can pass a zero-value Event and only populate Type/UserID/CardID/Data.
func (b *Broker) Publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	ev.ID = strconv.FormatUint(b.nextID, 10)
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}

	if len(b.history) == historyCapacity {
		copy(b.history, b.history[1:])
		b.history = b.history[:historyCapacity-1]
	}
	b.history = append(b.history, ev)

	for _, sub := range b.subscribers[ev.UserID] {
		select {
		case sub.ch <- ev:
		default:
			// Drop oldest by draining one slot, then enqueue.
			select {
			case <-sub.ch:
			default:
			}
			select {
			case sub.ch <- ev:
			default:
			}
		}
	}
}

// Subscribe returns a channel of events for the user. The caller must call
// the returned cleanup function when done. If lastEventID is non-empty and
// is still in the history, replay events after it before streaming new ones.
func (b *Broker) Subscribe(userID, lastEventID string) (<-chan Event, func()) {
	sub := &subscription{ch: make(chan Event, subscriberBuffer)}

	b.mu.Lock()
	if lastEventID != "" {
		for _, ev := range b.history {
			if ev.UserID != userID {
				continue
			}
			if compareEventIDs(ev.ID, lastEventID) > 0 {
				select {
				case sub.ch <- ev:
				default:
				}
			}
		}
	}
	b.subscribers[userID] = append(b.subscribers[userID], sub)
	b.mu.Unlock()

	cleanup := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subscribers[userID]
		for i, s := range subs {
			if s == sub {
				b.subscribers[userID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(b.subscribers[userID]) == 0 {
			delete(b.subscribers, userID)
		}
		close(sub.ch)
	}

	return sub.ch, cleanup
}

// compareEventIDs orders broker-issued numeric IDs. Falls back to string
// comparison for non-numeric IDs (which the broker itself never produces).
func compareEventIDs(a, b string) int {
	ai, errA := strconv.ParseUint(a, 10, 64)
	bi, errB := strconv.ParseUint(b, 10, 64)
	if errA == nil && errB == nil {
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		default:
			return 0
		}
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
