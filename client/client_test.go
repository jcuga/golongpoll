package client

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jcuga/golongpoll"
)

func testEventsManager() *golongpoll.LongpollManager {
	eventsManager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled: true,
	})
	if err != nil {
		fmt.Println("Failed to create manager: ", err)
	}

	return eventsManager
}

func testServer() (url.URL, *golongpoll.LongpollManager) {
	eventsManager := testEventsManager()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", eventsManager.SubscriptionHandler)
	mux.HandleFunc("/publish", eventsManager.PublishHandler)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("Failed to listen on address, error: %v", err))
	}

	server := &http.Server{Handler: mux}

	fmt.Println("Tests event server listening on", listener.Addr().String())

	go func() {
		panic(server.Serve(listener))
	}()

	u, _ := url.Parse("http://" + listener.Addr().String() + "/events")

	return *u, eventsManager
}

func testAuthServer(username, password string) (url.URL, *golongpoll.LongpollManager) {
	eventsManager := testEventsManager()

	var hf http.HandlerFunc = func(w http.ResponseWriter, req *http.Request) {
		reqUsername, reqPassword, _ := req.BasicAuth()
		if reqUsername == username && reqPassword == password {
			eventsManager.SubscriptionHandler(w, req)
		} else {
			w.WriteHeader(401)
		}
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	fmt.Println("Tests event server listening on", listener.Addr().String())

	go func() {
		panic(http.Serve(listener, hf))
	}()

	u, _ := url.Parse("http://" + listener.Addr().String() + "/events")

	return *u, eventsManager
}

func TestClient_Events(t *testing.T) {
	category := "testing"

	u, manager := testServer()

	// Have a small timeout for tests
	opts := ClientOptions{
		SubscribeUrl:         u,
		Category:             category,
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
	}
	c, err := NewClient(opts)

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	events := c.Start(time.Now())

	expectedResults := []string{"test1", "test2", "test3", "test4", "test5", "test6"}

	// NOTE: publishing multiple events consecutively without delay will trigger
	// the conditions for issue #19 where we weren't seeing any event after
	// the first one when they share the same publish timestamp milliseconds.
	// The loop below publishes 4 in a row without delay.
	for num, result := range expectedResults {
		manager.Publish(category, result)

		if num == 3 {
			// Force client to encounter a poll timeout response.
			// Since client requests a timeout of 1 second, delaying 2 here
			// will trigger the timeout response from server.
			// There should be no adverse affect on the client and we should
			// still get the expected events in our channel.
			time.Sleep(time.Duration(2) * time.Second)
		}
	}

	for i := 0; i < len(expectedResults); i++ {
		select {
		case e, ok := <-events:
			if !ok {
				c.Stop()
				t.Fatal("Unexpected channel close.")
			}
			data, ok := e.Data.(string)
			if !ok {
				t.Errorf("Expected data to be a sring, got: %T", e.Data)
			} else if data != expectedResults[i] {
				t.Errorf("Unexpected data value, expected: %v, got: %v", expectedResults[i], data)
			}

		case <-time.After(3 * time.Second):
			t.Error("Should have seen events.")
		}
	}

	// We'll stop the client, and make sure it doesn't handle any events anymore
	c.Stop()
	manager.Publish(category, "testNope")

	select {
	case e, ok := <-events:
		if ok {
			t.Error("Expected channel close")
		}

		if e != nil {
			t.Errorf("Expected nil Event, got: %v", e)
		}

	case <-time.After(3 * time.Second):
		t.Error("Should have seen a channel close.")
	}
}

// Test getting events before client start by providing a pollSince in the past.
func TestClient_PastEvents(t *testing.T) {
	category := "testing"

	u, manager := testServer()

	// Have a small timeout for tests
	opts := ClientOptions{
		SubscribeUrl:         u,
		Category:             category,
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
	}

	// Publish events before client starts
	expectedResults := []string{"test1", "test2", "test3"}
	for _, result := range expectedResults {
		manager.Publish(category, result)
		time.Sleep(time.Duration(1) * time.Second)
	}

	time.Sleep(time.Duration(1) * time.Second)

	c, err := NewClient(opts)

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	// Starting after events published, but asking for events old enough to
	// include those events.
	events := c.Start(time.Now().Add(-5 * time.Second))

	for i := 0; i < len(expectedResults); i++ {
		select {
		case e, ok := <-events:
			if !ok {
				c.Stop()
				t.Fatal("Unexpected channel close.")
			}
			data, ok := e.Data.(string)
			if !ok {
				t.Errorf("Expected data to be a sring, got: %T", e.Data)
			} else if data != expectedResults[i] {
				t.Errorf("Unexpected data value, expected: %v, got: %v", expectedResults[i], data)
			}

		case <-time.After(3 * time.Second):
			t.Error("Should have seen events.")
		}
	}

	c.Stop()
}

func TestClient_HttpBasicAuthentication(t *testing.T) {
	category := "testing"
	testUser := "bob"
	testPassword := "bob"

	u, manager := testAuthServer(testUser, testPassword)

	opts := ClientOptions{
		SubscribeUrl:         u,
		Category:             category,
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
		BasicAuthUsername:    testUser,
		BasicAuthPassword:    testPassword,
	}
	c, err := NewClient(opts)

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	events := c.Start(time.Now())

	manager.Publish(category, "test")

	select {
	case e, ok := <-events:
		if !ok {
			c.Stop()
			t.Fatal("Unexpected channel close.")
		}
		data, ok := e.Data.(string)
		if !ok {
			t.Errorf("Expected data to be a sring, got: %T", e.Data)
		} else if data != "test" {
			t.Errorf("Unexpected data value, expected: %v, got: %v", "test", data)
		}
	case <-time.After(3 * time.Second):
		t.Error("Error selecting from the events channel")
	}

	c.Stop()

	// We have to make a new client, we can't change and re-start the old one.
	opts = ClientOptions{
		SubscribeUrl:         u,
		Category:             category,
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
		BasicAuthUsername:    testUser,
		BasicAuthPassword:    "notThePassword",
	}
	c, err = NewClient(opts)

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	events = c.Start(time.Now())
	defer c.Stop()

	manager.Publish(category, "test")

	select {
	case e, ok := <-events:
		t.Error("Received something in the events channel although the requests are unauthorized")

		if e != nil {
			t.Errorf("unexpected event: %v", e)
		}

		// No matter what getting here is an error, note if this was a channel close or not
		t.Errorf("Channel ok: %v", ok)

	case <-time.After(3 * time.Second):
		// worked as expected, the request was invalid
	}
}

// Ensure OnFailure gets called and works as intended.
// In this test, an invalid category is used.
// This will trigger an api level error where HTTP 200 response but
// the returned json will have an error message.
func TestClient_OnfailureInvalidParams(t *testing.T) {
	// category is invalid, as it is longer than 1024 (7*150==1050)
	category := strings.Repeat("testing", 150)
	u, _ := testServer()

	opts := ClientOptions{
		SubscribeUrl:         u,
		Category:             category,
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
		OnFailure:            getOnFailureTestHandler(),
	}
	c, err := NewClient(opts)

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	onFailureTestCase(t, c)
}

// Ensure OnFailure gets called and works as intended.
// In this test, the longpoll server is not running so the
// HTTP call itself will fail.
func TestClient_OnfailureServerDown(t *testing.T) {
	category := "testing"

	// NOTE: not creating testServer here.
	u, err := url.Parse("http://127.0.0.1:5974/nope")
	if err != nil {
		t.Fatalf("Error parsing test url: %v", err)
	}

	opts := ClientOptions{
		SubscribeUrl:         *u,
		Category:             category,
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
		OnFailure:            getOnFailureTestHandler(),
	}
	c, err := NewClient(opts)

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	onFailureTestCase(t, c)
}

// OnFailure triggerd by a non-HTTP 200 response (404)
func TestClient_OnfailureServer404(t *testing.T) {
	category := "testing"
	u, manager := testServer()

	// NOTE: making url of server plus a bogus path part
	extraPartUrl, err := url.Parse(u.String() + "NopeNotReal")
	if err != nil {
		t.Fatalf("Error parsing test url: %v", err)
	}

	opts := ClientOptions{
		SubscribeUrl:         *extraPartUrl,
		Category:             category,
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
		OnFailure:            getOnFailureTestHandler(),
	}
	c, err := NewClient(opts)

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	manager.Publish(category, "shouldNeverSeeThis")
	time.Sleep(100 * time.Millisecond)
	onFailureTestCase(t, c)
}

// Handler that tells client to stop retry after 3 tries
func getOnFailureTestHandler() func(error) bool {
	retries := 0
	return func(err error) bool {
		retries += 1
		if retries <= 3 {
			log.Printf("OnFailure - retry count: %d - keep trying\n", retries)
			return true
		}

		log.Printf("OnFailure - retry count: %d - stop trying\n", retries)
		return false
	}
}

func onFailureTestCase(t *testing.T, client *Client) {
	events := client.Start(time.Now().Add(-2 * time.Second))
	defer client.Stop()

	select {
	case e, ok := <-events:
		if ok {
			t.Errorf("Expected channel close.")
		}
		if e != nil {
			t.Errorf("Expected nil data, got: %v", e)
		}

	case <-time.After(15 * time.Second):
		// Since ReattemptWaitSeconds=1, having 3 retries should be 3s + time to
		// make bad requests 3x to localhost.
		// UPDATE: This is brittle! On windows, it actually takes a few seconds before it realizes the connection
		// fails, so the band-aid is to wait long enough.
		t.Error("Should have closed channel by now.")
	}
}

// Same as TestClient_PastEvents but with a wrapped subscription handler that
// will test the header values.
func TestClient_ExtraHeaders(t *testing.T) {
	category := "testing"
	u, manager := testHeadersServer(t)
	defer manager.Shutdown()

	pubUrl, _ := url.Parse(strings.Replace(u.String(), "events", "publish", 1))

	opts := ClientOptions{
		SubscribeUrl:         u,
		PublishUrl:           *pubUrl,
		Category:             category,
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
		ExtraHeaders:         []HeaderKeyValue{{Key: "howdy", Value: "doody"}, {Key: "hi", Value: "MOM"}},
	}
	c, err := NewClient(opts)

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	expectedResults := []string{"test1", "test2", "test3"}
	for _, result := range expectedResults {
		pubErr := c.Publish(category, result)
		if pubErr != nil {
			t.Errorf("Got unexpected error during publish: %v", err)
		}
		time.Sleep(time.Duration(100) * time.Millisecond)
	}

	events := c.Start(time.Now().Add(-5 * time.Second))

	for i := 0; i < len(expectedResults); i++ {
		select {
		case e, ok := <-events:
			if !ok {
				c.Stop()
				t.Fatal("Unexpected channel close.")
			}
			data, ok := e.Data.(string)
			if !ok {
				t.Errorf("Expected data to be a sring, got: %T", e.Data)
			} else if data != expectedResults[i] {
				t.Errorf("Unexpected data value, expected: %v, got: %v", expectedResults[i], data)
			}

		case <-time.After(3 * time.Second):
			t.Error("Should have seen events.")
		}
	}

	c.Stop()
}

func testHeadersServer(t *testing.T) (url.URL, *golongpoll.LongpollManager) {
	eventsManager := testEventsManager()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {

		val := r.Header.Get("howdy")
		if val != "doody" {
			t.Errorf("Unexpected/missing header value. Expected: 'dooty', got: %q", val)
		}

		val = r.Header.Get("hi")
		if val != "MOM" {
			t.Errorf("Unexpected/missing header value. Expected: 'MOM', got: %q", val)
		}

		eventsManager.SubscriptionHandler(w, r)
	})

	mux.HandleFunc("/publish", func(w http.ResponseWriter, r *http.Request) {

		val := r.Header.Get("howdy")
		if val != "doody" {
			t.Errorf("Unexpected/missing header value. Expected: 'dooty', got: %q", val)
		}

		val = r.Header.Get("hi")
		if val != "MOM" {
			t.Errorf("Unexpected/missing header value. Expected: 'MOM', got: %q", val)
		}

		eventsManager.PublishHandler(w, r)
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("Failed to listen on address, error: %v", err))
	}

	server := &http.Server{Handler: mux}

	fmt.Println("Tests event server listening on", listener.Addr().String())

	go func() {
		panic(server.Serve(listener))
	}()

	u, _ := url.Parse("http://" + listener.Addr().String() + "/events")

	return *u, eventsManager
}

