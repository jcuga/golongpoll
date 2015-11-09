// This is a basic example.  Here we have some random events being generated
// by a goroutine, and the SubscriptionHandler function being served locally.
//
// Having events coming from another goroutine shows how some other part
// of your go program could be generating events.  Events don't have to be
// created by other HTTP requests.  See the advanced example for events being
// created by web requests.
//
// The events have a simple string payload, but they could be anything that
// is serializable to JSON.  See the advanced example for more complicated
// event payloads
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
	manager, err := golongpoll.CreateManager()
	if err != nil {
		log.Fatalf("Failed to create manager: %q", err)
	}
	// pump out random events
	go generateRandomEvents(manager)
	// Serve our basic example driver webpage
	http.HandleFunc("/basic", BasicExampleHomepage)
	// Serve our event subscription web handler
	http.HandleFunc("/basic/events", manager.SubscriptionHandler)
	http.ListenAndServe("127.0.0.1:8081", nil)

	// We'll never get here as long as http.ListenAndServe starts successfully
	// because it runs until you kill the program (like pressing Control-C)
	// Buf if you make a stoppable http server, or want to shut down the
	// internal longpoll manager for other reasons, you can do so via
	// Shutdown:
	manager.Shutdown() // Stops the internal goroutine that provides subscription behavior
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
	for {
		time.Sleep(time.Duration(rand.Intn(5000)) * time.Millisecond)
		lpManager.Publish("farm", farm_events[rand.Intn(len(farm_events))])
	}
}

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
    // This is hte time format the longpoll server uses.
    var sinceTime = (new Date(Date.now())).getTime();

    // Let's subscribe to animal related events.
    var category = "farm";

    (function poll() {
        var timeout = 15;  // in seconds
        var optionalSince = "";
        if (sinceTime) {
            optionalSince = "&since_time=" + sinceTime;
        }
        var pollUrl = "http://127.0.0.1:8081/basic/events?timeout=" + timeout + "&category=" + category + optionalSince;
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
                if (data && data.events && data.events.length == 0) {
                    console.log("Empty events, that's weird!")
                    // should get a timeout response, not an empty event array
                    // if no events during longpoll window.  so this is weird
                    setTimeout(poll, errorDelay);
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
                console.log("Didn't get expected event data, try again shortly...");
                setTimeout(poll, errorDelay);
            }, dataType: "json",
        error: function (data) {
            console.log("Error in ajax request--trying again shortly...");
            setTimeout(poll, 3000);  // 3s
        }
        });
    })();
</script>
</body>
</html>`)
}
