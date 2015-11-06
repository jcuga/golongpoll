package golongpoll

import (
	"container/list"
	"errors"
	"fmt"
	"time"
)

// lpEvent is a longpoll event.  This type has a Timestamp as milliseconds since
// epoch (UTC), a string category, and an arbitrary Data payload.
// The category is the subscription category/topic that clients can listen for
// via longpolling.  The Data payload can be anything that is JSON serializable
// via the encoding/json library's json.Marshal function.
type lpEvent struct {
	// Timestamp is milliseconds since epoch to match javascrits Date.getTime()
	Timestamp int64  `json:"timestamp"`
	Category  string `json:"category"`
	// NOTE: Data can be anything that is able to passed to json.Marshal()
	Data interface{} `json:"data"`
}

// eventResponse is the json response that carries longpoll events.
type eventResponse struct {
	Events *[]lpEvent `json:"events"`
}

// eventBuffer is a buffer of Events that adds new events to the front/root and
// and old events are removed from the back/tail when the buffer reaches it's
// maximum capacity.
// NOTE: this add-new-to-front/remove-old-from-back behavior is fairly
// efficient since it is implemented as a ring with root.prev being the tail.
// Unlike an array, we don't have to shift every element when something gets
// added to the front, and because our root has a root.prev reference, we can
// quickly jump from the root to the tail instead of having to follow every
// node's node.next field to finally reach the end.
// For more details on our list's implementation, see:
// https://golang.org/src/container/list/list.go
type eventBuffer struct {
	*list.List
	MaxBufferSize int
}

// QueueEvent adds a new longpoll Event to the front of our buffer and removes
// the oldest event from the back of the buffer if we're already at maximum
// capacity.
func (eb *eventBuffer) QueueEvent(event *lpEvent) error {
	if event == nil {
		return errors.New("event was nil")
	}
	// Cull our buffer if we're at max capacity
	if eb.List.Len() >= eb.MaxBufferSize {
		oldestEvent := eb.List.Back()
		if oldestEvent != nil {
			eb.List.Remove(oldestEvent)
		}
	}
	// Add event to front of our list
	eb.List.PushFront(event)
	return nil
}

// GetEventsSnce will return all of the Events in our buffer that occurred after
// the given input time (since).  Returns an error value if there are any
// objects that aren't an Event type in the buffer.  (which would be weird...)
func (eb *eventBuffer) GetEventsSince(since time.Time) ([]lpEvent, error) {
	events := make([]lpEvent, 0)
	// NOTE: events are bufferd with the most recent event at the front.
	// So we want to start our search at the front of the buffer and stop
	// searching once we've reached events that are older than the 'since'
	// argument.  But we want to return the subset of events in chronological
	// order, which is least recent in front.  So do our search from the
	// start so we can cut out early, but then iterate back from our last
	// item we want to return as a result.  Doing this avoids having to capture
	// results and then create another copy of the results but in reverse
	// order.
	var lastGoodItem *list.Element
	// Search forward until we reach events that are too old
	for element := eb.List.Front(); element != nil; element = element.Next() {
		event, ok := element.Value.(*lpEvent)
		if !ok {
			return events, fmt.Errorf("Found non-event type in event buffer.")
		}
		// is event time after 'since' time arg? convert 'since' to epoch ms
		if event.Timestamp > timeToEpochMilliseconds(since) {
			lastGoodItem = element
		} else {
			// we've reached items that are too old, they occurred before or on
			// 'since' so we don't care about anything after this point.
			break
		}
	}
	// Now accumulate results in the correct chronological order starting from
	// our oldest, valid Event that occurrs after 'since'
	if lastGoodItem != nil {
		for element := lastGoodItem; element != nil; element = element.Prev() {
			event, ok := element.Value.(*lpEvent)
			if !ok {
				return events, fmt.Errorf(
					"Found non-event type in event buffer.")
			}
			// we already know this event is after 'since'
			events = append(events,
				lpEvent{event.Timestamp, event.Category, event.Data})
		}
	}
	return events, nil
}
