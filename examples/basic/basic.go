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
	"context"
	"fmt"
	"github.com/jcuga/golongpoll"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func main() {
	filePersistor, err := golongpoll.NewFilePersistor("/home/pi/code/golongpoll/events.data", 4096, 30)
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

	mux := http.NewServeMux()
	// Serve our basic example driver webpage
	mux.HandleFunc("/basic", BasicExampleHomepage)
	// Serve our event subscription web handler
	mux.HandleFunc("/basic/events", manager.SubscriptionHandler)
	mux.HandleFunc("/basic/publish", getPublishHandler(manager))

	server := &http.Server{Addr: "127.0.0.1:8081", Handler: mux}

	fmt.Println("Serving webpage at http://127.0.0.1:8081/basic")
	httpDone := make(chan bool)
	go func() {
		if err := server.ListenAndServe(); err != nil {
			// handle err
			close(httpDone)
		}
		sErr := manager.ShutdownWithTimeout(5) // TODO: comment about this
		if sErr != nil {
			fmt.Println("Got shutdown error: ", sErr)
		}
	}()

	// Setting up signal capturing
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	// Waiting for SIGINT (pkill -2)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		// TODO: handle err
		fmt.Printf("Error shutting down: %v\n", err)
	}

	fmt.Println("waiting for http done")
	// Wait for ListenAndServe goroutine to close.
	<-httpDone
	fmt.Println("all done for real") // TODO: remove me
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
