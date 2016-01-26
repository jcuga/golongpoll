package golongpoll

import (
	"container/list"
	"fmt"
	"testing"
	"time"
)

func Test_eventBuffer_QueueEvent(t *testing.T) {
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(time.Now()),
	}
	var events = []lpEvent{
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-10 * time.Second)), "red", "some string data 1"},
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-5 * time.Second)), "blue", "some string data 2"},
		lpEvent{timeToEpochMilliseconds(time.Now()), "red", "some string data 3"}}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	// Verify data queued in expected order
	if buffer.List.Len() != 3 {
		t.Errorf("eventBuffer list length was: %d, expected: %d.", buffer.List.Len(), 3)
	}
	eventIndex := 0
	// NOTE how list stores recently queued at the front, so iterate in reverse
	// to get chronological order (oldest first)
	for element := buffer.List.Back(); element != nil; element = element.Prev() {
		// Make sure converts back to Event
		event, ok := element.Value.(*lpEvent)
		if !ok {
			t.Errorf("Found non-event type in event buffer at index %d.", eventIndex)
		}
		// Make sure is event we think it should be
		if *event != events[eventIndex] {
			t.Errorf("Wrong event at index %d.  Expected: %q, Actual %q.",
				eventIndex, events[eventIndex], event)
		}
		eventIndex += 1
	}
}

func Test_eventBuffer_QueueEvent_MaxBufferReached(t *testing.T) {
	max := 10         // how large our buffer is
	moreThanMax := 55 // how many events to queue
	buffer := eventBuffer{
		list.New(),
		max, // max buffer size
		timeToEpochMilliseconds(time.Now()),
	}
	// Create moreThanMax events
	events := make([]lpEvent, 0)
	for i := 0; i < moreThanMax; i++ {
		events = append(events, lpEvent{
			timeToEpochMilliseconds(time.Now().Add(time.Duration(i) * time.Second)),
			"banana",
			fmt.Sprintf("some string data %d", i)})
	}
	for i := range events {
		// NOTE queue should always succeed even when we're adding more than max
		// buffer size because the buffer will simply truncate the least
		// recently added events when it's out of space.
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf(
				"Event queue failed when it shouldn't have. Tried to queue: %q",
				events[i])
		}
	}
	// Verify that the last max-many events are what's left in the buffer
	if buffer.List.Len() != max {
		t.Errorf("eventBuffer list length was: %d, expected: %d.", buffer.List.Len(), max)
	}
	// we should see only the last max-many items, so start our comparison
	// offset at that point:
	eventIndex := moreThanMax - max
	for element := buffer.List.Back(); element != nil; element = element.Prev() {
		// Make sure converts back to Event
		event, ok := element.Value.(*lpEvent)
		if !ok {
			t.Errorf("Found non-event type in event buffer at index %d.", eventIndex)
		}
		// Make sure is event we think it should be
		if *event != events[eventIndex] {
			t.Errorf("Wrong event at index %d.  Expected: %q, Actual %q.",
				eventIndex, events[eventIndex], event)
		}
		eventIndex += 1
	}
}

func Test_eventBuffer_QueueEvent_InvalidInput(t *testing.T) {
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(time.Now()),
	}
	// You can't queue nil.  And trying to do so shouldn't change the buffer.
	if err := buffer.QueueEvent(nil); err == nil {
		t.Errorf(
			"Event queue succeeded when it shouldn't have. Tried to queue nil.")
	}
	if buffer.List.Len() != 0 {
		t.Errorf("Event list was modified after a failed queue.  "+
			"Expected len: 0, got: %d", buffer.List.Len())
	}
}

func Test_eventBuffer_GetEventsSince(t *testing.T) {
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(time.Now()),
	}
	// Three of the events in our buffer occurred after our since param
	since := time.Now().Add(-9 * time.Second)
	var events = []lpEvent{
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-12 * time.Second)), "red", "some string data 1"},
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-11 * time.Second)), "blue", "some string data 2"},
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-8 * time.Second)), "red", "some string data 3"},
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-5 * time.Second)), "blue", "some string data 4"},
		lpEvent{timeToEpochMilliseconds(time.Now()), "red", "some string data 5"}}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	// Get all events since 9 seconds ago (last 3 events)
	gotEvents, err := buffer.GetEventsSince(since, false) // TODO: add tests for doDelete = true
	if err != nil {
		t.Fatalf("Got error %q from GetEventsSince with since: %v", err, since)
	}
	if len(gotEvents) != 3 {
		t.Fatalf("GetEventsSince returned unexpected number of events. "+
			"Expected: %d, Got: %d", 3, len(gotEvents))
	}
	// Confirm the event's we got are the ones we expect:
	for i := range gotEvents {
		// i is 0, 1, 2
		// The event's we expect are events[2], [3], [4]
		if gotEvents[i] != events[i+2] {
			t.Fatalf("Expected: %q, got: %q", events[i+2], gotEvents[i])
		}
	}
}

