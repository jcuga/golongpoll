// This is a more advanced example that shows a few more possibilities when
// using golongpoll.
//
// In this example, we'll demonstrate publishing events via http handlers and
// also adding additional logic/validation on top of the LongpollManager's
// SubscriptionHandler function.
//
// To run this example:
//   go build examples/advanced/advanced.go
// Then run the binary and visit http://127.0.0.1:8081/advanced
// Try clicking the action button with a variety of different actions and
// toggle whether or not they are public or private.  Then switch to other
// users and do the same.  Observe what you see.  Better yet, have multiple
// browser windows open and click from the different users and observe.
//
// Noteworthy things going on in this example:
//   - Event payloads are an actual json object, not a plain string.
//
//   - we use closures to capture the LongpollManager to support calling
//     Publish() from an http handler, and to wrap the SubscriptionHandler
//     with our own logic.  This is safe to have random http handlers
//     calling functions on LongpollManager because the manager's data members
//     are all channels which are made for sharing.
//
//   - The html and javascript is not the prettiest :-P
//
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/jcuga/golongpoll"
)

func main() {
	manager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled:                 true,
		MaxLongpollTimeoutSeconds:      120,
		MaxEventBufferSize:             100,
		EventTimeToLiveSeconds:         60 * 2, // Event's stick around for 2 minutes
		DeleteEventAfterFirstRetrieval: false,
	})
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
	fmt.Println("Serving webpage at http://127.0.0.1:8081/advanced")
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

// A fairly trivial json-convertable structure that demonstrates how events
// don't have to be a plain string.  Anything JSON will work.
type UserAction struct {
	User     string `json:"user"`
	Action   string `json:"action"`
	IsPublic bool   `json:"is_public"` // Whether or not others can see this
}

// Creates a closure function that is used as an http handler that allows
// users to publish events (what this example is calling a user action event)
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
		// Publish on public subscription channel if the action is public.
		// Everyone can see this event.
		if isPublic {
			manager.Publish("public_actions", actionEvent)
		}
		// Publish on user's private channel regardless
		// Only the user that called this will see the event.
		manager.Publish(user+"_actions", actionEvent)
	}
}

