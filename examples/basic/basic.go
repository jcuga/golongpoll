// This is a basic example of how to use golongpoll.
//
// In this example, we'll generate some random events and provide a way for
// browsers to subscribe to those events.
//
// To run this example:
//   go build examples/basic/basic.go
// Then run the binary and visit http://127.0.0.1:8081/basic
//
// A couple of notes:
//   - In this example, event payloads are string data, but they can be anything
//     that is convert-able to JSON (passes encoding/json's Marshal() function)
//
//   - LongpollManager.SubscriptionHandler is directly bound to a http handler.
//     But there's no reason why you can't wrap that handler in your own
//     function to add a layer of validation, access control, or whatever else
//     you can think up.  See the advanced example on how to do this.
//
//   - Our events are simple and randomly generated in a goroutine, but you can
//     create events anywhere and anyhow as long as you pass a reference to the
//     LongpollManager and just call Publish().  Maybe you have a goroutine that
//     checks the weather, a stock price, or something cool.  Or you can even
//     have another http handler that calls Publish().  To do this, you must
//     capture the manager reference in a closure.  See the advanced example.
//
package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/jcuga/golongpoll"
)

func main() {
	manager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled: true,
		// NOTE: if not defined here, other options have reasonable defaults,
		// so no need specifying options you don't care about
	})
	if err != nil {
		log.Fatalf("Failed to create manager: %q", err)
	}
	// pump out random events
	go generateRandomEvents(manager)
	// Serve our basic example driver webpage
	http.HandleFunc("/basic", BasicExampleHomepage)

	// Serve our event subscription web handler
	http.HandleFunc("/basic/events", manager.SubscriptionHandler)

	fmt.Println("Serving webpage at http://127.0.0.1:8081/basic")
	http.ListenAndServe("127.0.0.1:8081", nil)

	// We'll never get here as long as http.ListenAndServe starts successfully
	// because it runs until you kill the program (like pressing Control-C)
	// Buf if you make a stoppable http server, or want to shut down the
	// internal longpoll manager for other reasons, you can do so via
	// Shutdown:
	manager.Shutdown() // Stops the internal goroutine that provides subscription behavior
	// Again, calling shutdown is a bit silly here since the goroutines will
	// exit on main() exit.  But I wanted to show you that it is possible.
}

func generateRandomEvents(lpManager *golongpoll.LongpollManager) {
	farm_events := []string{
		"Cow says 'Moooo!'",
		"Duck went 'Quack!'",
		"Chicken says: 'Cluck!'",
		"Goat chewed grass.",
		"Pig went 'Oink! Oink!'",
		"Horse ate hay.",
		"Tractor went: Vroom Vroom!",
		"Farmer ate bacon.",
	}
	// every 0-5 seconds, something happens at the farm:
	for {
		time.Sleep(time.Duration(rand.Intn(5000)) * time.Millisecond)
		lpManager.Publish("farm", farm_events[rand.Intn(len(farm_events))])
	}
}

// Here we're providing a webpage that shows events as they happen.
// In this code you'll see a sample of how to implement longpolling on the
// client side in javascript.  I used jquery here...
//
// I was too lazy to serve this file statically.
// This is me setting a bad example :)
func BasicExampleHomepage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
<html>
<head>
    <title>golongpoll basic example</title>
</head>
<body>
    <h1>golongpoll basic example</h1>
    <h2>Here's whats happening around the farm:</h2>
    <ul id="animal-events"></ul>
<script src="http://code.jquery.com/jquery-1.11.3.min.js"></script>
<script>

    // for browsers that don't have console
    if(typeof window.console == 'undefined') { window.console = {log: function (msg) {} }; }

    // Start checking for any events that occurred after page load time (right now)
    // Notice how we use .getTime() to have num milliseconds since epoch in UTC
    // This is the time format the longpoll server uses.
    var sinceTime = (new Date(Date.now())).getTime();

    // Let's subscribe to animal related events.
    var category = "farm";

    (function poll() {
        var timeout = 45;  // in seconds
        var optionalSince = "";
        if (sinceTime) {
            optionalSince = "&since_time=" + sinceTime;
        }
        var pollUrl = "/basic/events?timeout=" + timeout + "&category=" + category + optionalSince;
        // how long to wait before starting next longpoll request in each case:
        var successDelay = 10;  // 10 ms
        var errorDelay = 3000;  // 3 sec
        $.ajax({ url: pollUrl,
            success: function(data) {
                if (data && data.events && data.events.length > 0) {
                    // got events, process them
                    // NOTE: these events are in chronological order (oldest first)
                    for (var i = 0; i < data.events.length; i++) {
                        // Display event
                        var event = data.events[i];
                        $("#animal-events").append("<li>" + event.data + " at " + (new Date(event.timestamp).toLocaleTimeString()) +  "</li>")
                        // Update sinceTime to only request events that occurred after this one.
                        sinceTime = event.timestamp;
                    }
                    // success!  start next longpoll
                    setTimeout(poll, successDelay);
                    return;
                }
                if (data && data.timeout) {
                    console.log("No events, checking again.");
                    // no events within timeout window, start another longpoll:
                    setTimeout(poll, successDelay);
                    return;
                }
                if (data && data.error) {
                    console.log("Error response: " + data.error);
                    console.log("Trying again shortly...")
                    setTimeout(poll, errorDelay);
                    return;
                }
                // We should have gotten one of the above 3 cases:
                // either nonempty event data, a timeout, or an error.
                console.log("Didn't get expected event data, try again shortly...");
                setTimeout(poll, errorDelay);
            }, dataType: "json",
        error: function (data) {
            console.log("Error in ajax request--trying again shortly...");
            setTimeout(poll, errorDelay);  // 3s
        }
        });
    })();
</script>
</body>
</html>`)
}
