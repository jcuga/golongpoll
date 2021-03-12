package golongpoll

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofrs/uuid"
)

type CloseNotifierRecorder struct {
	httptest.ResponseRecorder
	CloseNotifier chan bool
}

// As it turns out, httptest.ResponseRecorder (returned by httptest.NewRecorder)
// does not support CloseNotify, so mock it to avoid panics about not supporting
// the interface
func (cnr *CloseNotifierRecorder) CloseNotify() <-chan bool {
	return cnr.CloseNotifier
}

func NewCloseNotifierRecorder() *CloseNotifierRecorder {
	return &CloseNotifierRecorder{
		httptest.ResponseRecorder{
			HeaderMap: make(http.Header),
			Body:      new(bytes.Buffer),
			Code:      200,
		},
		make(chan bool, 1),
	}
}

//gocyclo:ignore
func Test_LongpollManager_Publish(t *testing.T) {
	manager, err := StartLongpoll(Options{
		LoggingEnabled: true,
	})
	// Confirm the create call worked, and our manager has the expected values
	if err != nil {
		t.Errorf("Failed to create default LongpollManager.  Error was: %q", err)
	}
	if len(manager.eventsIn) != 0 {
		t.Errorf("Expected event channel to be initially empty. Instead len: %d",
			len(manager.eventsIn))
	}
	if len(manager.subManager.SubEventBuffer) != 0 {
		t.Errorf("Expected sub manager's event map to be initially empty. Instead len: %d",
			len(manager.subManager.SubEventBuffer))
	}
	err = manager.Publish("fruits", "apple")
	if err != nil {
		t.Errorf("Unexpected error publishing event: %q", err)
	}
	// SubscriptionManager's goroutine should not have picked up event yet:
	// I *think* this should always work because we don't typically yield
	// execution to other goroutines until an end of a func reached,
	// or a channel/network/sleep call is invoked.
	if len(manager.eventsIn) != 1 {
		t.Errorf("Expected event channel to have 1 event. Instead len: %d",
			len(manager.eventsIn))
	}
	// Allow sub manager's goroutine time to pull from channel.
	// This sleep should cause us to yield and let the other goroutine run
	time.Sleep(50 * time.Millisecond)
	if len(manager.eventsIn) != 0 {
		t.Errorf("Expected event channel to have 0 events. Instead len: %d",
			len(manager.eventsIn))
	}
	// Confirm event wound up in the sub manager's internal map:
	if len(manager.subManager.SubEventBuffer) != 1 {
		t.Errorf("Expected sub manager's event map to have 1 item. Instead len: %d",
			len(manager.subManager.SubEventBuffer))
	}
	buf, found := manager.subManager.SubEventBuffer["fruits"]
	if !found {
		t.Errorf("Failed to find event in sub manager's category-to-eventBuffer map")
	}
	// Double check that the expected max buffer size and capacity were set
	if buf.eventBufferPtr.MaxBufferSize != 250 {
		t.Errorf("Expected max buffer size of %d, but got %d.", 250, buf.eventBufferPtr.MaxBufferSize)
	}
	if buf.eventBufferPtr.List.Len() != 1 {
		t.Errorf("Expected buffer to be 1 item. instead: %d", buf.eventBufferPtr.List.Len())
	}
	if buf.eventBufferPtr.Front().Value.(*Event).Data != "apple" {
		t.Errorf("Expected event data to be %q, but got %q", "apple",
			buf.eventBufferPtr.Front().Value.(*Event).Data)
	}

	// Publish two more events
	err = manager.Publish("veggies", "potato")
	if err != nil {
		t.Errorf("Unexpected error publishing event: %q", err)
	}
	err = manager.Publish("fruits", "orange")
	if err != nil {
		t.Errorf("Unexpected error publishing event: %q", err)
	}
	// Allow other goroutine a chance to do a channel read
	time.Sleep(50 * time.Millisecond)

	if len(manager.eventsIn) != 0 {
		t.Errorf("Expected event channel to have 0 events. Instead len: %d",
			len(manager.eventsIn))
	}
	if len(manager.subManager.SubEventBuffer) != 2 {
		t.Errorf("Expected sub manager's event map to have 2 item. Instead len: %d",
			len(manager.subManager.SubEventBuffer))
	}
	buf, found = manager.subManager.SubEventBuffer["fruits"]
	if !found {
		t.Errorf("Failed to find event in sub manager's category-to-eventBuffer map")
	}
	if buf.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Expected buffer to be 2 items. instead: %d", buf.eventBufferPtr.List.Len())
	}
	if buf.eventBufferPtr.Front().Value.(*Event).Data != "orange" {
		t.Errorf("Expected event data to be %q, but got %q", "orange",
			buf.eventBufferPtr.Front().Value.(*Event).Data)
	}
	buf, found = manager.subManager.SubEventBuffer["veggies"]
	if !found {
		t.Errorf("Failed to find event in sub manager's category-to-eventBuffer map")
	}
	if buf.eventBufferPtr.List.Len() != 1 {
		t.Errorf("Expected buffer to be 1 item. instead: %d", buf.eventBufferPtr.List.Len())
	}
	if buf.eventBufferPtr.Front().Value.(*Event).Data != "potato" {
		t.Errorf("Expected event data to be %q, but got %q", "potato",
			buf.eventBufferPtr.Front().Value.(*Event).Data)
	}
	// Don't forget to kill subscription manager's running goroutine
	manager.Shutdown()
}

//gocyclo:ignore
func Test_LongpollManager_Publish_MaxBufferSize(t *testing.T) {
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:     true,
		MaxEventBufferSize: 2,
	})

	if len(manager.subManager.SubEventBuffer) != 0 {
		t.Errorf("Expected sub manager's event map to be initially empty. Instead len: %d",
			len(manager.subManager.SubEventBuffer))
	}
	err := manager.Publish("fruits", "apple")
	if err != nil {
		t.Errorf("Unexpected error publishing event: %q", err)
	}
	err = manager.Publish("fruits", "banana")
	if err != nil {
		t.Errorf("Unexpected error publishing event: %q", err)
	}
	// yield so other goroutine can do channel reads
	time.Sleep(50 * time.Millisecond)
	if len(manager.eventsIn) != 0 {
		t.Errorf("Expected event channel to have 0 events. Instead len: %d",
			len(manager.eventsIn))
	}
	// Confirm events wound up in the sub manager's internal map:
	// NOTE: only one map entry because both events the same category
	if len(manager.subManager.SubEventBuffer) != 1 {
		t.Errorf("Expected sub manager's event map to have 1 item. Instead len: %d",
			len(manager.subManager.SubEventBuffer))
	}
	buf, found := manager.subManager.SubEventBuffer["fruits"]
	if !found {
		t.Errorf("Failed to find event in sub manager's category-to-eventBuffer map")
	}
	// Double check that the expected max buffer size and capacity were set
	if buf.eventBufferPtr.MaxBufferSize != 2 {
		t.Errorf("Expected max buffer size of %d, but got %d.", 2, buf.eventBufferPtr.MaxBufferSize)
	}
	if buf.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Expected buffer to be 2 items. instead: %d", buf.eventBufferPtr.List.Len())
	}
	if buf.eventBufferPtr.Front().Value.(*Event).Data != "banana" {
		t.Errorf("Expected event data to be %q, but got %q", "banana",
			buf.eventBufferPtr.Front().Value.(*Event).Data)
	}
	if buf.eventBufferPtr.Back().Value.(*Event).Data != "apple" {
		t.Errorf("Expected event data to be %q, but got %q", "apple",
			buf.eventBufferPtr.Front().Value.(*Event).Data)
	}

	// Now try and publish another event on the same fruit category,
	// confirm that it works, but the oldest fruit is no longer in buffer
	err = manager.Publish("fruits", "pear")
	if err != nil {
		t.Errorf("Unexpected error publishing event: %q", err)
	}
	// yield so other goroutine can do channel reads
	time.Sleep(50 * time.Millisecond)
	buf, found = manager.subManager.SubEventBuffer["fruits"]
	if !found {
		t.Errorf("Failed to find event in sub manager's category-to-eventBuffer map")
	}
	// Double check that the expected max buffer size and capacity were set
	if buf.eventBufferPtr.MaxBufferSize != 2 {
		t.Errorf("Expected max buffer size of %d, but got %d.", 2, buf.eventBufferPtr.MaxBufferSize)
	}
	if buf.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Expected buffer to be 2 items. instead: %d", buf.eventBufferPtr.List.Len())
	}
	if buf.eventBufferPtr.Front().Value.(*Event).Data != "pear" {
		t.Errorf("Expected event data to be %q, but got %q", "banana",
			buf.eventBufferPtr.Front().Value.(*Event).Data)
	}
	if buf.eventBufferPtr.Back().Value.(*Event).Data != "banana" {
		t.Errorf("Expected event data to be %q, but got %q", "apple",
			buf.eventBufferPtr.Front().Value.(*Event).Data)
	}

	// Now confirm publishing on a different category still works
	err = manager.Publish("veggies", "potato")
	if err != nil {
		t.Errorf("Unexpected error publishing event: %q", err)
	}
	// yield so other goroutine can do channel reads
	time.Sleep(50 * time.Millisecond)
	buf, found = manager.subManager.SubEventBuffer["veggies"]
	if !found {
		t.Errorf("Failed to find event in sub manager's category-to-eventBuffer map")
	}
	if buf.eventBufferPtr.List.Len() != 1 {
		t.Errorf("Expected buffer to be 1 item. instead: %d", buf.eventBufferPtr.List.Len())
	}
	if buf.eventBufferPtr.Front().Value.(*Event).Data != "potato" {
		t.Errorf("Expected event data to be %q, but got %q", "potato",
			buf.eventBufferPtr.Front().Value.(*Event).Data)
	}
	// Don't forget to kill subscription manager's running goroutine
	manager.Shutdown()
}