// Creates a closure function that is used as an http handler for browsers to
// subscribe to events via longpolling.
// Notice how we're wrapping LongpollManager.SubscriptionHandler in order to
// add our own logic and validation.
func getEventSubscriptionHandler(manager *golongpoll.LongpollManager) func(w http.ResponseWriter, r *http.Request) {
	// Creates closure that captures the LongpollManager
	// Wraps the manager.SubscriptionHandler with a layer of dummy access control validation
	return func(w http.ResponseWriter, r *http.Request) {
		category := r.URL.Query().Get("category")
		user := r.URL.Query().Get("user")
		// NOTE: real user authentication should be used in the real world!

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

		// Only allow supported subscription categories:
		if category != "public_actions" && category != "larry_actions" &&
			category != "moe_actions" && category != "curly_actions" {
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

// Here we're providing a webpage that lets you pick a user, perform an action
// and see the recent history (last 2 min) of all your actions and any public
// action by the other users.
//
// In this code you'll see a sample of how to implement longpolling on the
// client side in javascript.  I used jquery here.  There are TWO longpolls
// going on in this webpage: for your actions, and for everyone's public actions
//
// I was too lazy to serve this file statically.
// This is me setting a bad example :)
func AdvancedExampleHomepage(w http.ResponseWriter, r *http.Request) {
	// Hacky way to inject the current user into the webpage:
	username := r.URL.Query().Get("user")
	if username == "" {
		username = "curly"
	}
	fmt.Fprintf(w, `
<html>
<head>
    <title>golongpoll advanced example</title>
</head>
<body>
    <h1>Hello, <script> document.write("%s"); </script></h1>
    Switch to user:
    <ul>
    	<li><a href="/advanced?user=curly">Curly</a></li>
    	<li><a href="/advanced?user=moe">Moe</a></li>
    	<li><a href="/advanced?user=larry">Larry</a></li>
    </ul>

    <div>
    	<h3>Try doing something:</h3>
		<input type="radio" name="actionGroup" value="punch"> Punch<br>
		<input type="radio" name="actionGroup" value="slap" checked> Slap<br>
		<input type="radio" name="actionGroup" value="poke"> Poke<br>
		<input type="radio" name="actionGroup" value="nuk nuk nuk"> Say: Nuk Nuk Nuk!<br><br>
		<input type="checkbox" id="isPublic" value="true"> Let others see that I did this.<br>
		<input type="button" id="action-button" value="Do it!">
    </div>
<hr>
<h3>Activity Stream</h3>
    <table border="1">
    	<tr>
			<th>Your actions</th>
			<th>Everyone's public actions</th>
    	</tr>
    	<tr>
    		<td style="vertical-align:top;">
    			<table border="1" id="your-actions">
    			</table>
    		</td>
    		<td style="vertical-align:top;">
    			<table border="1" id="public-actions">
    			</table>
    		</td>
    	</tr>
    </table>

<script src="http://code.jquery.com/jquery-1.11.3.min.js"></script>
<script>
	// This is a bunch of copy-n-paste hackathon javascript that is not good form
	// The point of this example is to demonstrate the longpoll usage, not how
	// to write good js/html

    // for browsers that don't have console
    if(typeof window.console == 'undefined') { window.console = {log: function (msg) {} }; }

    // Start checking for any events that occurred within 2 minutes prior to page load
    // so you can switch pages to other users, and then come back and see
    // recent events:
    var yourActionsSinceTime = (new Date(Date.now() - 120000)).getTime();;

    // Let's subscribe to your events.
    var yourActionsCategory = "%s_actions";

    // Longpoll subscription for your actions.  this will show both public and
    // private events because that's what the server is publishing on this
    // category, and only you are allowed to access it.
    (function pollYourActions() {
        var timeout = 45;  // in seconds
        var optionalSince = "";
        if (yourActionsSinceTime) {
            optionalSince = "&since_time=" + yourActionsSinceTime;
        }
        var pollUrl = "/advanced/events?user=%s&timeout=" + timeout + "&category=" + yourActionsCategory + optionalSince;
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
                        var publicString = "(public)";
                        if (event.data.is_public === false) {
                    		var publicString = "(private)";
                        }
                        // prepend instead of append so newest is up top--easier to see with no scrolling
                        $("#your-actions").prepend("<tr><td>" + event.data.user + ": " + event.data.action + " " + publicString + " at " + (new Date(event.timestamp).toLocaleTimeString()) +  "</td></tr>")
                        // Update sinceTime to only request events that occurred after this one.
                        yourActionsSinceTime = event.timestamp;
                    }
                    // success!  start next longpoll
                    setTimeout(pollYourActions, successDelay);
                    return;
                }
                if (data && data.timeout) {
                    console.log("No events, checking again.");
                    // no events within timeout window, start another longpoll:
                    setTimeout(pollYourActions, successDelay);
                    return;
                }
                if (data && data.error) {
                    console.log("Error response: " + data.error);
                    console.log("Trying again shortly...")
                    setTimeout(pollYourActions, errorDelay);
                    return;
                }
                // We should have gotten one of the above 3 cases:
                // either nonempty event data, a timeout, or an error.
                console.log("Didn't get expected event data, try again shortly...");
                setTimeout(pollYourActions, errorDelay);
            }, dataType: "json",
        error: function (data) {
            console.log("Error in ajax request--trying again shortly...");
            setTimeout(pollYourActions, errorDelay);  // 3s
        }
        });
    })();

    // Add another longpoller for all user's public events:
    var publicActionsSinceTime = (new Date(Date.now() - 120000)).getTime();;
    var publicActionsCategory = "public_actions";

    // Longpoll subscription for everyone's (public) actions.
    // You wont see other people's private actions
    (function pollPublicActions() {
        var timeout = 45;  // in seconds
        var optionalSince = "";
        if (publicActionsSinceTime) {
            optionalSince = "&since_time=" + publicActionsSinceTime;
        }
        var pollUrl = "/advanced/events?user=%s&timeout=" + timeout + "&category=" + publicActionsCategory + optionalSince;
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
                        var publicString = "(public)";
                        if (event.data.is_public === false) {
                            var publicString = "(private)";
                        }
                        // prepend instead of append so newest is up top--easier to see with no scrolling
                        $("#public-actions").prepend("<tr><td>" + event.data.user + ": " + event.data.action + " " + publicString + " at " + (new Date(event.timestamp).toLocaleTimeString()) +  "</td></tr>")
                        // Update sinceTime to only request events that occurred after this one.
                        publicActionsSinceTime = event.timestamp;
                    }
                    // success!  start next longpoll
                    setTimeout(pollPublicActions, successDelay);
                    return;
                }
                if (data && data.timeout) {
                    console.log("No events, checking again.");
                    // no events within timeout window, start another longpoll:
                    setTimeout(pollPublicActions, successDelay);
                    return;
                }
                if (data && data.error) {
                    console.log("Error response: " + data.error);
                    console.log("Trying again shortly...")
                    setTimeout(pollPublicActions, errorDelay);
                    return;
                }
                // We should have gotten one of the above 3 cases:
                // either nonempty event data, a timeout, or an error.
                console.log("Didn't get expected event data, try again shortly...");
                setTimeout(pollPublicActions, errorDelay);
            }, dataType: "json",
        error: function (data) {
            console.log("Error in ajax request--trying again shortly...");
            setTimeout(pollPublicActions, errorDelay);  // 3s
        }
        });
    })();


// Click handler for action button.  This hits the http handler that publishes
// events.
$( "#action-button" ).click(function() {
	var actionString = $('input:radio[name=actionGroup]:checked').val();
	var optionalPublic = "";
	if ($("#isPublic").is(':checked')) {
		optionalPublic = "&public=true";
	}
    var actionSubmitUrl = "/advanced/user/action?user=%s&action=" + actionString + optionalPublic;

	$.ajax({ url: actionSubmitUrl,
            success: function(data) {
            	console.log("action submitted");
            }, dataType: "html",
        error: function (data) {
        	alert("Action failed due to error.");
        }
        });
});

</script>
</body>
</html>`, username, username, username, username, username)
	// Those ugly, repeated username params are all populating some %s placeholder
	// throughout our html/javascript.
}
