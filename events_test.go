package golongpoll

import (
	"container/list"
	"fmt"
	"testing"
	"time"
)

func Test_eventBuffer_QueueEvent(t *testing.T) {
	testStart := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(testStart),
	}
	var events = []Event{
		newEventWithTime(testStart.Add(-10*time.Second), "red", "some string data 1"),
		newEventWithTime(testStart.Add(-5*time.Second), "blue", "some string data 2"),
		newEventWithTime(testStart, "red", "some string data 3"),
	}
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
		event, ok := element.Value.(*Event)
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
	testStart := time.Now()
	max := 10         // how large our buffer is
	moreThanMax := 55 // how many events to queue
	buffer := eventBuffer{
		list.New(),
		max, // max buffer size
		timeToEpochMilliseconds(testStart),
	}
	// Create moreThanMax events
	events := make([]Event, 0)
	for i := 0; i < moreThanMax; i++ {
		events = append(events,
			newEventWithTime(testStart.Add(time.Duration(i)*time.Second),
				"banana",
				fmt.Sprintf("some string data %d", i)))
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
		event, ok := element.Value.(*Event)
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
	testStart := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(testStart),
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
	if buffer.oldestEventTime != timeToEpochMilliseconds(testStart) {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, testStart)
	}
}

func Test_eventBuffer_GetEventsSince(t *testing.T) {
	testStart := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(testStart),
	}
	// Three of the events in our buffer occurred after our since param
	since := testStart.Add(-9 * time.Second)
	var events = []Event{
		newEventWithTime(testStart.Add(-12*time.Second), "red", "some string data 1"),
		newEventWithTime(testStart.Add(-11*time.Second), "blue", "some string data 2"),
		newEventWithTime(testStart.Add(-8*time.Second), "red", "some string data 3"),
		newEventWithTime(testStart.Add(-5*time.Second), "blue", "some string data 4"),
		newEventWithTime(testStart, "red", "some string data 5"),
	}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	if buffer.oldestEventTime != events[0].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, testStart)
	}
	// Get all events since 9 seconds ago (last 3 events)
	gotEvents, err := buffer.GetEventsSince(since, false, nil)
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
			buffer.oldestEventTime, testStart)
	}
}