func Test_LongpollManager_Publish_InvalidArgs(t *testing.T) {
	manager, err := StartLongpoll(Options{
		LoggingEnabled: true,
	})
	// Confirm the create call worked, and our manager has the expected values
	if err != nil {
		t.Errorf("Failed to create default LongpollManager.  Error was: %q", err)
	}
	// You must provide a category:
	err = manager.Publish("", "apple")
	if err == nil {
		t.Errorf("Expected calls to Publish with blank category would fail.")
	}
	// category can't be longer than 1024:
	// So 1024 len category should work:
	tooLong := ""
	for i := 0; i < 1024; i++ {
		tooLong += "a"
	}
	err = manager.Publish(tooLong, "apple")
	if err != nil {
		t.Errorf("Expected calls to Publish with 1024 len category not to fail, but got: %q.", err)
	}
	// But now that we're at 1025, we're boned.
	tooLong += "a"
	err = manager.Publish(tooLong, "apple")
	if err == nil {
		t.Errorf("Expected calls to Publish with blank category would fail.")
	}
}

func Test_LongpollManager_Shutdown(t *testing.T) {
	manager, err := StartLongpoll(Options{
		LoggingEnabled: true,
	})
	if err != nil {
		t.Errorf("Failed to create default LongpollManager.  Error was: %q", err)
	}
	manager.Shutdown()
	// Confirm the shutdown signal channel was closed (this is now the
	// goroutines are notified to quit)
	select {
	case _, isOpen := <-manager.subManager.Quit:
		if isOpen {
			t.Errorf("Expected channel to be closed, instead it was still open")
		}
	default:
		t.Errorf("Expected channel close, instead got no activity.")
	}
}

func Test_LongpollManager_newclientSubscription(t *testing.T) {
	subTime := time.Date(2015, 11, 7, 11, 33, 4, 0, time.UTC)
	sub, err := newclientSubscription("colors", subTime, nil)
	if err != nil {
		t.Errorf("Unexpected error when creating new client subscription: %q", err)
	}
	if sub.clientCategoryPair.SubscriptionCategory != "colors" {
		t.Errorf("Unexpected sub category, expected: %q. got: %q", "colors",
			sub.clientCategoryPair.SubscriptionCategory)
	}
	if sub.LastEventTime != subTime {
		t.Errorf("Unexpected sub last event time, expected: %q. got: %q", subTime,
			sub.LastEventTime)
	}
	if cap(sub.Events) != 1 {
		t.Errorf("Unexpected event channel capacity. expected: %q. got: %q", 1,
			cap(sub.Events))
	}

	u, _ := uuid.NewV4()
	sub, err = newclientSubscription("news", subTime, &u)

	if err != nil {
		t.Errorf("Unexpected error when creating new client subscription: %q", err)
	}
	if sub.clientCategoryPair.SubscriptionCategory != "news" {
		t.Errorf("Unexpected sub category, expected: %q. got: %q", "news",
			sub.clientCategoryPair.SubscriptionCategory)
	}
	if sub.LastEventID != &u {
		t.Errorf("Unexpected sub last event time, expected: %q. got: %q", u,
			sub.LastEventID)
	}
}

func ajaxHandler(handlerFunc func(w http.ResponseWriter, r *http.Request)) http.Handler {
	return http.HandlerFunc(handlerFunc)
}

//gocyclo:ignore
func Test_LongpollManager_WebClient_InvalidRequests(t *testing.T) {
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:     true,
		MaxEventBufferSize: 100,
	})
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)

	// Empty request, this is going to result in an JSON error object:
	req, _ := http.NewRequest("GET", "", nil)
	w := NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	// Also note how it says "1-120", so our custom timeout arg of 120 was
	// used
	if w.Body.String() != "{\"error\": \"Invalid timeout arg.  Must be 1-120.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Invalid timeout, not a number
	req, _ = http.NewRequest("GET", "?timeout=adf&category=veggies", nil)
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid timeout arg.  Must be 1-120.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Invalid timeout, too small
	req, _ = http.NewRequest("GET", "?timeout=0&category=veggies", nil)
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid timeout arg.  Must be 1-120.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Invalid timeout, too big
	req, _ = http.NewRequest("GET", "?timeout=121&category=veggies", nil)
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid timeout arg.  Must be 1-120.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Valid timeout, but missing category:
	req, _ = http.NewRequest("GET", "?timeout=30", nil)
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid subscription category, must be 1-1024 characters long.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Valid timeout, but category is too small
	req, _ = http.NewRequest("GET", "?timeout=30&category=", nil)
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid subscription category, must be 1-1024 characters long.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Valid timeout, but category too long
	tooLong := ""
	for i := 0; i < 1025; i++ {
		tooLong += "a"
	} // 1025 chars long
	req, _ = http.NewRequest("GET", "?timeout=30&category="+tooLong, nil)
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid subscription category, must be 1-1024 characters long.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Valid timeout, valid category, but invalid since_time
	req, _ = http.NewRequest("GET", "?timeout=30&category=foobar&since_time=asdf", nil)
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid since_time arg.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Don't forget to kill our pubsub manager's run goroutine
	manager.Shutdown()
}

func Test_LongpollManager_WebClient_NoEventsSoTimeout(t *testing.T) {
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:     true,
		MaxEventBufferSize: 100,
	})
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)

	// Valid request, but we don't have any events published,
	// so this will wait 2 seconds (because timeout param = 2)
	// and then come back with a timeout response
	req, _ := http.NewRequest("GET", "?timeout=2&category=veggies", nil)
	w := httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	approxTimeoutTime := timeToEpochMilliseconds(time.Now())
	var timeoutResp timeoutResponse
	if err := json.Unmarshal(w.Body.Bytes(), &timeoutResp); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if timeoutResp.TimeoutMessage != "no events before timeout" {
		t.Errorf("Unexpected timeout message: %q", timeoutResp.TimeoutMessage)
	}
	if timeoutResp.Timestamp < (approxTimeoutTime-100) ||
		timeoutResp.Timestamp > approxTimeoutTime {
		t.Errorf("Unexpected timeout timestamp.  Expected: %q, got: %q",
			approxTimeoutTime, timeoutResp.Timestamp)
	}

	// Don't forget to kill our pubsub manager's run goroutine
	manager.Shutdown()
}

