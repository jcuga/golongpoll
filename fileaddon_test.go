package golongpoll

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func Test_NewFilePersistor_Valid(t *testing.T) {
	filename := "golongpoll_unittest.tmp"

	// Successful calls to NewFilePersistor should create the provided
	// filename if it doesn't exist. Make sure we remove said file once done
	defer os.Remove(filename)

	if _, err := os.Stat(filename); err == nil {
		t.Error("Did not expect file to already exist")
		os.Remove(filename)
	}

	fp, err := NewFilePersistor(filename, 512, 3)

	if err != nil {
		t.Error("Expected nil error.")
	}

	if fp == nil {
		t.Error("Expected non-nil return value")
		return
	}

	if fp.filename != filename {
		t.Errorf("Unexpected filename member, expected: %v, got: %v", filename, fp.filename)
	}

	if fp.writeBufferSize != 512 {
		t.Errorf("Unexpected writeBufferSize member, expected: %v, got: %v", 512, fp.writeBufferSize)
	}

	if fp.writeFlushPeriodSeconds != 3 {
		t.Errorf("Unexpected writeFlushPeriodSeconds member, expected: %v, got: %v", 3, fp.writeFlushPeriodSeconds)
	}

	if _, err := os.Stat(filename); err != nil {
		t.Errorf("Expected NewFilePersistor to create file: %v", filename)
	}
}

func Test_NewFilePersistor_InvalidArgs(t *testing.T) {
	filename := "golongpoll_unittest.tmp"

	invalidArgTest(t, filename, 0, 5,
		"writeBufferSize must be > 0")
	invalidArgTest(t, filename, -1, 5,
		"writeBufferSize must be > 0")
	invalidArgTest(t, filename, 1024, 0,
		"writeFlushPeriodSeconds must be > 0")
	invalidArgTest(t, filename, 1024, -1,
		"writeFlushPeriodSeconds must be > 0")
	invalidArgTest(t, "", 1024, 5,
		"filename cannot be empty")
}

func invalidArgTest(t *testing.T, filename string, writeBufferSize int, writeFlushPeriod int,
	expectedError string) {
	fp, err := NewFilePersistor(filename, writeBufferSize, writeFlushPeriod)

	if fp != nil {
		t.Error("expected null return value")
		return
	}

	if err == nil {
		t.Error("Expected non-nill error")
		return
	}

	if err.Error() != expectedError {
		t.Errorf("Unexpected error response, expected: %v, got: %v", expectedError, err.Error())
	}

	// Since given invalid args, the provided filename should not have been created
	// Only check this if a filename was actually provided.
	if len(filename) > 0 {
		if _, err := os.Stat(filename); err == nil {
			t.Error("Did not expect file to be created")
			os.Remove(filename)
		}
	}
}

// Test using FilePersistorAddOn directly versus as an AddOn to Longpollmanager.
// Start with a newly created file, publish data, then shutdown.
// Start again, publish more, then stop.
// We should get consecutive data from the two runs.
func Test_FilePersistorAddOn_DirectEndToEnd(t *testing.T) {
	filename := "golongpoll_filepersist_e2e.tmp"
	defer os.Remove(filename)

	// NOTE the use of 10 second flush time, which will
	// demonstrate that OnShutdown() will call Flush()
	// since this test won't take long enough to rely on periodic flush.
	fp, err := NewFilePersistor(filename, 1024, 10)
	if err != nil {
		t.Error("Expected nil error.")
	}
	if fp == nil {
		t.Error("Expected non-nil return value")
		return
	}

	// Launch addon's run goroutine.
	// This also gets any pre-loaded event data.
	// Expect an empty, closed channel since no pre-existing events in file.
	startChan := fp.OnLongpollStart()
	_, ok := <-startChan
	if ok {
		t.Fatal("Expected to receive empty closed channel.")
	}

	// Publish some data
	now := time.Now()
	event1 := newEventWithTime(now.Add(-4*time.Second), "news", "cheese is good")
	event2 := newEventWithTime(now.Add(-2*time.Second), "facts", "i\n<3\ncheese")
	fp.OnPublish(event1)
	fp.OnPublish(event2)

	// Shutdown, this will flush buffered events to disk
	fp.OnShutdown()
	confirmEventsInFile(t, filename, event1, event2)

	// Start a new file persist addon with same filename
	// to demonstrate able to append to existing events.
	// NOTE: This time with a 2 second flush period.
	// We'll check file data after a 2+ second dealy to ensure flushed
	// before calling shutdown.
	fp, err = NewFilePersistor(filename, 4096, 2)
	if err != nil {
		t.Error("Expected nil error.")
	}
	if fp == nil {
		t.Error("Expected non-nil return value")
		return
	}

	// We should get the 2 existing events in the returned on-start channel
	startChan = fp.OnLongpollStart()
	start1, ok := <-startChan
	if !ok {
		t.Fatal("Expected to receive non-empty channel.")
	}
	start2, ok := <-startChan
	if !ok {
		t.Fatal("Expected to receive channel with a second event..")
	}
	_, ok = <-startChan
	if ok {
		t.Fatal("Expected to hit end of on-start channel.")
	}

	if *start1 != *event1 {
		t.Errorf("Unexpected 1st on-start channel event, expected: %v, got: %v", event1, start1)
	}

	if *start2 != *event2 {
		t.Errorf("Unexpected 1st on-start channel event, expected: %v, got: %v", event2, start2)
	}

	// Publish new data
	event3 := newEventWithTime(now.Add(-1*time.Second), "news", "cheese discovered on moon!")
	fp.OnPublish(event3)
	// Data is buffered and not flushed to disk yet, so only 2 events not 3 still
	confirmEventsInFile(t, filename, event1, event2)

	// Wait until just after flush period seconds (2) and confim event is now in file
	time.Sleep(2100 * time.Millisecond)
	confirmEventsInFile(t, filename, event1, event2, event3)

	// Now publish a final event and shutdown, confirming all 4 events in file
	// after shutdown completes.
	event4 := newEventWithTime(now.Add(-1*time.Second), "facts", "i am\ntired")
	fp.OnPublish(event4)
	fp.OnShutdown()
	confirmEventsInFile(t, filename, event1, event2, event3, event4)
}

