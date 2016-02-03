package golongpoll

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func Test_LongpollManager_CreateManager(t *testing.T) {
	manager, err := CreateManager()
	// Confirm the create call worked, and our manager has the expected values
	if err != nil {
		t.Errorf("Failed to create default LongpollManager.  Error was: %q", err)
	}
	// Channel size defaults to 100
	if cap(manager.eventsIn) != 100 {
		t.Errorf("Unexpected event channel capacity.  Expected: %d, got: %d",
			100, cap(manager.eventsIn))
	}
	if cap(manager.subManager.clientSubscriptions) != 100 {
		t.Errorf("Unexpected client subscription channel capacity.  Expected: %d, got: %d",
			100, cap(manager.subManager.clientSubscriptions))
	}
	if cap(manager.subManager.ClientTimeouts) != 100 {
		t.Errorf("Unexpected client timeout channel capacity.  Expected: %d, got: %d",
			100, cap(manager.subManager.ClientTimeouts))
	}
	// Max event buffer size defaults to 250
	if manager.subManager.MaxEventBufferSize != 250 {
		t.Errorf("Unexpected client timeout channel capacity.  Expected: %d, got: %d",
			250, manager.subManager.MaxEventBufferSize)
	}
	// Don't forget to kill subscription manager's running goroutine
	manager.Shutdown()
}

func Test_LongpollManager_CreateCustomManager(t *testing.T) {
	manager, err := CreateCustomManager(360, 700, true)
	// Confirm the create call worked, and our manager has the expected values
	if err != nil {
		t.Errorf("Failed to create default LongpollManager.  Error was: %q", err)
	}
	// Channel size defaults to 100
	if cap(manager.eventsIn) != 100 {
		t.Errorf("Unexpected event channel capacity.  Expected: %d, got: %d",
			100, cap(manager.eventsIn))
	}
	if cap(manager.subManager.clientSubscriptions) != 100 {
		t.Errorf("Unexpected client subscription channel capacity.  Expected: %d, got: %d",
			100, cap(manager.subManager.clientSubscriptions))
	}
	if cap(manager.subManager.ClientTimeouts) != 100 {
		t.Errorf("Unexpected client timeout channel capacity.  Expected: %d, got: %d",
			100, cap(manager.subManager.ClientTimeouts))
	}
	// Max event buffer size set  to 700
	if manager.subManager.MaxEventBufferSize != 700 {
		t.Errorf("Unexpected client timeout channel capacity.  Expected: %d, got: %d",
			700, manager.subManager.MaxEventBufferSize)
	}
	// Don't forget to kill subscription manager's running goroutine
	manager.Shutdown()
}

func Test_LongpollManager_CreateCustomManager_InvalidArgs(t *testing.T) {
	manager, err := CreateCustomManager(360, 0, false) // buffer size == 0
	// Confirm create call failed
	if err == nil {
		t.Errorf("Expected error when creating custom manager with invalid event buffer size ")
	}
	if manager != nil {
		t.Errorf("Expected nil response for manager when create call returned error.")
	}
	manager, err = CreateCustomManager(360, -1, false) // buffer size == -1
	if err == nil {
		t.Errorf("Expected error when creating custom manager with invalid event buffer size ")
	}
	if manager != nil {
		t.Errorf("Expected nil response for manager when create call returned error.")
	}
	manager, err = CreateCustomManager(0, 200, false) // timeout == 0
	if err == nil {
		t.Errorf("Expected error when creating custom manager with invalid timeout ")
	}
	if manager != nil {
		t.Errorf("Expected nil response for manager when create call returned error.")
	}
	manager, err = CreateCustomManager(-1, 200, false) // timeout == -1
	if err == nil {
		t.Errorf("Expected error when creating custom manager with invalid timeout.")
	}
	if manager != nil {
		t.Errorf("Expected nil response for manager when create call returned error.")
	}
}