func Test_LongpollManager_WebClient_Disconnect_RemoveClientSub(t *testing.T) {
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:     true,
		MaxEventBufferSize: 100,
	})
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)
	if _, found := manager.subManager.ClientSubChannels["veggies"]; found {
		t.Errorf("Expected client sub channel not to exist yet ")
	}
	// This request has timeout of 5 seconds, but we're going to simulate a
	// disconnect in 2 seconds which is earlier.
	req, _ := http.NewRequest("GET", "?timeout=5&category=veggies", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	// As of go 1.7, calls to t.Error, t.Fatal, after a test exits causes a panic.
	// Before, these were suppressed.  So as it turns out, this test's goroutine
	// that spawns here was outliving the test.  Now let's make the test body
	// explicitly wait for it.
	// see thread here: https://github.com/golang/go/issues/15976
	goroutineDone := make(chan bool)
	go func() {
		time.Sleep(time.Duration(250) * time.Millisecond)
		// confirm subscription entry exists, with one client
		if _, found := manager.subManager.ClientSubChannels["veggies"]; !found {
			t.Errorf("Expected client sub channel to exist")
		}
		if val, _ := manager.subManager.ClientSubChannels["veggies"]; len(val) != 1 {
			t.Errorf("Expected sub channel to have one client subscribed")
		}
		time.Sleep(time.Duration(2) * time.Second)
		// Confirm that the subscription entry no longer exists.
		// Before, this test asserted that the entry ('veggie' key in the map)
		// existed, but the value listed no clients.  But code was changed to auto
		// remove subscription keys in this map if there are no clients listening.
		// Since these asserts were erroneously firing after the test body exited
		// (and this is undefined behavior and pre go 1.7 it's simply ignored),
		// this bad assertion was never failing when it should have.
		// Once the test body was forced to wait for it's spawned goroutine to exit,
		// the old assertion started failing.
		// This test is now updated to assert that the key "veggies" no longer
		// exists since we're explicitly removing map entries when the value is
		// an empty container.
		if _, found := manager.subManager.ClientSubChannels["veggies"]; found {
			t.Errorf("Expected client sub channel to be auto removed when 0 clients.")
		}
		goroutineDone <- true
	}()

	subscriptionHandler.ServeHTTP(w, req)

	// causes test body to block until the above spawned goroutine finishes.
	// Otherwise this will panic in go 1.7 and later :)
	<-goroutineDone

	// Don't forget to kill our pubsub manager's run goroutine
	manager.Shutdown()
}

func Test_LongpollManager_WebClient_Disconnect_TerminateHttp(t *testing.T) {
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:     true,
		MaxEventBufferSize: 100,
	})
	testChannel := make(chan int, 2)
	webValue := 7
	goroutineValue := 13
	subscriptionHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		manager.SubscriptionHandler(w, r)
		testChannel <- webValue
	})
	if _, found := manager.subManager.ClientSubChannels["veggies"]; found {
		t.Errorf("Expected client sub channel not to exist yet ")
	}
	// This request has timeout of 5 seconds, but we're going to simulate a
	// disconnect in 1 second which is earlier.
	// If the wrapping subscription handler gets to the end, it will publish
	// the number 7 on our test channel. Have a goroutine publish a different
	// value at some time after the disconnect, but before the timeout
	// and confirm that the disconnect forced the subscription handler
	// to return early and thus we get the expected published value.
	req, _ := http.NewRequest("GET", "?timeout=5&category=veggies", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	go func() {
		time.Sleep(time.Duration(3) * time.Second)
		testChannel <- goroutineValue
	}()

	go func() {
		time.Sleep(time.Duration(250) * time.Millisecond)
		// confirm subscription entry exists, with one client
		if _, found := manager.subManager.ClientSubChannels["veggies"]; !found {
			t.Errorf("Expected client sub channel to exist")
		}
		if val, _ := manager.subManager.ClientSubChannels["veggies"]; len(val) != 1 {
			t.Errorf("Expected sub channel to have one client subscribed")
		}
		time.Sleep(time.Duration(3) * time.Second)
		// Confirm that our test channel has the value from our web handler and
		// not from our goroutine
		select {
		case val := <-testChannel:
			if val != webValue {
				t.Errorf("Expected to get channel send from http handler before the goroutine.")
			}
		}
	}()

	subscriptionHandler.ServeHTTP(w, req)
	// Don't forget to kill our pubsub manager's run goroutine
	manager.Shutdown()
}

//gocyclo:ignore
func Test_LongpollManager_WebClient_HasEvents(t *testing.T) {
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:     true,
		MaxEventBufferSize: 100,
	})
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)

	// Valid request, but we don't have any events published,
	// so this will wait for a publish or timeout (in this case we'll get
	// something)
	req, _ := http.NewRequest("GET", "?timeout=30&category=veggies", nil)
	w := httptest.NewRecorder()
	// Publish two events, only the second is for our subscription category
	// Note how these events occur after the client subscribed
	// if they occurred before, since we don't provide a since_time url param
	// we'd default to now and skip those events.
	startTime := time.Now()
	go func() {
		time.Sleep(1500 * time.Millisecond)
		manager.Publish("fruits", "peach")
		time.Sleep(1000 * time.Millisecond)
		manager.Publish("veggies", "corn")
	}()
	subscriptionHandler.ServeHTTP(w, req)

	// Confirm we got the correct event
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	var eventResponse eventResponse
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(eventResponse.Events) != 1 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 1, len(eventResponse.Events))
	}
	if (eventResponse.Events)[0].Category != "veggies" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggies", (eventResponse.Events)[0].Category)
	}
	if (eventResponse.Events)[0].Data != "corn" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "corn", (eventResponse.Events)[0].Data)
	}

	// Make a new subscription request.
	// Note how since there's no since_time url param, we default to now,
	// and thus don't see the previous event from our last http request
	req, _ = http.NewRequest("GET", "?timeout=2&category=veggies", nil)
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	approxTimeoutTime := timeToEpochMilliseconds(time.Now())
	var timeoutResp timeoutResponse
	if err := json.Unmarshal(w.Body.Bytes(), &timeoutResp); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if timeoutResp.TimeoutMessage != "no events before timeout" {
		t.Errorf("Unexpected timeout message: %q", timeoutResp.TimeoutMessage)
	}
	if timeoutResp.Timestamp < (approxTimeoutTime-100) ||
		timeoutResp.Timestamp > approxTimeoutTime {
		t.Errorf("Unexpected timeout timestamp.  Expected: %q, got: %q",
			approxTimeoutTime, timeoutResp.Timestamp)
	}

	// Now ask for events since the start of our test, which will include
	// our previously seen event
	req, _ = http.NewRequest("GET", fmt.Sprintf("?timeout=2&category=veggies&since_time=%d",
		timeToEpochMilliseconds(startTime)), nil)
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(eventResponse.Events) != 1 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 1, len(eventResponse.Events))
	}
	if (eventResponse.Events)[0].Category != "veggies" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggies", (eventResponse.Events)[0].Category)
	}
	if (eventResponse.Events)[0].Data != "corn" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "corn", (eventResponse.Events)[0].Data)
	}

	firstEventTime := (eventResponse.Events)[0].Timestamp
	manager.Publish("veggies", "carrot")
	time.Sleep(50 * time.Millisecond) // allow yield for goroutine channel reads

	// Now ask for any events since our first one, and confirm we get the second
	// 'veggie' category event
	req, _ = http.NewRequest("GET", fmt.Sprintf("?timeout=2&category=veggies&since_time=%d", firstEventTime), nil)
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(eventResponse.Events) != 1 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 1, len(eventResponse.Events))
	}
	if (eventResponse.Events)[0].Category != "veggies" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggies", (eventResponse.Events)[0].Category)
	}
	if (eventResponse.Events)[0].Data != "carrot" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "carrot", (eventResponse.Events)[0].Data)
	}

	// Confirm we get both events when asking for any events since start of test run
	req, _ = http.NewRequest("GET", fmt.Sprintf("?timeout=2&category=veggies&since_time=%d",
		timeToEpochMilliseconds(startTime)), nil)
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(eventResponse.Events) != 2 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 2, len(eventResponse.Events))
	}
	if (eventResponse.Events)[0].Data != "corn" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "corn", (eventResponse.Events)[0].Data)
	}
	if (eventResponse.Events)[1].Data != "carrot" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "carrot", (eventResponse.Events)[0].Data)
	}

	// Don't forget to kill our pubsub manager's run goroutine
	manager.Shutdown()
}