// Tests the new, optional (can be nil) lastEventUUID argument
// that was added to fix issue #19.
func Test_eventBuffer_GetEventsSince_lastEventUUID(t *testing.T) {
	testStart := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(testStart),
	}

	var events = []Event{
		newEventWithTime(testStart.Add(-3*time.Second), "news", "Cheese is good."),
		newEventWithTime(testStart.Add(-2*time.Second), "news", "I like cheese."),
		newEventWithTime(testStart.Add(-2*time.Second), "news", "We are out of cheese."),
		newEventWithTime(testStart.Add(-2*time.Second), "news", "Perhaps we should buy more."),
		newEventWithTime(testStart, "news", "Capital idea!"),
	}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}

	// Get all events, then we can use their ID values for more scenarios below.
	// Getting events since a second before oldest time will yield all 5 events.
	since := testStart.Add(-4 * time.Second)
	allEvents, err := buffer.GetEventsSince(since, false, nil)
	if err != nil {
		t.Fatalf("Got error %q from GetEventsSince with since: %v", err, since)
	}
	if len(allEvents) != 5 {
		t.Fatalf("GetEventsSince returned unexpected number of events. "+
			"Expected: %d, Got: %d", 5, len(allEvents))
	}

	// Make sure in expected order:
	if allEvents[0].Data != "Cheese is good." {
		t.Fatalf("Expected data to be: %v, but got: %v", "Cheese is good.", allEvents[0].Data)
	}

	if allEvents[0].Timestamp != timeToEpochMilliseconds(testStart.Add(-3*time.Second)) {
		t.Fatalf("Expected timestamp to be: %v, but got: %v", testStart.Add(-3*time.Second), allEvents[0].Data)
	}

	if allEvents[1].Data != "I like cheese." {
		t.Fatalf("Expected data to be: %v, but got: %v", "I like cheese.", allEvents[1].Data)
	}

	if allEvents[1].Timestamp != timeToEpochMilliseconds(testStart.Add(-2*time.Second)) {
		t.Fatalf("Expected timestamp to be: %v, but got: %v", testStart.Add(-2*time.Second), allEvents[1].Data)
	}

	if allEvents[2].Data != "We are out of cheese." {
		t.Fatalf("Expected data to be: %v, but got: %v", "We are out of cheese.", allEvents[2].Data)
	}

	if allEvents[2].Timestamp != timeToEpochMilliseconds(testStart.Add(-2*time.Second)) {
		t.Fatalf("Expected timestamp to be: %v, but got: %v", testStart.Add(-2*time.Second), allEvents[2].Data)
	}

	if allEvents[3].Data != "Perhaps we should buy more." {
		t.Fatalf("Expected data to be: %v, but got: %v", "Perhaps we should buy more.", allEvents[3].Data)
	}

	if allEvents[3].Timestamp != timeToEpochMilliseconds(testStart.Add(-2*time.Second)) {
		t.Fatalf("Expected timestamp to be: %v, but got: %v", testStart.Add(-2*time.Second), allEvents[3].Data)
	}

	if allEvents[4].Data != "Capital idea!" {
		t.Fatalf("Expected data to be: %v, but got: %v", "Capital idea!", allEvents[4].Data)
	}

	if allEvents[4].Timestamp != timeToEpochMilliseconds(testStart) {
		t.Fatalf("Expected timestamp to be: %v, but got: %v", testStart, allEvents[4].Data)
	}

	// Asking for all since the oldest event's time will yield the other 4/5
	eventsSinceMinusThree, _ := buffer.GetEventsSince(testStart.Add(-3*time.Second), false, nil)
	if len(eventsSinceMinusThree) != 4 {
		t.Fatalf("GetEventsSince returned unexpected number of events. "+
			"Expected: %d, Got: %d", 4, len(eventsSinceMinusThree))
	}
	if eventsSinceMinusThree[0].ID != allEvents[1].ID {
		t.Errorf("Expected first event of eventsSinceMinusThree be allEvent's second event.")
	}

	// Asking for all since -2 seconds from start will yield only the last event as there
	// are 3 events all with at same -2 time. This is the essence of issue #19 as clients
	// would never see subsequent events having the same timestamp.
	eventsSinceMinusTWo, _ := buffer.GetEventsSince(testStart.Add(-2*time.Second), false, nil)
	if len(eventsSinceMinusTWo) != 1 {
		t.Fatalf("GetEventsSince returned unexpected number of events. "+
			"Expected: %d, Got: %d", 1, len(eventsSinceMinusTWo))
	}
	if eventsSinceMinusTWo[0].ID != allEvents[4].ID {
		t.Errorf("Expected first event of eventsSinceMinusTWo be allEvent's 5th event.")
	}

	// Now ask for all since -2 seconds, but using first event's ID as last_id, expect to get all 3
	// at time -2 and the later (5th original) event as well. This last_id mechanism is how
	// we can get a subset of events that fall within the same timestamp as we will
	// return any event with Timestamp == since_time (instead of > since_time) as long as
	// we haven't hit last_id yet as the data is ordered newest-first internally.
	// Notice how if none of the events at a given since_time have the provided last_id
	// as an ID, then you will get all events at since_time included.
	eventsSinceMinusTWoWithFirstID, _ := buffer.GetEventsSince(testStart.Add(-2*time.Second), false, &allEvents[0].ID)
	if len(eventsSinceMinusTWoWithFirstID) != 4 {
		t.Fatalf("GetEventsSince returned unexpected number of events. "+
			"Expected: %d, Got: %d", 4, len(eventsSinceMinusTWoWithFirstID))
	}
	if eventsSinceMinusTWoWithFirstID[0].ID != allEvents[1].ID {
		t.Errorf("Expected first event of eventsSinceMinusTWoWithFirstID be allEvent's 2nd event.")
	}

	// Now ask for since time -2, but with the last_id of the first -2 time event (2nd original event)
	eventsSinceMinusTWoWithSecondID, _ := buffer.GetEventsSince(testStart.Add(-2*time.Second), false, &allEvents[1].ID)
	if len(eventsSinceMinusTWoWithSecondID) != 3 {
		t.Fatalf("GetEventsSince returned unexpected number of events. "+
			"Expected: %d, Got: %d", 3, len(eventsSinceMinusTWoWithSecondID))
	}
	if eventsSinceMinusTWoWithSecondID[0].ID != allEvents[2].ID {
		t.Errorf("Expected first event of eventsSinceMinusTWoWithSecondID be allEvent's 3rd event.")
	}

	// Ditto, but with 3rd original event's ID which will return 2 events: the last 2 original ones.
	eventsSinceMinusTWoWithThirdID, _ := buffer.GetEventsSince(testStart.Add(-2*time.Second), false, &allEvents[2].ID)
	if len(eventsSinceMinusTWoWithThirdID) != 2 {
		t.Fatalf("GetEventsSince returned unexpected number of events. "+
			"Expected: %d, Got: %d", 2, len(eventsSinceMinusTWoWithThirdID))
	}
	if eventsSinceMinusTWoWithThirdID[0].ID != allEvents[3].ID {
		t.Errorf("Expected first event of eventsSinceMinusTWoWithThirdID be allEvent's 4th event.")
	}

	// Finally, use the 4th event's ID and we'll get none of the time-2 events as we used
	// the last_id of the last of events with that timestamp.
	eventsSinceMinusTWoWithFourthID, _ := buffer.GetEventsSince(testStart.Add(-2*time.Second), false, &allEvents[3].ID)
	if len(eventsSinceMinusTWoWithFourthID) != 1 {
		t.Fatalf("GetEventsSince returned unexpected number of events. "+
			"Expected: %d, Got: %d", 1, len(eventsSinceMinusTWoWithFourthID))
	}
	if eventsSinceMinusTWoWithFourthID[0].ID != allEvents[4].ID {
		t.Errorf("Expected first event of eventsSinceMinusTWoWithFourthID be allEvent's 5th event.")
	}
}

