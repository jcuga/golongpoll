package golongpoll

import (
	"container/list"
	"fmt"
	"testing"
	"time"
)

func Test_eventBuffer_QueueEvent(t *testing.T) {
	test_start := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(test_start),
	}
	var events = []lpEvent{
		lpEvent{timeToEpochMilliseconds(test_start.Add(-10 * time.Second)), "red", "some string data 1"},
		lpEvent{timeToEpochMilliseconds(test_start.Add(-5 * time.Second)), "blue", "some string data 2"},
		lpEvent{timeToEpochMilliseconds(test_start), "red", "some string data 3"}}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
		// oldestEventTime should be the oldest event in the buffer, which is
		// the first event we added
		if buffer.oldestEventTime != events[0].Timestamp {
			t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
				buffer.oldestEventTime, events[0].Timestamp)
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
	test_start := time.Now()
	max := 10         // how large our buffer is
	moreThanMax := 55 // how many events to queue
	buffer := eventBuffer{
		list.New(),
		max, // max buffer size
		timeToEpochMilliseconds(test_start),
	}
	// Create moreThanMax events
	events := make([]lpEvent, 0)
	for i := 0; i < moreThanMax; i++ {
		events = append(events, lpEvent{
			timeToEpochMilliseconds(test_start.Add(time.Duration(i) * time.Second)),
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
	// Oldest event time should be the timestamp of the last event in the buffer
	// which is event index 45 since 55 were added but our max buffer caps at
	// 10, so we only have events 45-54, index 45 is the oldest
	if buffer.oldestEventTime != events[45].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, events[45].Timestamp)
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
	test_start := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(test_start),
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
	// this value should remain unchanged:
	if buffer.oldestEventTime != timeToEpochMilliseconds(test_start) {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, test_start)
	}
}

func Test_eventBuffer_GetEventsSince(t *testing.T) {
	test_start := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(test_start),
	}
	// Three of the events in our buffer occurred after our since param
	since := test_start.Add(-9 * time.Second)
	var events = []lpEvent{
		lpEvent{timeToEpochMilliseconds(test_start.Add(-12 * time.Second)), "red", "some string data 1"},
		lpEvent{timeToEpochMilliseconds(test_start.Add(-11 * time.Second)), "blue", "some string data 2"},
		lpEvent{timeToEpochMilliseconds(test_start.Add(-8 * time.Second)), "red", "some string data 3"},
		lpEvent{timeToEpochMilliseconds(test_start.Add(-5 * time.Second)), "blue", "some string data 4"},
		lpEvent{timeToEpochMilliseconds(test_start), "red", "some string data 5"}}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	if buffer.oldestEventTime != events[0].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, test_start)
	}
	// Get all events since 9 seconds ago (last 3 events)
	gotEvents, err := buffer.GetEventsSince(since, false)
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
	// Remains unchanged:
	if buffer.oldestEventTime != events[0].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, test_start)
	}
}

func Test_eventBuffer_GetEventsSince_deleteFetchedEvents(t *testing.T) {
	// Now try the same test but with deleteFetchedEvents arg set to true
	// and confirm fetched events were removed
	test_start := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(test_start),
	}
	// Three of the events in our buffer occurred after our since param
	since := test_start.Add(-9 * time.Second)
	var events = []lpEvent{
		lpEvent{timeToEpochMilliseconds(test_start.Add(-12 * time.Second)), "red", "some string data 1"},
		lpEvent{timeToEpochMilliseconds(test_start.Add(-11 * time.Second)), "blue", "some string data 2"},
		lpEvent{timeToEpochMilliseconds(test_start.Add(-8 * time.Second)), "red", "some string data 3"},
		lpEvent{timeToEpochMilliseconds(test_start.Add(-5 * time.Second)), "blue", "some string data 4"},
		lpEvent{timeToEpochMilliseconds(test_start), "red", "some string data 5"}}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	if buffer.oldestEventTime != events[0].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, test_start)
	}
	// Get all events since 9 seconds ago (last 3 events)
	// NOTE: this time we specify that we should delete said fetched events
	gotEvents, err := buffer.GetEventsSince(since, true)
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
	// Remains unchanged.  It's not worth updating the oldest time when removing
	// via this function because depending on the since argument you may or may
	// not be removing events at the end.  Rather than adding the logic to
	// conditionally update oldest event time, we leave it be.
	// This is being lazy, but does not break any other behavior.
	if buffer.oldestEventTime != events[0].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, test_start)
	}
	// Now confirm that the buffer had thos events removed:
	if buffer.List.Len() != 2 {
		t.Errorf("buffer had unexpected number of events.  was: %d, expected: %d.",
			buffer.List.Len(), 2)
	}
	if buffer.List.Front().Value.(*lpEvent) != &events[1] {
		t.Errorf("buffer had unexpected event at front. expected events[1].")
	}
	if buffer.List.Front().Next().Value.(*lpEvent) != &events[0] {
		t.Errorf("buffer had unexpected event at second item. expected events[0].")
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
	gotEvents, err := buffer.GetEventsSince(since, false)
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
	gotEvents, err := buffer.GetEventsSince(since, false)
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
	gotEvents, err := buffer.GetEventsSince(since, false)
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
	events, err := buffer.GetEventsSince(time.Now(), false)
	// This buffer is hosed because there is non-Event data in it.
	// All calls to GetEventsSince should fail.
	if err == nil {
		t.Fatalf("Expected non-nil error, got nil")
	}
	if len(events) != 0 {
		t.Fatalf("Expected empty events, got len: %d", len(events))
	}
}

