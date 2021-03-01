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
	"github.com/jcuga/golongpoll"
	"github.com/jcuga/golongpoll/addons/persistence"
	"log"
	"net/http"
)

func main() {
	filePersistor, err := persistence.NewFilePersistor("/home/pi/code/golongpoll/events.data", 2, 10)
	if err != nil {
		fmt.Printf("Failed to create file persistor, error: %v", err)
		return
	}

	manager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled: true,
		// NOTE: if not defined here, other options have reasonable defaults,
		// so no need specifying options you don't care about
		AddOn: filePersistor,
	})
	if err != nil {
		log.Fatalf("Failed to create manager: %q", err)
	}

	// Serve our basic example driver webpage
	http.HandleFunc("/basic", BasicExampleHomepage)

	// Serve our event subscription web handler
	http.HandleFunc("/basic/events", manager.SubscriptionHandler)
	http.HandleFunc("/basic/publish", getPublishHandler(manager))

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

func getPublishHandler(manager *golongpoll.LongpollManager) func(w http.ResponseWriter, r *http.Request) {
	// Creates closure that captures the LongpollManager
	return func(w http.ResponseWriter, r *http.Request) {
		data := r.URL.Query().Get("data")
		if len(data) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Missing required URL param 'data'."))
			return
		}
		manager.Publish("echo", data)
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
    <input id="publish-input"type="text" /> <button id="publish-btn" onclick="publish()">Publish</button>
    <h2>Events</h2>
    <ul id="animal-events"></ul>
<script src="http://code.jquery.com/jquery-1.11.3.min.js"></script>
<script>

    function publish() {
        var data = $("#publish-input").val();
        if (data.length == 0) {
            alert("input cannot be empty");
            return;
        }

        var jqxhr = $.get( "/basic/publish", { data: data })
            .done(function() {
                console.log("post successful");
            })
            .fail(function() {
              alert( "post request failed" );
            });
    }

    // for browsers that don't have console
    if(typeof window.console == 'undefined') { window.console = {log: function (msg) {} }; }

    var sinceTime = 1;

    // Let's subscribe to animal related events.
    var category = "echo";

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