func Test_LongpollManager_WebClient_HasBufferedEvents(t *testing.T) {
	// Test behavior where clients can see events that happened before
	// they started their longpoll by accessing events in the
	// subscriptionManager's eventBuffer containers.
	// Of course, clients only see this if they request events with a
	// 'since_time' argument of a time earlier than the events occurred.
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:     true,
		MaxEventBufferSize: 100,
	})

	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)

	startTime := time.Now()
	time.Sleep(500 * time.Millisecond)
	manager.Publish("veggies", "broccoli")
	time.Sleep(500 * time.Millisecond)
	manager.Publish("veggies", "corn")
	time.Sleep(500 * time.Millisecond)

	// This request clearly takes place after the two events were published.
	// But we ask for any events since the start of this test case
	req, _ := http.NewRequest("GET", fmt.Sprintf("?timeout=2&category=veggies&since_time=%d",
		timeToEpochMilliseconds(startTime)), nil)
	w := httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)

	// Confirm we got the correct event
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	var eventResponse eventResponse
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(eventResponse.Events) != 2 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 2, len(eventResponse.Events))
	}
	if (eventResponse.Events)[0].Category != "veggies" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggies", (eventResponse.Events)[0].Category)
	}
	if (eventResponse.Events)[0].Data != "broccoli" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "broccoli", (eventResponse.Events)[0].Data)
	}
	if (eventResponse.Events)[1].Category != "veggies" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggies", (eventResponse.Events)[1].Category)
	}
	if (eventResponse.Events)[1].Data != "corn" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "corn", (eventResponse.Events)[1].Data)
	}

	// Don't forget to kill our pubsub manager's run goroutine
	manager.Shutdown()

}

func Test_LongpollManager_makeTimeoutResponse(t *testing.T) {
	now := time.Now()
	timeoutResp := makeTimeoutResponse(now)
	timeoutTime := timeToEpochMilliseconds(now)
	if timeoutResp.TimeoutMessage != "no events before timeout" {
		t.Errorf("Unexpected timeout message: %q", timeoutResp.TimeoutMessage)
	}
	if timeoutResp.Timestamp != timeoutTime {
		t.Errorf("Unexpected timeout timestamp.  Expected: %q, got: %q",
			timeoutTime, timeoutResp.Timestamp)
	}
}

//gocyclo:ignore
func Test_LongpollManager_StartLongpoll_Options(t *testing.T) {
	// Error cases due to invalid options:
	if _, err := StartLongpoll(Options{
		LoggingEnabled:            true,
		MaxLongpollTimeoutSeconds: 120,
		MaxEventBufferSize:        -1,
		EventTimeToLiveSeconds:    1,
	}); err == nil {
		t.Errorf("Expected error when passing MaxEventBufferSize that was < 0")
	}
	if _, err := StartLongpoll(Options{
		LoggingEnabled:            true,
		MaxLongpollTimeoutSeconds: -1,
		MaxEventBufferSize:        100,
		EventTimeToLiveSeconds:    1,
	}); err == nil {
		t.Errorf("Expected error when passing MaxLongpollTimeoutSeconds that was < 0")
	}
	if _, err := StartLongpoll(Options{
		LoggingEnabled:            true,
		MaxLongpollTimeoutSeconds: 120,
		MaxEventBufferSize:        100,
		EventTimeToLiveSeconds:    -1,
	}); err == nil {
		t.Errorf("Expected error when passing EventTimeToLiveSeconds that was < 0")
	}
	// Confirm valid options work
	// actual TTL
	if manager, err := StartLongpoll(Options{
		LoggingEnabled:            true,
		MaxLongpollTimeoutSeconds: 120,
		MaxEventBufferSize:        100,
		EventTimeToLiveSeconds:    30,
	}); err != nil {
		t.Errorf("Unxpected error when calling StartLongpoll with valid options")
	} else {
		manager.Shutdown()
	}

	// Forever
	if manager, err := StartLongpoll(Options{
		LoggingEnabled:            true,
		MaxLongpollTimeoutSeconds: 120,
		MaxEventBufferSize:        100,
		EventTimeToLiveSeconds:    forever,
	}); err != nil {
		t.Errorf("Unxpected error when calling StartLongpoll with valid options")
	} else {
		manager.Shutdown()
	}
	// Confirm zero TTL converts to forever
	if manager, err := StartLongpoll(Options{
		LoggingEnabled:            true,
		MaxLongpollTimeoutSeconds: 120,
		MaxEventBufferSize:        100,
		EventTimeToLiveSeconds:    0,
	}); err != nil {
		t.Errorf("Unxpected error when calling StartLongpoll with valid options")
	} else {
		if manager.subManager.EventTimeToLiveSeconds != forever {
			t.Errorf("Expected default of FOREVER when EventTimeToLiveSeconds is 0.  instead: %d",
				manager.subManager.EventTimeToLiveSeconds)
		}
		manager.Shutdown()
	}
	// Confirm defaults for options set to zero value
	// either explicitly like so:
	if manager, err := StartLongpoll(Options{
		LoggingEnabled:                 false,
		MaxLongpollTimeoutSeconds:      0,
		MaxEventBufferSize:             0,
		EventTimeToLiveSeconds:         0,
		DeleteEventAfterFirstRetrieval: false,
	}); err != nil {
		t.Errorf("Unxpected error when calling StartLongpoll with valid options")
	} else {
		if manager.subManager.EventTimeToLiveSeconds != forever {
			t.Errorf("Expected default of FOREVER when EventTimeToLiveSeconds is 0.  instead: %d",
				manager.subManager.EventTimeToLiveSeconds)
		}
		if manager.subManager.MaxLongpollTimeoutSeconds != 120 {
			t.Errorf("Expected default of 120 when MaxLongpollTimeoutSeconds is 0.  instead: %d",
				manager.subManager.MaxLongpollTimeoutSeconds)
		}
		if manager.subManager.MaxEventBufferSize != 250 {
			t.Errorf("Expected default of 250 when MaxEventBufferSize is 0.  instead: %d",
				manager.subManager.MaxEventBufferSize)
		}
		if manager.subManager.LoggingEnabled != false {
			t.Errorf("Expected default of false when LoggingEnabled is left out.  instead: %t",
				manager.subManager.LoggingEnabled)
		}
		if manager.subManager.DeleteEventAfterFirstRetrieval != false {
			t.Errorf("Expected default of false when DeleteEventAfterFirstRetrieval is left out.  instead: %t",
				manager.subManager.DeleteEventAfterFirstRetrieval)
		}
		manager.Shutdown()
	}

	// or implicitly by never defining them:
	if manager, err := StartLongpoll(Options{}); err != nil {
		t.Errorf("Unxpected error when calling StartLongpoll with valid options")
	} else {
		if manager.subManager.EventTimeToLiveSeconds != forever {
			t.Errorf("Expected default of FOREVER when EventTimeToLiveSeconds is 0.  instead: %d",
				manager.subManager.EventTimeToLiveSeconds)
		}
		if manager.subManager.MaxLongpollTimeoutSeconds != 120 {
			t.Errorf("Expected default of 120 when MaxLongpollTimeoutSeconds is 0.  instead: %d",
				manager.subManager.MaxLongpollTimeoutSeconds)
		}
		if manager.subManager.MaxEventBufferSize != 250 {
			t.Errorf("Expected default of 250 when MaxEventBufferSize is 0.  instead: %d",
				manager.subManager.MaxEventBufferSize)
		}
		if manager.subManager.LoggingEnabled != false {
			t.Errorf("Expected default of false when LoggingEnabled is left out.  instead: %t",
				manager.subManager.LoggingEnabled)
		}
		if manager.subManager.DeleteEventAfterFirstRetrieval != false {
			t.Errorf("Expected default of false when DeleteEventAfterFirstRetrieval is left out.  instead: %t",
				manager.subManager.DeleteEventAfterFirstRetrieval)
		}
		manager.Shutdown()
	}
}

