// This is a more advanced example.
// Here you can see a wrapped LongpollManager.SubscriptionHandler that
// serves as a trivial example on how to add restrictions or additional
// behavior to a subscription handler.
// Also, we'll see an event payload that is not just a simple string and
// multiple longpolls coming from the same webpage.
//
//  Try it out by visiting 127.0.0.1:8081/advanced
//  You can open multiple browser windows at that same address and click
//  around and observe the events that you see coming in.
//
// TODO: cleanup comments. mention how this is web-to-web whereas basic was non-web to web
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/jcuga/golongpoll"
)

func main() {
	manager, err := golongpoll.CreateManager()
	if err != nil {
		log.Fatalf("Failed to create manager: %q", err)
	}
	// Serve our example driver webpage
	http.HandleFunc("/advanced", AdvancedExampleHomepage)
	// Serve handler that generates events
	http.HandleFunc("/advanced/user/action", getUserActionHandler(manager))
	// Serve handler that subscribes to events.
	http.HandleFunc("/advanced/events", getEventSubscriptionHandler(manager))
	// Start webserver
	http.ListenAndServe("127.0.0.1:8081", nil)

	// We'll never get here as long as http.ListenAndServe starts successfully
	// because it runs until you kill the program (like pressing Control-C)
	// Buf if you make a stoppable http server, or want to shut down the
	// internal longpoll manager for other reasons, you can do so via
	// Shutdown:
	manager.Shutdown() // Stops the internal goroutine that provides subscription behavior
}

// A fairly trivial json-convertable structure that shows to serve
type UserAction struct {
	User     string `json:"user"`
	Action   string `json:"action"`
	IsPublic bool   `json:"is_public"` // Whether or not others can see this
}

// Notice how we use a closure to capture the manager so we can interact
// with the LongpollManager while handling a web request.
func getUserActionHandler(manager *golongpoll.LongpollManager) func(w http.ResponseWriter, r *http.Request) {
	// Creates closure that captures the LongpollManager
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		action := r.URL.Query().Get("action")
		public := r.URL.Query().Get("public")
		// Perform validation on url query params:
		if len(user) == 0 || len(action) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Missing required URL param."))
			return
		}
		if user != "larry" && user != "moe" && user != "curly" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Not a user."))
			return
		}
		if len(public) > 0 && public != "true" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Optional param 'public' must be 'true' if present."))
			return
		}
		// convert string arg to bool
		isPublic := false
		if public == "true" {
			isPublic = true
		}
		actionEvent := UserAction{User: user, Action: action, IsPublic: isPublic}
		// Publish on public subscription channel if the action is public
		if isPublic {
			manager.Publish("public_actions", actionEvent)
		}
		// Publish on user's private channel regardless
		manager.Publish(user+"_actions", actionEvent)
	}
}

// Notice how we use a closure to capture the manager so we can interact
// with the LongpollManager while handling a web request.
// Also notice how we wrap manager.SubscriptionHandler to add an additional
// layer of functionality--in this case a dummy auth check (which serves only as
// an example and should never be used to enforce real user authentication!).
func getEventSubscriptionHandler(manager *golongpoll.LongpollManager) func(w http.ResponseWriter, r *http.Request) {
	// Creates closure that captures the LongpollManager
	// Wraps the manager.SubscriptionHandler with a layer of dummy access control validation
	return func(w http.ResponseWriter, r *http.Request) {
		category := r.URL.Query().Get("category")
		user := r.URL.Query().Get("user")

		// Dummy user access control in the event the client is requesting
		// a user's private activity stream:
		if category == "larry_actions" && user != "larry" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("You're not Larry."))
			return
		}
		if category == "moe_actions" && user != "moe" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("You're not Moe."))
			return
		}
		if category == "curly_actions" && user != "curly" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("You're not Curly."))
			return
		}

		// The only other channel we support is the public one:
		if category != "public_actions" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Subscription channel does not exist."))
			return
		}

		// Client is either requesting the public stream, or a private
		// stream that they're allowed to see.
		// Go ahead and let the subscription happen:
		manager.SubscriptionHandler(w, r)
	}
}

// TODO: two subscriptions, public and private
// Controls to perform actions, show errors
// TODO: switch between users link
// TODO: note this means js must pull current user from url param
func AdvancedExampleHomepage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
<html>
<head>
    <title>golongpoll advanced example</title>
</head>
<body>
    <h1>golongpoll advanced example</h1>
    <h2>Here's whats happening around the farm:</h2>
    <ul id="animal-events"></ul>
<script src="http://code.jquery.com/jquery-1.11.3.min.js"></script>
<script>

</script>
</body>
</html>`)
}