func confirmEventsInFile(t *testing.T, filename string, expectedEvents ...*Event) {
	parsedEvents := make([]Event, 0)
	f, err := os.Open(filename)
	if err != nil {
		t.Fatalf("Failed to open file: %v, error: %v", filename, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event Event
		var line = scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		} else {
			parsedEvents = append(parsedEvents, event)
		}
	}

	if len(parsedEvents) != len(expectedEvents) {
		t.Fatalf("Unexpected number of events in file, expected: %v, got: %v", len(expectedEvents), len(parsedEvents))
	}

	for index, e := range expectedEvents {
		if *e != parsedEvents[index] {
			t.Fatalf("Unexpected event parsed from file at index: %v, expected: %v, got: %v", index, *e, parsedEvents[index])
		}
	}
}

// Ensure we're able to skip incomplete/non-parsable JSON and still
// get data before/after it. Also ensure skip frivolous newlines.
func Test_FilePersistorAddOn_CorruptFileHandling(t *testing.T) {
	filename := "golongpoll-corrupt-file-utest.tmp"
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	_, wErr := f.WriteString(
		`

{"timestamp":1614693211367,"category":"echo","data":"1. one for the money","id":"a6670793-2e06-4653-bcae-7eed98d9f7d1"}


{"timestamp":1614693216002,"category":"echo","data":"2. two for the show.","id":"d3e30040-c8a5-4fa4-863c-b67f7199f7b7"}


{"timestamp":1614693220260,"category":"echo","data":"3. three to get ready","id":"443c0218-2442-4a5c-9060-be8b95f206ba"}
timestamp":1614693238618,"category":"echo","data":"missing leading {","id":"7adeb875-2f05-47ef-9e32-9655d14f4f1e"}
{"ti
mestamp":1614828881552,"category":"echo","data":"random newline broken-ness","id":"abe02190-9921-45c2-8c91-6f7b3f39804

{"timestamp":1614693238618,"category":"echo","data":"4. and away we gooooo!","id":"7adeb875-2f05-47ef-9e32-9655d14f4f1e"}`)

	if wErr != nil {
		t.Errorf("Error writing to file, error: %v", wErr)
	}
	f.Close()
	defer os.Remove(f.Name())

	fp, err := NewFilePersistor(filename, 4096, 5)
	if err != nil {
		t.Error("Expected nil error.")
	}
	if fp == nil {
		t.Error("Expected non-nil return value")
		return
	}

	// Confirm we get the 4 good events from our corrupt file.
	// We won't see the incomplete/un-parsable data.
	startChan := fp.OnLongpollStart()
	confirmNextChannelEvent(t, startChan, "echo", "1. one for the money")
	confirmNextChannelEvent(t, startChan, "echo", "2. two for the show.")
	confirmNextChannelEvent(t, startChan, "echo", "3. three to get ready")
	confirmNextChannelEvent(t, startChan, "echo", "4. and away we gooooo!")
}

func confirmNextChannelEvent(t *testing.T, startChan <-chan *Event, category string, data string) {
	gotEvent, ok := <-startChan
	if !ok {
		t.Fatal("Expected to receive non-empty channel.")
	}

	if gotEvent == nil {
		t.Fatal("Expected non-nil event from channel")
	}

	if gotEvent.Category != category {
		t.Errorf("Unexpected event category, expected: %v, got: %v", category, gotEvent.Category)
	}

	if gotEvent.Data != data {
		t.Errorf("Unexpected event data, expected: %v, got: %v", data, gotEvent.Data)
	}
}