//gocyclo:ignore
func Test_LongpollManager_EventExpiration(t *testing.T) {
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:            true,
		MaxLongpollTimeoutSeconds: 120,
		MaxEventBufferSize:        100,
		EventTimeToLiveSeconds:    1,
	})
	sm := manager.subManager
	if len(sm.SubEventBuffer) != 0 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 0)
	}
	if sm.bufferPriorityQueue.Len() != 0 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 0)
	}
	manager.Publish("fruit", "apple")
	time.Sleep(10 * time.Millisecond)
	manager.Publish("veggie", "corn")
	time.Sleep(750 * time.Millisecond)
	manager.Publish("fruit", "orange")
	// Allow sub manager's goroutine time to pull from channel.
	// This sleep should cause us to yield and let the other goroutine run
	time.Sleep(50 * time.Millisecond)
	// Only ~800ms has went by, nothing should be expired out yet, so confirm
	// all data is there
	if len(sm.SubEventBuffer) != 2 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 2)
	}
	fruitBuffer, fruitFound := sm.SubEventBuffer["fruit"]
	veggieBuffer, veggiesFound := sm.SubEventBuffer["veggie"]
	if !fruitFound || !veggiesFound {
		t.Errorf("failed to find fruit and veggie category event buffers")
	}
	if fruitBuffer.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Unexpected number of fruit events.  was: %d, expected %d",
			fruitBuffer.eventBufferPtr.List.Len(), 2)
	}
	if veggieBuffer.eventBufferPtr.List.Len() != 1 {
		t.Errorf("Unexpected number of veggie events.  was: %d, expected %d",
			veggieBuffer.eventBufferPtr.List.Len(), 1)
	}
	if sm.bufferPriorityQueue.Len() != 2 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 2)
	}
	// Confirm top of heap is the veggie category since veggie is the category
	// with the oldest last-event (even tho fruit was started first, it has a
	// more recent event published on it--the heap sorts categories by how old
	// each categories most recent event is)
	if priority, peekErr := sm.bufferPriorityQueue.peekTopPriority(); peekErr != nil {
		t.Errorf("Unexpected error checking top priority: %v", peekErr)
	} else {
		if priority != veggieBuffer.eventBufferPtr.List.Front().Value.(*Event).Timestamp {
			t.Errorf("Expected priority to be: %d, was: %d", priority,
				veggieBuffer.eventBufferPtr.List.Front().Value.(*Event).Timestamp)
		}
	}
	// Now wait long enough for the first two published events to expire
	time.Sleep(220 * time.Millisecond)
	// NOTE how nothing was removed yet, because we do lazy-eval and only remove
	// expired stuff when activity occurs (but we do do a periodic check in
	// addition, but thats every 3 min by default.)
	if len(sm.SubEventBuffer) != 2 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 2)
	}
	fruitBuffer, fruitFound = sm.SubEventBuffer["fruit"]
	veggieBuffer, veggiesFound = sm.SubEventBuffer["veggie"]
	if !fruitFound || !veggiesFound {
		t.Errorf("failed to find fruit and veggie category event buffers")
	}
	if fruitBuffer.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Unexpected number of fruit events.  was: %d, expected %d",
			fruitBuffer.eventBufferPtr.List.Len(), 2)
	}
	if veggieBuffer.eventBufferPtr.List.Len() != 1 {
		t.Errorf("Unexpected number of veggie events.  was: %d, expected %d",
			veggieBuffer.eventBufferPtr.List.Len(), 1)
	}
	if sm.bufferPriorityQueue.Len() != 2 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 2)
	}
	if priority, peekErr := sm.bufferPriorityQueue.peekTopPriority(); peekErr != nil {
		t.Errorf("Unexpected error checking top priority: %v", peekErr)
	} else {
		if priority != veggieBuffer.eventBufferPtr.List.Front().Value.(*Event).Timestamp {
			t.Errorf("Expected priority to be: %d, was: %d", priority,
				veggieBuffer.eventBufferPtr.List.Front().Value.(*Event).Timestamp)
		}
	}
	// Force the fruit event to be expired out by introducing activity on the
	// fruit category.  (In this case, a new event)
	manager.Publish("fruit", "pear")
	// Allow sub manager's goroutine time to pull from channel.
	// This sleep should cause us to yield and let the other goroutine run
	time.Sleep(50 * time.Millisecond)
	if len(sm.SubEventBuffer) != 2 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 2)
	}
	fruitBuffer, fruitFound = sm.SubEventBuffer["fruit"]
	veggieBuffer, veggiesFound = sm.SubEventBuffer["veggie"]
	if !fruitFound || !veggiesFound {
		t.Errorf("failed to find fruit and veggie category event buffers")
	}
	// NOTE: fruit buffer has still only 2 events, not 3, because the one was
	// expired out
	if fruitBuffer.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Unexpected number of fruit events.  was: %d, expected %d",
			fruitBuffer.eventBufferPtr.List.Len(), 2)
	}
	if veggieBuffer.eventBufferPtr.List.Len() != 1 {
		t.Errorf("Unexpected number of veggie events.  was: %d, expected %d",
			veggieBuffer.eventBufferPtr.List.Len(), 1)
	}
	if sm.bufferPriorityQueue.Len() != 2 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 2)
	}
	// veggie buffer is still the oldest-newest-event category
	if priority, peekErr := sm.bufferPriorityQueue.peekTopPriority(); peekErr != nil {
		t.Errorf("Unexpected error checking top priority: %v", peekErr)
	} else {
		if priority != veggieBuffer.eventBufferPtr.List.Front().Value.(*Event).Timestamp {
			t.Errorf("Expected priority to be: %d, was: %d", priority,
				veggieBuffer.eventBufferPtr.List.Front().Value.(*Event).Timestamp)
		}
	}
	// Force the veggie event to be expired out by introducing activity on the
	// veggie category.  (In this case, a client requests a longpoll)
	// also confirm longpoll doesn't return the now-expired events, even if
	// client using a really old since param
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)
	req, _ := http.NewRequest("GET", "?timeout=2&category=veggie", nil)
	w := httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	approxTimeoutTime := timeToEpochMilliseconds(time.Now())
	var timeoutResp timeoutResponse
	if err := json.Unmarshal(w.Body.Bytes(), &timeoutResp); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if timeoutResp.TimeoutMessage != "no events before timeout" {
		t.Errorf("Unexpected timeout message: %q", timeoutResp.TimeoutMessage)
	}
	if timeoutResp.Timestamp < (approxTimeoutTime-100) ||
		timeoutResp.Timestamp > approxTimeoutTime {
		t.Errorf("Unexpected timeout timestamp.  Expected: %q, got: %q",
			approxTimeoutTime, timeoutResp.Timestamp)
	}
	// Now confirm veggie category removed--only one category: fruit
	// and in that category, only 2 events left
	if len(sm.SubEventBuffer) != 1 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 1)
	}
	fruitBuffer, fruitFound = sm.SubEventBuffer["fruit"]
	veggieBuffer, veggiesFound = sm.SubEventBuffer["veggie"]

	if !fruitFound || veggiesFound {
		t.Errorf("Fruit should be found, veggies should not.")
	}
	// NOTE: fruit buffer has still has last two published fruit events.
	if fruitBuffer.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Unexpected number of fruit events.  was: %d, expected %d",
			fruitBuffer.eventBufferPtr.List.Len(), 2)
	}
	if sm.bufferPriorityQueue.Len() != 1 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 1)
	}
	// fruit buffer is now the oldest most-recent-event-time buffer (and the only one)
	if priority, peekErr := sm.bufferPriorityQueue.peekTopPriority(); peekErr != nil {
		t.Errorf("Unexpected error checking top priority: %v", peekErr)
	} else {
		if priority != fruitBuffer.eventBufferPtr.List.Front().Value.(*Event).Timestamp {
			t.Errorf("Expected priority to be: %d, was: %d", priority,
				fruitBuffer.eventBufferPtr.List.Front().Value.(*Event).Timestamp)
		}
	}
	// Now force the expire check on the last two fruit events.
	// Enough time has elapsed that everything should be gone by now.
	req, _ = http.NewRequest("GET", "?timeout=1&category=fruit", nil)
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	approxTimeoutTime = timeToEpochMilliseconds(time.Now())
	if err := json.Unmarshal(w.Body.Bytes(), &timeoutResp); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if timeoutResp.TimeoutMessage != "no events before timeout" {
		t.Errorf("Unexpected timeout message: %q", timeoutResp.TimeoutMessage)
	}
	if timeoutResp.Timestamp < (approxTimeoutTime-100) ||
		timeoutResp.Timestamp > approxTimeoutTime {
		t.Errorf("Unexpected timeout timestamp.  Expected: %q, got: %q",
			approxTimeoutTime, timeoutResp.Timestamp)
	}
	// Now confirm veggie category removed--only one category: fruit
	// and in that category, only 2 events left
	if len(sm.SubEventBuffer) != 0 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 0)
	}
	_, fruitFound = sm.SubEventBuffer["fruit"]
	_, veggiesFound = sm.SubEventBuffer["veggie"]

	if fruitFound || veggiesFound {
		t.Errorf("Both fruit and veggie buffers should no longer exist in map.")
	}
	if sm.bufferPriorityQueue.Len() != 0 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 0)
	}
	// fruit buffer is now the oldest most-recent-event-time buffer (and the only one)
	if priority, peekErr := sm.bufferPriorityQueue.peekTopPriority(); peekErr == nil {
		t.Errorf("Expected error when peeking at top of empty queue.")
	} else {
		if priority != -1 {
			t.Errorf("Expected priority to be the -1 error value, instead it was: %d", priority)
		}
	}
	manager.Shutdown()
}