func Test_eventBuffer_GetEventsSince_deleteFetchedEvents(t *testing.T) {
	// Now try the same test but with deleteFetchedEvents arg set to true
	// and confirm fetched events were removed
	testStart := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(testStart),
	}
	// Three of the events in our buffer occurred after our since param
	since := testStart.Add(-9 * time.Second)
	var events = []Event{
		newEventWithTime(testStart.Add(-12*time.Second), "red", "some string data 1"),
		newEventWithTime(testStart.Add(-11*time.Second), "blue", "some string data 2"),
		newEventWithTime(testStart.Add(-8*time.Second), "red", "some string data 3"),
		newEventWithTime(testStart.Add(-5*time.Second), "blue", "some string data 4"),
		newEventWithTime(testStart, "red", "some string data 5"),
	}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	if buffer.oldestEventTime != events[0].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, testStart)
	}
	// Get all events since 9 seconds ago (last 3 events)
	// NOTE: this time we specify that we should delete said fetched events
	gotEvents, err := buffer.GetEventsSince(since, true, nil)
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
			buffer.oldestEventTime, testStart)
	}
	// Now confirm that the buffer had thos events removed:
	if buffer.List.Len() != 2 {
		t.Errorf("buffer had unexpected number of events.  was: %d, expected: %d.",
			buffer.List.Len(), 2)
	}
	if buffer.List.Front().Value.(*Event) != &events[1] {
		t.Errorf("buffer had unexpected event at front. expected events[1].")
	}
	if buffer.List.Front().Next().Value.(*Event) != &events[0] {
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
	var events = []Event{
		newEventWithTime(time.Now().Add(-12*time.Second), "red", "some string data 1"),
		newEventWithTime(time.Now().Add(-11*time.Second), "blue", "some string data 2"),
		newEventWithTime(time.Now().Add(-8*time.Second), "red", "some string data 3"),
		newEventWithTime(time.Now().Add(-5*time.Second), "blue", "some string data 4"),
		newEventWithTime(time.Now(), "red", "some string data 5"),
	}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	// Get all events since 30 seconds ago (all 5 events)
	gotEvents, err := buffer.GetEventsSince(since, false, nil)
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
	var events = []Event{
		newEventWithTime(time.Now().Add(-12*time.Second), "red", "some string data 1"),
		newEventWithTime(time.Now().Add(-11*time.Second), "blue", "some string data 2"),
		newEventWithTime(time.Now().Add(-8*time.Second), "red", "some string data 3"),
		newEventWithTime(time.Now().Add(-5*time.Second), "blue", "some string data 4"),
		newEventWithTime(time.Now().Add(-3*time.Second), "red", "some string data 5"),
	}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	// Get all events since ~0 seconds ago  (no events)
	gotEvents, err := buffer.GetEventsSince(since, false, nil)
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
	gotEvents, err := buffer.GetEventsSince(since, false, nil)
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
	events, err := buffer.GetEventsSince(time.Now(), false, nil)
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
	testStart := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(testStart),
	}

	err := buffer.DeleteEventsOlderThan(timeToEpochMilliseconds(testStart.Add(1 * time.Second)))
	if err != nil {
		t.Errorf("DeleteEventsOlderThan returned non-nil error: %v", err)
	}
	if buffer.List.Len() != 0 {
		t.Errorf("Expected empty list, got %d items in list.", buffer.List.Len())
	}
}

