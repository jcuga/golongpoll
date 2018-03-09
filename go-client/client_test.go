package glpclient

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jcuga/golongpoll"
)

func testServer() (*url.URL, *golongpoll.LongpollManager) {

	var err error
	eventsManager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled: true,
	})
	if err != nil {
		fmt.Println("Failed to create manager: %q", err)
	}

	http.HandleFunc("/events", eventsManager.SubscriptionHandler)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	fmt.Println("Tests event server listening on", listener.Addr().String())

	go func() {
		panic(http.Serve(listener, nil))
	}()

	u, _ := url.Parse("http://" + listener.Addr().String() + "/events")

	return u, eventsManager
}

func TestClient(t *testing.T) {
	category := "testing"

	u, manager := testServer()

	c := NewClient(*u, category)
	// Have a small timeout for tests
	c.Timeout = 1

	c.Start()

	manager.Publish(category, "test")

	select {
	case raw := <-c.EventsChan:
		var data string
		err := json.Unmarshal(raw, &data)
		if err != nil {
			t.Error("Error while decoding event from channel")
		}
		if data != "test" {
			t.Error("Wrong data coming out of the events channel")
		}

	case <-time.After(2 * time.Second):
		t.Error("Error selecting from the events channel")
	}

	// We'll stop the client, and make sure it doesn't handle any events anymore
	c.Stop()

	manager.Publish(category, "test2")

	select {
	case <-c.EventsChan:
		t.Error("Got something in the events chan although the client has been stopped")

	case <-time.After(2 * time.Second):
		// all good, we needed this to happen
	}

	// Now start it back and send a few events
	expectedResults := []string{"test1", "test2", "test3"}

	c.Start()

	for _, result := range expectedResults {
		manager.Publish(category, result)
	}

	for i := 0; i < len(expectedResults); i++ {
		select {
		case raw := <-c.EventsChan:
			var data string
			err := json.Unmarshal(raw, &data)
			if err != nil {
				t.Error("Error while decoding event from channel")
			}
			if data != expectedResults[i] {
				t.Error("Wrong data coming out of the events channel")
			}

		case <-time.After(2 * time.Second):
			t.Error("Error selecting from the events channel")
		}
	}
}