func Test_eventBuffer_DeleteEventsOlderThan_Trivial(t *testing.T) {
	test_start := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(test_start),
	}

	err := buffer.DeleteEventsOlderThan(timeToEpochMilliseconds(test_start.Add(1 * time.Second)))
	if err != nil {
		t.Errorf("DeleteEventsOlderThan returned non-nil error: %v", err)
	}
	if buffer.List.Len() != 0 {
		t.Errorf("Expected empty list, got %d items in list.", buffer.List.Len())
	}
}

func Test_eventBuffer_DeleteEventsOlderThan_InvalidData(t *testing.T) {
	test_start := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(test_start),
	}
	// Corrupt list by adding something that is not an lpEvent:
	buffer.List.PushBack("asdf")
	// Confirm calls to operate on this list are now errors
	err := buffer.DeleteEventsOlderThan(timeToEpochMilliseconds(test_start.Add(1 * time.Second)))
	if err == nil {
		t.Errorf("DeleteEventsOlderThan returned nil error when non-nil error was expected.")
	}
	if buffer.List.Len() != 1 {
		t.Errorf("Expected 1 item list, got %d items in list.", buffer.List.Len())
	}
}

func Test_eventBuffer_DeleteEventsOlderThan(t *testing.T) {
	test_start := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(test_start),
	}
	var events = []lpEvent{
		lpEvent{timeToEpochMilliseconds(test_start.Add(-12 * time.Second)), "red", "some string data 1"},
		lpEvent{timeToEpochMilliseconds(test_start.Add(-11 * time.Second)), "blue", "some string data 2"},
		lpEvent{timeToEpochMilliseconds(test_start.Add(-8 * time.Second)), "red", "some string data 3"},
		lpEvent{timeToEpochMilliseconds(test_start.Add(-5 * time.Second)), "blue", "some string data 4"},
		lpEvent{timeToEpochMilliseconds(test_start), "red", "some string data 5"}}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	if buffer.oldestEventTime != events[0].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, test_start)
	}
	// Scenario 1: ask to delete older than a time older than the oldest event,
	// which means nothing gets deleted.
	if err := buffer.DeleteEventsOlderThan(timeToEpochMilliseconds(test_start.Add(-13 * time.Second))); err != nil {
		t.Errorf("Unexpected error during DeleteEventsOlderThan: %v", err)
	}
	// No events removed
	if buffer.List.Len() != len(events) {
		t.Errorf("Unexpected number of events left in buffer. was: %d, expected: %d.",
			buffer.List.Len(), len(events))
	}
	// oldestEventTime unchanged
	if buffer.oldestEventTime != events[0].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, test_start)
	}

	// Scenario 2: ask to delete older than some time half way thru the events
	// so some of them are deleted
	if err := buffer.DeleteEventsOlderThan(timeToEpochMilliseconds(test_start.Add(-10 * time.Second))); err != nil {
		t.Errorf("Unexpected error during DeleteEventsOlderThan: %v", err)
	}
	// Oldest 2 events removed
	if buffer.List.Len() != 3 {
		t.Errorf("Unexpected number of events left in buffer. was: %d, expected: %d.",
			buffer.List.Len(), 3)
	}
	// oldestEventTime updated to the oldest event that is now in buffer
	if buffer.oldestEventTime != events[2].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, test_start)
	}

	// Scenario 3: ask to delete older than some time newer than all events,
	// so all events are deleted.
	// NOTE: asking to delete at time == newest event time,
	// this also tests that we are inclusive in our boundary, using <= not just <
	if err := buffer.DeleteEventsOlderThan(timeToEpochMilliseconds(test_start)); err != nil {
		t.Errorf("Unexpected error during DeleteEventsOlderThan: %v", err)
	}
	// Remaining 3 events removed
	if buffer.List.Len() != 0 {
		t.Errorf("Unexpected number of events left in buffer. was: %d, expected: %d.",
			buffer.List.Len(), 0)
	}
	// as events are removed oldestEventTime is set to the next event,
	// so now that we're empty, our oldestEventTime member just mirrors the
	// oldest event that was removed
	if buffer.oldestEventTime != events[4].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, test_start)
	}
}
