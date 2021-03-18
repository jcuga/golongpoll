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
		Url:                  u,
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
			// Since client requests a timeout of 1 second, dealying 2 here
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
		Url:                  u,
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
		Url:                  u,
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
		Url:                  u,
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
		Url:                  u,
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
		Url:                  *u,
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
		Url:                  *extraPartUrl,
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

	case <-time.After(5 * time.Second):
		// Since ReattemptWaitSeconds=1, having 3 retries should be 3s + time to
		// make bad requests 3x to localhost.
		t.Error("Should have closed channel by now.")
	}
}