func TestClient_NewClientInvalidOptions(t *testing.T) {
	subUrl, _ := url.Parse("http://127.0.0.1:8080/events")
	pubUrl, _ := url.Parse("http://127.0.0.1:8080/publish")

	// First up, need at least one of: sub/pub purl
	c, err := NewClient(ClientOptions{
		Category:             "asdf",
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
	})

	if err == nil {
		t.Errorf("Expected non-nil error for invalid NewClient options (missing sub/pub urls).")
	}
	if c != nil {
		t.Errorf("Expected nil client on error.")
	}

	// Need to supply a non-empty category
	c, err = NewClient(ClientOptions{
		SubscribeUrl:         *subUrl,
		PublishUrl:           *pubUrl,
		Category:             "",
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
	})

	if err == nil {
		t.Errorf("Expected non-nil error for invalid NewClient options (missing category).")
	}
	if c != nil {
		t.Errorf("Expected nil client on error.")
	}

	// if supplying basic auth, must provide both user and password
	c, err = NewClient(ClientOptions{
		SubscribeUrl:         *subUrl,
		PublishUrl:           *pubUrl,
		Category:             "testing",
		BasicAuthPassword:    "passwordButNoUsername",
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
	})

	if err == nil {
		t.Errorf("Expected non-nil error for invalid NewClient options (have BasicAuthPassword but missing BasicAuthUsername).")
	}
	if c != nil {
		t.Errorf("Expected nil client on error.")
	}

	c, err = NewClient(ClientOptions{
		SubscribeUrl:         *subUrl,
		PublishUrl:           *pubUrl,
		Category:             "testing",
		BasicAuthUsername:    "usernameButNoPassword",
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
	})

	if err == nil {
		t.Errorf("Expected non-nil error for invalid NewClient options (have BasicAuthUsername but missing BasicAuthPassword).")
	}
	if c != nil {
		t.Errorf("Expected nil client on error.")
	}

	// Calling start with a client that does not have opts.SubscribeUrl set will immediately return a closed channel and log error
	c, err = NewClient(ClientOptions{
		PublishUrl:           *pubUrl,
		Category:             "testing",
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
	})

	// Client should be created and is valid, but only for using Publish only
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	events := c.Start(time.Now())

	select {
	case _, ok := <-events:
		if ok {
			t.Errorf("Expected closed channel.")
		}

	case <-time.After(1 * time.Second):
		t.Error("Should have seen channel close.")
	}

	// Calling publish without opts.PublishUrl will return an error
	c, err = NewClient(ClientOptions{
		SubscribeUrl:         *subUrl,
		Category:             "testing",
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
	})

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	err = c.Publish("someCategory", "someData")

	if err == nil {
		t.Error("Expected non-nil error when calling Publish with opts.PublishUrl empty.")
	}
}