// Shared by multiple tests with manager configured different ways:
//gocyclo:ignore
func deleteOnFetchTest(manager *LongpollManager, t *testing.T) {
	sm := manager.subManager
	if len(sm.SubEventBuffer) != 0 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 0)
	}
	if sm.EventTimeToLiveSeconds != forever && sm.bufferPriorityQueue.Len() != 0 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 0)
	}
	manager.Publish("fruit", "apple")
	time.Sleep(10 * time.Millisecond)
	manager.Publish("veggie", "corn")
	time.Sleep(1150 * time.Millisecond)
	manager.Publish("fruit", "orange")
	time.Sleep(10 * time.Millisecond)
	manager.Publish("veggie", "carrot")
	// small wait so yield occurs and other goroutine gets a chance
	time.Sleep(50 * time.Millisecond)
	// Confirm expected state:
	if len(sm.SubEventBuffer) != 2 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 2)
	}
	fruitBuffer, fruitFound := sm.SubEventBuffer["fruit"]
	veggieBuffer, veggiesFound := sm.SubEventBuffer["veggie"]
	if !fruitFound || !veggiesFound {
		t.Errorf("failed to find fruit and veggie category event buffers")
	}
	if fruitBuffer.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Unexpected number of fruit events.  was: %d, expected %d",
			fruitBuffer.eventBufferPtr.List.Len(), 2)
	}
	if veggieBuffer.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Unexpected number of veggie events.  was: %d, expected %d",
			veggieBuffer.eventBufferPtr.List.Len(), 2)
	}
	if sm.EventTimeToLiveSeconds != forever && sm.bufferPriorityQueue.Len() != 2 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 2)
	}
	// Confirm top of heap is the fruit category since fruit is the category
	// with the oldest last-event
	var priorityBeforeRemoval int64
	// Only check heap if we're using it.  When no TTL, heap is not used.
	if sm.EventTimeToLiveSeconds != forever {
		if priority, peekErr := sm.bufferPriorityQueue.peekTopPriority(); peekErr != nil {
			t.Errorf("Unexpected error checking top priority: %v", peekErr)
		} else {
			if priority != fruitBuffer.eventBufferPtr.List.Front().Value.(*Event).Timestamp {
				t.Errorf("Expected priority to be: %d, was: %d", priority,
					fruitBuffer.eventBufferPtr.List.Front().Value.(*Event).Timestamp)
			}
			priorityBeforeRemoval = priority
		}
	}

	// Now let's do a longpoll on the fruit category asking for events less
	// than 1s old, confirm the most recent fruit (orange) was removed
	sinceTime := timeToEpochMilliseconds(time.Now().Add(-1 * time.Second))
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)
	req, _ := http.NewRequest("GET", fmt.Sprintf("?timeout=1&category=fruit&since_time=%d", sinceTime), nil)
	w := httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	var eventResponse eventResponse
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(eventResponse.Events) != 1 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 1, len(eventResponse.Events))
	}
	if (eventResponse.Events)[0].Category != "fruit" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "fruit", (eventResponse.Events)[0].Category)
	}
	if (eventResponse.Events)[0].Data != "orange" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "orange", (eventResponse.Events)[0].Data)
	}
	// Also confirm that orange is now gone out of the buffer
	if len(sm.SubEventBuffer) != 2 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 2)
	}
	fruitBuffer, fruitFound = sm.SubEventBuffer["fruit"]
	veggieBuffer, veggiesFound = sm.SubEventBuffer["veggie"]
	if !fruitFound || !veggiesFound {
		t.Errorf("failed to find fruit and veggie category event buffers")
	}
	if fruitBuffer.eventBufferPtr.List.Len() != 1 {
		t.Errorf("Unexpected number of fruit events.  was: %d, expected %d",
			fruitBuffer.eventBufferPtr.List.Len(), 1)
	}
	if fruitBuffer.eventBufferPtr.List.Front().Value.(*Event).Data.(string) != "apple" {
		t.Errorf("Unexpected event left in fruit buffer.  was: %s, expected: %s.",
			fruitBuffer.eventBufferPtr.List.Front().Value.(*Event).Data.(string), "apple")
	}
	if veggieBuffer.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Unexpected number of veggie events.  was: %d, expected %d",
			veggieBuffer.eventBufferPtr.List.Len(), 2)
	}
	if sm.EventTimeToLiveSeconds != forever && sm.bufferPriorityQueue.Len() != 2 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 2)
	}
	if sm.EventTimeToLiveSeconds != forever {
		// NOTE: the heap priority doesn't change when an event is removed due
		// to the DeleteEventAfterFirstRetrieval setting.  This is by design because
		// it is complicated to know what to update the priority to, and it doesn't
		// harm or break the other behavior to skip updating it, the worst that
		// happens is a frivolous expiration check that removes nothing.
		if priority, peekErr := sm.bufferPriorityQueue.peekTopPriority(); peekErr != nil {
			t.Errorf("Unexpected error checking top priority: %v", peekErr)
		} else {
			if priority != priorityBeforeRemoval {
				t.Errorf("Expected priority to be: %d, was: %d", priority,
					priorityBeforeRemoval)
			}
		}
	}

	// Now request all veggie events (since beginning of time), confirm all
	// veggies removed
	sinceTime = timeToEpochMilliseconds(time.Now().Add(-60 * time.Second))
	subscriptionHandler = ajaxHandler(manager.SubscriptionHandler)
	req, _ = http.NewRequest("GET", fmt.Sprintf("?timeout=1&category=veggie&since_time=%d", sinceTime), nil)
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(eventResponse.Events) != 2 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 2, len(eventResponse.Events))
	}
	if (eventResponse.Events)[0].Category != "veggie" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggie", (eventResponse.Events)[0].Category)
	}
	if (eventResponse.Events)[0].Data != "corn" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "corn", (eventResponse.Events)[0].Data)
	}
	if (eventResponse.Events)[1].Category != "veggie" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggie", (eventResponse.Events)[0].Category)
	}
	if (eventResponse.Events)[1].Data != "carrot" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "carrot", (eventResponse.Events)[0].Data)
	}
	time.Sleep(50 * time.Millisecond)
	if len(sm.SubEventBuffer) != 1 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 1)
	}
	fruitBuffer, fruitFound = sm.SubEventBuffer["fruit"]
	veggieBuffer, veggiesFound = sm.SubEventBuffer["veggie"]
	if !fruitFound || veggiesFound {
		t.Errorf("expected fruit to be found but not veggies")
	}

	if fruitBuffer.eventBufferPtr.List.Len() != 1 {
		t.Errorf("Unexpected number of fruit events.  was: %d, expected %d",
			fruitBuffer.eventBufferPtr.List.Len(), 1)
	}
	if fruitBuffer.eventBufferPtr.List.Front().Value.(*Event).Data.(string) != "apple" {
		t.Errorf("Unexpected event left in fruit buffer.  was: %s, expected: %s.",
			fruitBuffer.eventBufferPtr.List.Front().Value.(*Event).Data.(string), "apple")
	}
	if sm.EventTimeToLiveSeconds != forever {
		// Heap still not changed for same reasons as before
		if priority, peekErr := sm.bufferPriorityQueue.peekTopPriority(); peekErr != nil {
			t.Errorf("Unexpected error checking top priority: %v", peekErr)
		} else {
			if priority != priorityBeforeRemoval {
				t.Errorf("Expected priority to be: %d, was: %d", priority,
					priorityBeforeRemoval)
			}
		}
	}
}