func Test_LongpollManager_Publish(t *testing.T) {
	manager, err := CreateManager()
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
	if buf.eventBuffer_ptr.MaxBufferSize != 250 {
		t.Errorf("Expected max buffer size of %d, but got %d.", 250, buf.eventBuffer_ptr.MaxBufferSize)
	}
	if buf.eventBuffer_ptr.List.Len() != 1 {
		t.Errorf("Expected buffer to be 1 item. instead: %d", buf.eventBuffer_ptr.List.Len())
	}
	if buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data != "apple" {
		t.Errorf("Expected event data to be %q, but got %q", "apple",
			buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data)
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
	if buf.eventBuffer_ptr.List.Len() != 2 {
		t.Errorf("Expected buffer to be 2 items. instead: %d", buf.eventBuffer_ptr.List.Len())
	}
	if buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data != "orange" {
		t.Errorf("Expected event data to be %q, but got %q", "orange",
			buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data)
	}
	buf, found = manager.subManager.SubEventBuffer["veggies"]
	if !found {
		t.Errorf("Failed to find event in sub manager's category-to-eventBuffer map")
	}
	if buf.eventBuffer_ptr.List.Len() != 1 {
		t.Errorf("Expected buffer to be 1 item. instead: %d", buf.eventBuffer_ptr.List.Len())
	}
	if buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data != "potato" {
		t.Errorf("Expected event data to be %q, but got %q", "potato",
			buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data)
	}
	// Don't forget to kill subscription manager's running goroutine
	manager.Shutdown()
}

func Test_LongpollManager_Publish_MaxBufferSize(t *testing.T) {
	manager, err := CreateCustomManager(120, 2, true) // max buffer size 3
	if len(manager.subManager.SubEventBuffer) != 0 {
		t.Errorf("Expected sub manager's event map to be initially empty. Instead len: %d",
			len(manager.subManager.SubEventBuffer))
	}
	err = manager.Publish("fruits", "apple")
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
	if buf.eventBuffer_ptr.MaxBufferSize != 2 {
		t.Errorf("Expected max buffer size of %d, but got %d.", 2, buf.eventBuffer_ptr.MaxBufferSize)
	}
	if buf.eventBuffer_ptr.List.Len() != 2 {
		t.Errorf("Expected buffer to be 2 items. instead: %d", buf.eventBuffer_ptr.List.Len())
	}
	if buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data != "banana" {
		t.Errorf("Expected event data to be %q, but got %q", "banana",
			buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data)
	}
	if buf.eventBuffer_ptr.Back().Value.(*lpEvent).Data != "apple" {
		t.Errorf("Expected event data to be %q, but got %q", "apple",
			buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data)
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
	if buf.eventBuffer_ptr.MaxBufferSize != 2 {
		t.Errorf("Expected max buffer size of %d, but got %d.", 2, buf.eventBuffer_ptr.MaxBufferSize)
	}
	if buf.eventBuffer_ptr.List.Len() != 2 {
		t.Errorf("Expected buffer to be 2 items. instead: %d", buf.eventBuffer_ptr.List.Len())
	}
	if buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data != "pear" {
		t.Errorf("Expected event data to be %q, but got %q", "banana",
			buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data)
	}
	if buf.eventBuffer_ptr.Back().Value.(*lpEvent).Data != "banana" {
		t.Errorf("Expected event data to be %q, but got %q", "apple",
			buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data)
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
	if buf.eventBuffer_ptr.List.Len() != 1 {
		t.Errorf("Expected buffer to be 1 item. instead: %d", buf.eventBuffer_ptr.List.Len())
	}
	if buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data != "potato" {
		t.Errorf("Expected event data to be %q, but got %q", "potato",
			buf.eventBuffer_ptr.Front().Value.(*lpEvent).Data)
	}
	// Don't forget to kill subscription manager's running goroutine
	manager.Shutdown()
}

func Test_LongpollManager_Publish_InvalidArgs(t *testing.T) {
	manager, err := CreateManager()
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
	manager, err := CreateManager()
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
	sub, err := newclientSubscription("colors", subTime)
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
}

func ajaxHandler(handlerFunc func(w http.ResponseWriter, r *http.Request)) http.Handler {
	return http.HandlerFunc(handlerFunc)
}

func Test_LongpollManager_WebClient_InvalidRequests(t *testing.T) {
	manager, _ := CreateCustomManager(120, 100, true)
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)

	// Empty request, this is going to result in an JSON error object:
	req, _ := http.NewRequest("GET", "", nil)
	w := httptest.NewRecorder()
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
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid timeout arg.  Must be 1-120.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Invalid timeout, too small
	req, _ = http.NewRequest("GET", "?timeout=0&category=veggies", nil)
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid timeout arg.  Must be 1-120.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Invalid timeout, too big
	req, _ = http.NewRequest("GET", "?timeout=121&category=veggies", nil)
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid timeout arg.  Must be 1-120.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Valid timeout, but missing category:
	req, _ = http.NewRequest("GET", "?timeout=30", nil)
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid subscription category, must be 1-1024 characters long.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Valid timeout, but category is too small
	req, _ = http.NewRequest("GET", "?timeout=30&category=", nil)
	w = httptest.NewRecorder()
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
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid subscription category, must be 1-1024 characters long.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Valid timeout, valid category, but invalid since_time
	req, _ = http.NewRequest("GET", "?timeout=30&category=foobar&since_time=asdf", nil)
	w = httptest.NewRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if w.Body.String() != "{\"error\": \"Invalid last_event_time arg.\"}" {
		t.Errorf("Unexpected response: %q", w.Body.String())
	}

	// Don't forget to kill our pubsub manager's run goroutine
	manager.Shutdown()
}

func Test_LongpollManager_WebClient_NoEventsSoTimeout(t *testing.T) {
	manager, _ := CreateCustomManager(120, 100, true)
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)

	// Valid request, but we don't have any events published,
	// so this will wait 2 seconds (because timeout param = 2)
	// and then come back wtih a timeout response
	req, _ := http.NewRequest("GET", "?timeout=2&category=veggies", nil)
	w := NewCloseNotifierRecorder()
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
	manager, _ := CreateCustomManager(120, 100, true)
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)
	if _, found := manager.subManager.ClientSubChannels["veggies"]; found {
		t.Errorf("Expected client sub channel not to exist yet ")
	}
	// This request has timeout of 5 seconds, but we're going to simulate a
	// disconnect in 2 seconds which is earlier.
	req, _ := http.NewRequest("GET", "?timeout=5&category=veggies", nil)
	w := NewCloseNotifierRecorder()
	go func() {
		time.Sleep(time.Duration(250) * time.Millisecond)
		// confirm subscription entry exists, with one client
		if _, found := manager.subManager.ClientSubChannels["veggies"]; !found {
			t.Errorf("Expected client sub channel to exist")
		}
		if val, _ := manager.subManager.ClientSubChannels["veggies"]; len(val) != 1 {
			t.Errorf("Expected sub channel to have one client subscribed")
		}
		time.Sleep(time.Duration(1) * time.Second)
		w.CloseNotifier <- true
		time.Sleep(time.Duration(1) * time.Second)
		// Confirm subscription entry exists, but has no clients anymore
		if _, found := manager.subManager.ClientSubChannels["veggies"]; !found {
			t.Errorf("Expected client sub channel to exist.")
		}
		if val, _ := manager.subManager.ClientSubChannels["veggies"]; len(val) != 0 {
			t.Errorf("Expected sub channel to have no more clients subscribed")
		}
	}()

	subscriptionHandler.ServeHTTP(w, req)
	// Don't forget to kill our pubsub manager's run goroutine
	manager.Shutdown()
}

