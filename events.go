package longpoll

import (
	"container/list"
	"fmt"
	"time"
)

type Event struct {
	Timestamp int64  `json:"timestamp"` // milliseconds since epoch to match javascrits Date.getTime()
	Category  string `json:"category"`
	// NOTE: Data can be anything that is able to passed to json.Marshal()
	Data interface{} `json:"data"`
}

type eventResponse struct {
	Events *[]Event `json:"events"`
}

type eventBuffer struct {
	// Doubly linked list of events where new events are added to the back/tail
	// and old events are removed from the front/root
	// NOTE: this is efficient for front/back operations since it is
	// implemented as a ring with root.prev being the tail
	// SEE: https://golang.org/src/container/list/list.go
	*list.List
	MaxBufferSize int
}

func (eb *eventBuffer) QueueEvent(event *Event) error {
	// Cull our buffer if we're at max capacity
	if eb.List.Len() > eb.MaxBufferSize {
		oldestEvent := eb.List.Back()
		if oldestEvent != nil {
			eb.List.Remove(oldestEvent)
		}
	}
	// Add event to front of our list
	eb.List.PushFront(event)
	return nil
}

func (eb *eventBuffer) GetEventsSince(since time.Time) ([]Event, error) {
	events := make([]Event, 0) // NOTE: having init cap > 0 has zero value Event
	// structs which we don't want!
	// NOTE: events are bufferd with the most recent event at the front
	// So iterate in reverse to do chronoligical order/oldest first
	for element := eb.List.Back(); element != nil; element = element.Prev() {
		event, ok := element.Value.(*Event)
		if !ok {
			return nil, fmt.Errorf("Found non-event type in event buffer.")
		}
		// event time is after since time arg? convert since to epoch ms
		if event.Timestamp > timeToEpochMilliseconds(since) {
			events = append(events, Event{event.Timestamp, event.Category, event.Data})
		} else {
			// we've made it to events we've seen before, stop searching
			break
		}
	}
	// TODO: consider reversing order? perhaps make this an option?
	return events, nil
}