func Test_eventBuffer_DeleteEventsOlderThan_InvalidData(t *testing.T) {
	testStart := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(testStart),
	}
	// Corrupt list by adding something that is not an Event:
	buffer.List.PushBack("asdf")
	// Confirm calls to operate on this list are now errors
	err := buffer.DeleteEventsOlderThan(timeToEpochMilliseconds(testStart.Add(1 * time.Second)))
	if err == nil {
		t.Errorf("DeleteEventsOlderThan returned nil error when non-nil error was expected.")
	}
	if buffer.List.Len() != 1 {
		t.Errorf("Expected 1 item list, got %d items in list.", buffer.List.Len())
	}
}

func Test_eventBuffer_DeleteEventsOlderThan(t *testing.T) {
	testStart := time.Now()
	buffer := eventBuffer{
		list.New(),
		10, // max buffer size
		timeToEpochMilliseconds(testStart),
	}
	var events = []Event{
		newEventWithTime(testStart.Add(-12*time.Second), "red", "some string data 1"),
		newEventWithTime(testStart.Add(-11*time.Second), "blue", "some string data 2"),
		newEventWithTime(testStart.Add(-8*time.Second), "red", "some string data 3"),
		newEventWithTime(testStart.Add(-5*time.Second), "blue", "some string data 4"),
		newEventWithTime(testStart, "red", "some string data 5"),
	}
	for i := range events {
		if err := buffer.QueueEvent(&events[i]); err != nil {
			t.Errorf("Event queue failed when it shouldn't have. Tried to queue: %q", events[i])
		}
	}
	if buffer.oldestEventTime != events[0].Timestamp {
		t.Errorf("eventBuffer.oldestEventTime was: %v, expected: %v.",
			buffer.oldestEventTime, testStart)
	}
	// Scenario 1: ask to delete older than a time older than the oldest event,
	// which means nothing gets deleted.
	if err := buffer.DeleteEventsOlderThan(timeToEpochMilliseconds(testStart.Add(-13 * time.Second))); err != nil {
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
			buffer.oldestEventTime, testStart)
	}

	// Scenario 2: ask to delete older than some time half way thru the events
	// so some of them are deleted
	if err := buffer.DeleteEventsOlderThan(timeToEpochMilliseconds(testStart.Add(-10 * time.Second))); err != nil {
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
			buffer.oldestEventTime, testStart)
	}

	// Scenario 3: ask to delete older than some time newer than all events,
	// so all events are deleted.
	// NOTE: asking to delete at time == newest event time,
	// this also tests that we are inclusive in our boundary, using <= not just <
	if err := buffer.DeleteEventsOlderThan(timeToEpochMilliseconds(testStart)); err != nil {
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
			buffer.oldestEventTime, testStart)
	}
}