func Test_LongpollManager_WebClient_Disconnect_TerminateHttp(t *testing.T) {
	manager, _ := CreateCustomManager(120, 100, true)
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
	w := NewCloseNotifierRecorder()

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
		time.Sleep(time.Duration(1) * time.Second)
		w.CloseNotifier <- true
		time.Sleep(time.Duration(2) * time.Second)
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

func Test_LongpollManager_WebClient_HasEvents(t *testing.T) {
	manager, _ := CreateCustomManager(120, 100, true)
	subscriptionHandler := ajaxHandler(manager.SubscriptionHandler)

	// Valid request, but we don't have any events published,
	// so this will wait 2 seconds (because timeout param = 2)
	// and then come back wtih a timeout response
	req, _ := http.NewRequest("GET", "?timeout=30&category=veggies", nil)
	w := NewCloseNotifierRecorder()
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
	if len(*eventResponse.Events) != 1 {
		t.Errorf("Unexpected number of events.  Expected: %d, got: %d", 1, len(*eventResponse.Events))
	}
	if (*eventResponse.Events)[0].Category != "veggies" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggies", (*eventResponse.Events)[0].Category)
	}
	if (*eventResponse.Events)[0].Data != "corn" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "corn", (*eventResponse.Events)[0].Data)
	}

	// Make a new subscription request.
	// Note how since there's no since_time url param, we default to now,
	// and thus don't see the previous event from our last http request
	req, _ = http.NewRequest("GET", "?timeout=2&category=veggies", nil)
	w = NewCloseNotifierRecorder()
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
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(*eventResponse.Events) != 1 {
		t.Errorf("Unexpected number of events.  Expected: %d, got: %d", 1, len(*eventResponse.Events))
	}
	if (*eventResponse.Events)[0].Category != "veggies" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggies", (*eventResponse.Events)[0].Category)
	}
	if (*eventResponse.Events)[0].Data != "corn" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "corn", (*eventResponse.Events)[0].Data)
	}

	firstEventTime := (*eventResponse.Events)[0].Timestamp
	manager.Publish("veggies", "carrot")
	time.Sleep(50 * time.Millisecond) // allow yield for goroutine channel reads

	// Now ask for any events since our first one, and confirm we get the second
	// 'veggie' category event
	req, _ = http.NewRequest("GET", fmt.Sprintf("?timeout=2&category=veggies&since_time=%d", firstEventTime), nil)
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(*eventResponse.Events) != 1 {
		t.Errorf("Unexpected number of events.  Expected: %d, got: %d", 1, len(*eventResponse.Events))
	}
	if (*eventResponse.Events)[0].Category != "veggies" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggies", (*eventResponse.Events)[0].Category)
	}
	if (*eventResponse.Events)[0].Data != "carrot" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "carrot", (*eventResponse.Events)[0].Data)
	}

	// Confirm we get both events when asking for any events since start of test run
	req, _ = http.NewRequest("GET", fmt.Sprintf("?timeout=2&category=veggies&since_time=%d",
		timeToEpochMilliseconds(startTime)), nil)
	w = NewCloseNotifierRecorder()
	subscriptionHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SubscriptionHandler didn't return %v", http.StatusOK)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &eventResponse); err != nil {
		t.Errorf("Failed to decode json: %q", err)
	}
	if len(*eventResponse.Events) != 2 {
		t.Errorf("Unexpected number of events.  Expected: %d, got: %d", 2, len(*eventResponse.Events))
	}
	if (*eventResponse.Events)[0].Data != "corn" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "corn", (*eventResponse.Events)[0].Data)
	}
	if (*eventResponse.Events)[1].Data != "carrot" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "carrot", (*eventResponse.Events)[0].Data)
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
	manager, _ := CreateCustomManager(120, 100, true)
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
	if len(*eventResponse.Events) != 2 {
		t.Errorf("Unexpected number of events.  Expected: %d, got: %d", 2, len(*eventResponse.Events))
	}
	if (*eventResponse.Events)[0].Category != "veggies" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggies", (*eventResponse.Events)[0].Category)
	}
	if (*eventResponse.Events)[0].Data != "broccoli" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "broccoli", (*eventResponse.Events)[0].Data)
	}
	if (*eventResponse.Events)[1].Category != "veggies" {
		t.Errorf("Unexpected category.  Expected: %q, got: %q", "veggies", (*eventResponse.Events)[1].Category)
	}
	if (*eventResponse.Events)[1].Data != "corn" {
		t.Errorf("Unexpected data.  Expected: %q, got: %q", "corn", (*eventResponse.Events)[1].Data)
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

// TODO: test cleanup of SubscriptionManager.ClientSubChannels when
// there are no more clients after a disconnect

// TODO: test cleanup of SubscriptionManager.ClientSubChannels when
// an event is sent to all clients and they are thus removed leaving zero in
// mapped value (which is itself [an empty] map)

// TODO: test the delete event on first fetch behavior

// TODO: test the empty buffer cleanup in all scenarios

// TODO: test the Event TTL cleanup after a subscription queue

// TODO: test the Event TTL cleanup after a event queue

// TODO: test the Event TTL cleanup after skipped event queueing due to delete on first opt

// TODO: test the Event TTL cleanup that happens regularly regardless of client/event activity

// TODO: any additional tests based on code coverage