func Test_LongpollManager_DeleteOnFetch(t *testing.T) {
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:                 true,
		MaxLongpollTimeoutSeconds:      120,
		MaxEventBufferSize:             100,
		EventTimeToLiveSeconds:         60,
		DeleteEventAfterFirstRetrieval: true,
	})
	deleteOnFetchTest(manager, t)
}

func Test_LongpollManager_DeleteOnFetch_ForeverTTL(t *testing.T) {
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:                 true,
		MaxLongpollTimeoutSeconds:      120,
		MaxEventBufferSize:             100,
		EventTimeToLiveSeconds:         forever,
		DeleteEventAfterFirstRetrieval: true,
	})
	deleteOnFetchTest(manager, t)
}

func Test_LongpollManager_DeleteOnFetch_SkipBuffering(t *testing.T) {
	// Publish an event while a client is in the middle of a longpoll and
	// confirm that the event was received by the client and never buffered
	// on the server.
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:                 true,
		MaxLongpollTimeoutSeconds:      120,
		MaxEventBufferSize:             100,
		EventTimeToLiveSeconds:         60 * 10,
		DeleteEventAfterFirstRetrieval: true,
	})
	sm := manager.subManager
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)

	if len(sm.SubEventBuffer) != 0 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 0)
	}
	if sm.EventTimeToLiveSeconds != forever && sm.bufferPriorityQueue.Len() != 0 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 0)
	}

	// Valid request, but we don't have any events published,
	// so this will wait until a publish or a timeout, in this case we'll get
	// an event.
	req, _ := http.NewRequest("GET", "?timeout=30&category=fruit", nil)
	w := httptest.NewRecorder()
	// Publish two events, only the second is for our subscription category
	// Note how these events occur after the client subscribed
	// if they occurred before, since we don't provide a since_time url param
	// we'd default to now and skip those events.
	go func() {
		time.Sleep(1500 * time.Millisecond)
		manager.Publish("fruit", "peach")
	}()
	subscriptionHandler.ServeHTTP(w, req)
	// Confirm we got the correct event
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	var eventResponse eventResponse
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(eventResponse.Events) != 1 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 1, len(eventResponse.Events))
	}
	if (eventResponse.Events)[0].Category != "fruit" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "fruit", (eventResponse.Events)[0].Category)
	}
	if (eventResponse.Events)[0].Data != "peach" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "peach", (eventResponse.Events)[0].Data)
	}
	// Ensure nothing was buffered:
	if len(sm.SubEventBuffer) != 0 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 0)
	}
	if sm.EventTimeToLiveSeconds != forever && sm.bufferPriorityQueue.Len() != 0 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 0)
	}
	// Don't forget to kill our pubsub manager's run goroutine
	manager.Shutdown()
}

func Test_LongpollManager_PurgingOldCategories(t *testing.T) {
	// Confirm that old categories get removed by purge.
	// After any activity we check to see if it's time to call purge.
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:            true,
		MaxLongpollTimeoutSeconds: 120,
		MaxEventBufferSize:        100,
		EventTimeToLiveSeconds:    1,
	})
	sm := manager.subManager
	sm.staleCategoryPurgePeriodSeconds = 2
	if len(sm.SubEventBuffer) != 0 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 0)
	}
	if sm.bufferPriorityQueue.Len() != 0 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 0)
	}
	manager.Publish("fruit", "apple")
	time.Sleep(10 * time.Millisecond)
	manager.Publish("fruit", "orange")
	time.Sleep(2000 * time.Millisecond)
	// It's now been over 2s since both fruit events published, and since our
	// TTL setting is 1s, these are both expired.
	// But confirm purge didn't happen yet even tho 2s (the purge period) has
	// elapsed.
	if len(sm.SubEventBuffer) != 1 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 1)
	}
	fruitBuffer, fruitFound := sm.SubEventBuffer["fruit"]
	veggieBuffer, veggiesFound := sm.SubEventBuffer["veggie"]
	if !fruitFound || veggiesFound {
		t.Errorf("should find fruit but not veggie buffer")
	}
	if fruitBuffer.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Unexpected number of fruit events.  was: %d, expected %d",
			fruitBuffer.eventBufferPtr.List.Len(), 2)
	}
	if sm.bufferPriorityQueue.Len() != 1 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 1)
	}

	// Publish an event which will force us to check if elapsed time is greater
	// than purge period and kick off a purge.
	manager.Publish("veggie", "corn")
	time.Sleep(50 * time.Millisecond)

	// Confirm fruit events were destroyed
	if len(sm.SubEventBuffer) != 1 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 1)
	}
	fruitBuffer, fruitFound = sm.SubEventBuffer["fruit"]
	veggieBuffer, veggiesFound = sm.SubEventBuffer["veggie"]
	if fruitFound || !veggiesFound {
		t.Errorf("Expected to find veggies but not fruit buffer.")
	}
	if veggieBuffer.eventBufferPtr.List.Len() != 1 {
		t.Errorf("Unexpected number of veggie events.  was: %d, expected %d",
			veggieBuffer.eventBufferPtr.List.Len(), 1)
	}
	if sm.bufferPriorityQueue.Len() != 1 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 1)
	}
}

