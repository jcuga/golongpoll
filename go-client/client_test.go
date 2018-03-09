package glpclient

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/jcuga/golongpoll"
)

func testEventsManager() *golongpoll.LongpollManager {
	var err error
	eventsManager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled: true,
	})
	if err != nil {
		fmt.Println("Failed to create manager: %q", err)
	}

	return eventsManager
}

func testServer() (*url.URL, *golongpoll.LongpollManager) {
	eventsManager := testEventsManager()

	var hf http.HandlerFunc = eventsManager.SubscriptionHandler

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	fmt.Println("Tests event server listening on", listener.Addr().String())

	go func() {
		panic(http.Serve(listener, hf))
	}()

	u, _ := url.Parse("http://" + listener.Addr().String() + "/events")

	return u, eventsManager
}

func testAuthServer(username, password string) (*url.URL, *golongpoll.LongpollManager) {
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

	return u, eventsManager
}

func TestClient(t *testing.T) {
	category := "testing"

	u, manager := testServer()

	c := NewClient(u, category)
	// Have a small timeout for tests
	c.Timeout = 1

	c.Start()

	manager.Publish(category, "test")

	select {
	case e := <-c.EventsChan:
		var data string
		err := json.Unmarshal(e.Data, &data)
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
		case e := <-c.EventsChan:
			var data string
			err := json.Unmarshal(e.Data, &data)
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

func TestClientAuthentication(t *testing.T) {
	category := "testing"
	testUser := "bob"
	testPassword := "bob"

	u, manager := testAuthServer(testUser, testPassword)

	c := NewClient(u, category)
	// Have a small timeout for tests
	c.Timeout = 1

	// Setup basic auth
	c.BasicAuthUsername = testUser
	c.BasicAuthPassword = testPassword

	c.Start()

	manager.Publish(category, "test")

	select {
	case e := <-c.EventsChan:
		var data string
		err := json.Unmarshal(e.Data, &data)
		if err != nil {
			t.Error("Error while decoding event from channel")
		}
		if data != "test" {
			t.Error("Wrong data coming out of the events channel")
		}

	case <-time.After(2 * time.Second):
		t.Error("Error selecting from the events channel")
	}

	// Now use an invalid password
	c.Stop()
	c.BasicAuthPassword = "password"
	c.Start()

	manager.Publish(category, "test")

	select {
	case e := <-c.EventsChan:
		t.Error("Received something in the events channel although the requests are unauthorized")
		spew.Dump(e)

	case <-time.After(2 * time.Second):
		// worked as expected, the request was invalid
	}
}