func Test_eventBuffer_GetEventsSince_AllEvents(t *testing.T) {
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(time.Now()),
	}
	// All of the events in the buffer occurred after our since param
	since := time.Now().Add(-30 * time.Second)
	var events = []lpEvent{
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-12 * time.Second)), "red", "some string data 1"},
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-11 * time.Second)), "blue", "some string data 2"},
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-8 * time.Second)), "red", "some string data 3"},
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-5 * time.Second)), "blue", "some string data 4"},
		lpEvent{timeToEpochMilliseconds(time.Now()), "red", "some string data 5"}}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	// Get all events since 30 seconds ago (all 5 events)
	gotEvents, err := buffer.GetEventsSince(since, false) // TODO: add test for doDelete == true
	if err != nil {
		t.Fatalf("Got error %q from GetEventsSince with since: %v", err, since)
	}
	if len(gotEvents) != 5 {
		t.Fatalf("GetEventsSince returned unexpected number of events. "+
			"Expected: %d, Got: %d", 5, len(gotEvents))
	}
	// Confirm the event's we got are the ones we expect:
	for i := range gotEvents {
		if gotEvents[i] != events[i] {
			t.Fatalf("Expected: %q, got: %q", events[i+2], gotEvents[i])
		}
	}
}

func Test_eventBuffer_GetEventsSince_NoEvents(t *testing.T) {
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(time.Now()),
	}
	// None of the events in the buffer occurred after our since param
	since := time.Now()
	var events = []lpEvent{
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-12 * time.Second)), "red", "some string data 1"},
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-11 * time.Second)), "blue", "some string data 2"},
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-8 * time.Second)), "red", "some string data 3"},
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-5 * time.Second)), "blue", "some string data 4"},
		lpEvent{timeToEpochMilliseconds(time.Now().Add(-3 * time.Second)), "red", "some string data 5"}}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	// Get all events since ~0 seconds ago  (no events)
	gotEvents, err := buffer.GetEventsSince(since, false) // TODO: add test for doDelete == true
	if err != nil {
		t.Fatalf("Got error %q from GetEventsSince with since: %v", err, since)
	}
	if len(gotEvents) != 0 {
		t.Fatalf("GetEventsSince returned unexpected number of events. "+
			"Expected: %d, Got: %d", 0, len(gotEvents))
	}
}

func Test_eventBuffer_GetEventsSince_EmptyBuffer(t *testing.T) {
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(time.Now()),
	}
	// any events in the last 30 seconds
	since := time.Now().Add(-30 * time.Second)
	// No events to get.  Not an error, but just no Event results
	gotEvents, err := buffer.GetEventsSince(since, false) // TODO: add test for doDelete == True
	if err != nil {
		t.Fatalf("Got error %q from GetEventsSince with since: %v", err, since)
	}
	if len(gotEvents) != 0 {
		t.Fatalf("GetEventsSince returned unexpected number of events. "+
			"Expected: %d, Got: %d", 0, len(gotEvents))
	}
}

func Test_eventBuffer_GetEventsSince_InvalidItems(t *testing.T) {
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(time.Now()),
	}
	buffer.List.PushBack("some string.  clearly not an Event type")
	events, err := buffer.GetEventsSince(time.Now(), false) // TODO: add test for doDelete == true
	// This buffer is hosed because there is non-Event data in it.
	// All calls to GetEventsSince should fail.
	if err == nil {
		t.Fatalf("Expected non-nil error, got nil")
	}
	if len(events) != 0 {
		t.Fatalf("Expected empty events, got len: %d", len(events))
	}
}

// TODO: test eventBuffer.oldestEventTime