func Test_LongpollManager_PurgingOldCategories_Inactivity(t *testing.T) {
	// Confirm that old categories get removed by purge even if there is no
	// activity going on.  This purge is accomplished via the periodic
	// check regardless of activity.
	manager, _ := StartLongpoll(Options{
		LoggingEnabled:            true,
		MaxLongpollTimeoutSeconds: 120,
		MaxEventBufferSize:        100,
		EventTimeToLiveSeconds:    1,
	})
	sm := manager.subManager
	sm.staleCategoryPurgePeriodSeconds = 2
	if len(sm.SubEventBuffer) != 0 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 0)
	}
	if sm.bufferPriorityQueue.Len() != 0 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 0)
	}
	manager.Publish("fruit", "apple")
	time.Sleep(10 * time.Millisecond)
	manager.Publish("fruit", "orange")
	time.Sleep(2000 * time.Millisecond)
	// It's now been over 2s since both fruit events published, and since our
	// TTL setting is 1s, these are both expired.
	// But confirm purge didn't happen yet even tho 2s (the purge period) has
	// elapsed.
	if len(sm.SubEventBuffer) != 1 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 1)
	}
	fruitBuffer, fruitFound := sm.SubEventBuffer["fruit"]
	if !fruitFound {
		t.Errorf("should find fruit but not veggie buffer")
	}
	if fruitBuffer.eventBufferPtr.List.Len() != 2 {
		t.Errorf("Unexpected number of fruit events.  was: %d, expected %d",
			fruitBuffer.eventBufferPtr.List.Len(), 2)
	}
	if sm.bufferPriorityQueue.Len() != 1 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 1)
	}
	// Wait until we do our purge check due to inactivity, then confirm
	// data was cleared out.
	time.Sleep(3100 * time.Millisecond)

	// Confirm fruit events were destroyed
	if len(sm.SubEventBuffer) != 0 {
		t.Errorf("Unexpected category-to-buffer map size.  was: %d, expected %d",
			len(sm.SubEventBuffer), 0)
	}
	fruitBuffer, fruitFound = sm.SubEventBuffer["fruit"]
	if fruitFound {
		t.Errorf("Expected to not find fruit buffer.")
	}
	if sm.bufferPriorityQueue.Len() != 0 {
		t.Errorf("Unexpected heap size.  was: %d, expected: %d", sm.bufferPriorityQueue.Len(), 0)
	}
}

//gocyclo:ignore
// Tests the bug from issue #19 where clients see only the first of multiple events
// published within the same millisecond. The fix for this involves adding an event ID
// field and including the last seen id in the request to use in addition to since_time.
func Test_MultipleConsecutivePublishedEvents(t *testing.T) {
	manager, _ := StartLongpoll(Options{
		LoggingEnabled: true,
	})

	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)

	// Valid request, but we don't have any events published,
	// so this will wait for a publish or timeout (in this case we'll get
	// something)
	req, _ := http.NewRequest("GET", "?timeout=30&category=beer", nil)
	w := NewCloseNotifierRecorder()

	// Publish 3 events for our subscribed category all at once.
	// Note how these events occur after the client subscribed.
	// The old, buggy behavior was that the first published event would trigger
	// an immediate resposne to the client and then when the client
	// polled again for more events using the updated since_time param, the
	// other published events having the same timestamp as the first would be
	// skipped entirely. Now, including the last_id in the request will
	// allow clients to see events published at the same timestamp.
	// Also publish a 4th event after a delay and make sure we can get that data too.
	go func() {
		time.Sleep(1500 * time.Millisecond)
		manager.Publish("beer", "High Life")
		manager.Publish("beer", "Lionshead")
		manager.Publish("beer", "Miller Genuine Draft")
		time.Sleep(1500 * time.Millisecond)
		manager.Publish("beer", "Yuengling")
	}()
	subscriptionHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	var eventResponse eventResponse
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}

	if len(eventResponse.Events) != 1 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 1, len(eventResponse.Events))
	}

	firstEvent := (eventResponse.Events)[0]
	if firstEvent.Category != "beer" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "beer", firstEvent.Category)
	}
	if firstEvent.Data != "High Life" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "High Life", firstEvent.Data)
	}

	if len(firstEvent.ID) == 0 {
		t.Fatalf("Event ID was empty.")
	}

	// Now ask for any events using since_time and last_id and confirm we get the other 2/3 events publisehd at the saem time
	req, _ = http.NewRequest("GET", fmt.Sprintf("?timeout=2&category=beer&since_time=%d&last_id=%v", firstEvent.Timestamp, firstEvent.ID), nil)
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(eventResponse.Events) != 2 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 2, len(eventResponse.Events))
	}

	secondEvent := (eventResponse.Events)[0]
	thirdEvent := (eventResponse.Events)[1]

	if secondEvent.Category != "beer" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "beer", secondEvent.Category)
	}
	if secondEvent.Data != "Lionshead" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "Lionshead", secondEvent.Data)
	}

	if thirdEvent.Category != "beer" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "beer", thirdEvent.Category)
	}
	if thirdEvent.Data != "Miller Genuine Draft" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "Miller Genuine Draft", thirdEvent.Data)
	}

	if secondEvent.Timestamp != firstEvent.Timestamp {
		t.Errorf("Expected timestamps to match. Expected: %q, Actual: %q", firstEvent.Timestamp, secondEvent.Timestamp)
	}
	if thirdEvent.Timestamp != firstEvent.Timestamp {
		t.Errorf("Expected timestamps to match. Expected: %q, Actual: %q", firstEvent.Timestamp, thirdEvent.Timestamp)
	}

	// wait long enough for 4th event to occur and then confirm we get only that when calling with updated since_time and last_id
	time.Sleep(1500 * time.Millisecond)

	req, _ = http.NewRequest("GET", fmt.Sprintf("?timeout=2&category=beer&since_time=%d&last_id=%v", thirdEvent.Timestamp, thirdEvent.ID), nil)
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(eventResponse.Events) != 1 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 1, len(eventResponse.Events))
	}

	fourthEvent := (eventResponse.Events)[0]

	if fourthEvent.Category != "beer" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "beer", fourthEvent.Category)
	}
	if fourthEvent.Data != "Yuengling" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "Lionshead", fourthEvent.Data)
	}

	// Don't forget to kill our pubsub manager's run goroutine
	manager.Shutdown()
}

// Tests using the FilePersistorAddOn with LongpollManager.
func Test_WithFilePersistorAddOn(t *testing.T) {
	filename := "./glp-unit-tests-events.data"
	// NOTE: the flush time of 10 seconds means we're relying on
	// the OnShutdown() hook to flush data. This proves that OnShutdown
	// gets called and a flush occurs.
	filePersistor, err := NewFilePersistor(filename, 4096, 10)
	if err != nil {
		fmt.Printf("Failed to create file persistor, error: %v", err)
		return
	}
	defer os.Remove(filename)

	// Create manager with file persistor addon and publish data.
	manager, err := StartLongpoll(Options{
		AddOn: filePersistor,
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %q", err)
	}

	manager.Publish("food", "eggs")
	manager.Publish("food", "waffles")
	time.Sleep(10 * time.Millisecond)
	manager.Shutdown()
	time.Sleep(10 * time.Millisecond)

	// Now start a new manager using a file persistor with same underlying
	// filename. We should be able to see persisted events from previous run.
	// This means that:
	// a) OnPublish hook saw the events,
	// b) the data was flushed via OnShutdown, and
	// c) events are read back in from file via OnLongpollStart hook.
	filePersistor, err = NewFilePersistor(filename, 4096, 10)
	if err != nil {
		fmt.Printf("Failed to create file persistor, error: %v", err)
		return
	}

	manager, err = StartLongpoll(Options{
		AddOn: filePersistor,
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %q", err)
	}

	sinceTime := timeToEpochMilliseconds(time.Now().Add(-60 * time.Second))
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)
	req, _ := http.NewRequest("GET", fmt.Sprintf("?timeout=6&category=food&since_time=%d", sinceTime), nil)

	w := NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)

	// Confirm we got the correct event
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	var eventResponse eventResponse
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(eventResponse.Events) != 2 {
		t.Fatalf("Unexpected number of events.  Expected: %d, got: %d", 2, len(eventResponse.Events))
	}
	if (eventResponse.Events)[0].Category != "food" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "food", (eventResponse.Events)[0].Category)
	}
	if (eventResponse.Events)[0].Data != "eggs" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "eggs", (eventResponse.Events)[0].Data)
	}
	if (eventResponse.Events)[1].Category != "food" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "food", (eventResponse.Events)[1].Category)
	}
	if (eventResponse.Events)[1].Data != "waffles" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "waffles", (eventResponse.Events)[1].Data)
	}

}