func TestClient_PublishInvalidOptions(t *testing.T) {
	pubUrl, _ := url.Parse("http://127.0.0.1:8080/notBeingServedHere")

	c, err := NewClient(ClientOptions{
		PublishUrl:           *pubUrl,
		Category:             "testing",
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
	})

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	err = c.Publish("", "someData")

	if err == nil || err.Error() != "category must be 1-1024 characters long." {
		t.Errorf("Unexpected err, got: %v", err)
	}

	err = c.Publish("someCategory", nil)

	if err == nil || err.Error() != "data must be non-nil." {
		t.Errorf("Unexpected err, got: %v", err)
	}
}

func TestClient_PublishNoServer(t *testing.T) {
	pubUrl, _ := url.Parse("http://127.0.0.1:7391/notBeingServedHere")

	c, err := NewClient(ClientOptions{
		PublishUrl:           *pubUrl,
		Category:             "testing",
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
	})

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	err = c.Publish("someCategory", "someData")
	if err == nil {
		t.Fatalf("Expected error when publishing to server that does not exist.")
	}
}

func TestClient_Publish(t *testing.T) {
	category := "testing"

	u, _ := testServer()
	pubUrl, _ := url.Parse(strings.Replace(u.String(), "events", "publish", 1))

	// Have a small timeout for tests
	opts := ClientOptions{
		SubscribeUrl:         u,
		PublishUrl:           *pubUrl,
		Category:             category,
		PollTimeoutSeconds:   1,
		ReattemptWaitSeconds: 1,
		LoggingEnabled:       true,
	}
	c, err := NewClient(opts)

	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	defer c.Stop()

	events := c.Start(time.Now())

	expectedResults := []string{"test1", "test2", "test3", "test4", "test5", "test6"}

	for num, result := range expectedResults {
		// Use client pubilsh which will hit the longpoll manager's publish hook via http
		pubErr := c.Publish(category, result)
		if pubErr != nil {
			t.Fatalf("Unexpected publish error: %v", pubErr)
		}

		if num == 3 {
			// Force client to encounter a poll timeout response.
			// Since client requests a timeout of 1 second, delaying 2 here
			// will trigger the timeout response from server.
			// There should be no adverse affect on the client and we should
			// still get the expected events in our channel.
			time.Sleep(time.Duration(2) * time.Second)
		}
	}

	for i := 0; i < len(expectedResults); i++ {
		select {
		case e, ok := <-events:
			if !ok {
				c.Stop()
				t.Fatal("Unexpected channel close.")
			}
			data, ok := e.Data.(string)
			if !ok {
				t.Errorf("Expected data to be a sring, got: %T", e.Data)
			} else if data != expectedResults[i] {
				t.Errorf("Unexpected data value, expected: %v, got: %v", expectedResults[i], data)
			}

		case <-time.After(3 * time.Second):
			t.Error("Should have seen events.")
		}
	}
}
