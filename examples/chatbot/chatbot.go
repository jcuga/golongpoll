// This example creates a dummy chatbot while demonstrating the following features:
// 1) Golang client used by the trivial chatbot
// 2) Javascript client used by UI.
package main

import (
	"fmt"
	"github.com/jcuga/golongpoll"
	"github.com/jcuga/golongpoll/client"
	"log"
	"net/http"
	"net/url"
	"time"
)

func main() {

	// TODO: go thru and comment all this
	// TODO: comment how this is contrived (no multi user/segmentation)
	// TODO: comment how one could do that and add auth/checks (involve actual payload not plain string too)
	manager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled: true,
	})
	if err != nil {
		log.Fatalf("Failed to create manager: %q", err)
	}

	http.HandleFunc("/chatbot", chatBotExampleHomepage)
	http.HandleFunc("/chatbot/events", manager.SubscriptionHandler)
	http.HandleFunc("/chatbot/send", getSendHandler(manager))
	fmt.Println("Serving webpage at http://127.0.0.1:8101/chatbot")

	go beChatbot(manager)
	http.ListenAndServe("127.0.0.1:8101", nil)
}

func beChatbot(lpManager *golongpoll.LongpollManager) {
	u, err := url.Parse("http://127.0.0.1:8101/chatbot/events")
	if err != nil {
		panic(err)
	}

	c, err := client.NewClient(client.ClientOptions{
		Url:            *u,
		Category:       "to-chatbot",
		LoggingEnabled: true,
	})
	if err != nil {
		fmt.Println("FAILED TO CREATE LONGPOLL CLIENT: ", err)
		return
	}

	for event := range c.Start(time.Now()) {
		msg := event.Data.(string)
		reply := "I see you said: \"" + msg + "\""
		lpManager.Publish("from-chatbot", reply)
	}

	// Only happens if ...
	// TODO: is this when client stops, manager stops? either? too tired right now...
	fmt.Println("Chatbot golongpoll.Client out of events, stopping.")
}

func getSendHandler(manager *golongpoll.LongpollManager) func(w http.ResponseWriter, r *http.Request) {
	// Creates closure that captures the LongpollManager
	return func(w http.ResponseWriter, r *http.Request) {
		data := r.URL.Query().Get("data")
		if len(data) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Missing required URL param 'data'."))
			return
		}
		manager.Publish("to-chatbot", data)
	}
}

func chatBotExampleHomepage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
<html>
<head>
    <title>golongpoll chatbot</title>
</head>
<body>
<script>
function send() {
	var data = $("#send-input").val();
	if (data.length == 0) {
		alert("input cannot be empty");
		return;
	}

	var jqxhr = $.get( "/chatbot/send", { data: data })
		.done(function() {
			// TODO: style this, add formatted timestamp
			$("#conversation").append("<p>ME: " + data + "</p>");	
		})
		.fail(function() {
		  alert( "post request failed" );
		});
}
</script>
	<h1>golongpoll chatbot</h1>
	<div id="conversation" />
	<input id="send-input"type="text" /><button id="publish-btn" onclick="send()">Send</button>
	<!-- NOTE: jquery is NOT requried to use golongpoll or the golongpoll javascript client.
	Including here for shorter/lazier example. -->
<script src="http://code.jquery.com/jquery-1.11.3.min.js"></script>
<!-- TODO: update to master/not feature branch once merged. -->
<script>







var golongpoll = {
    newClient: function ({
            url,
            category,
            sinceTime=new Date().getTime(),
            lastId,
            onEvent = function (event) {},
            onFailure = function (errorMsg) { return true; },
            pollTimeoutSeconds=45,
            reattemptWaitSeconds=30,
            sucessWaitSeconds=0,
            basicAuthUsername="",
            basicAuthPassword="",
            loggingEnabled=false
        }) {
            if (!url) {
                client.log("newClient() requires non-empty 'url' option.");
                return null;
            }

            if (!category || category.length < 1 || category.length > 1024) {
                client.log("newClient() requires 'category' option between 1-1024 characters long.");
                return null;
            }

            if (sinceTime <= 0) {
                client.log("newClient() requires 'sinceTime' option > 0.");
                return null;
            }

            if (pollTimeoutSeconds < 1) {
                client.log("newClient() requires 'pollTimeoutSeconds' option >= 1.");
                return null;
            }

            if (reattemptWaitSeconds < 1) {
                client.log("newClient() requires 'reattemptWaitSeconds' option >= 1.");
                return null;
            }

            if ((basicAuthUsername.length > 0 && basicAuthPassword.length == 0) || (basicAuthUsername.length == 0 && basicAuthPassword.length > 0)) {
                client.log("newClient() requires 'basicAuthUsername' and 'basicAuthPassword' to be both empty or nonempty, not mixed.");
                return null;
            }

            var client = {
                running: true,
                stop: function () {
                    client.log("golongpoll client signaling stop.");
                    this.running = false;
                },
                url: url,
                category: category,
                sinceTime: sinceTime,
                lastId: lastId,
                onEvent: onEvent,
                onFailure: onFailure,
                pollTimeoutSeconds: pollTimeoutSeconds,
                reattemptWaitSeconds: reattemptWaitSeconds,
                // amount of time after event, default to zero, no wait which is typical
                sucessWaitSeconds: sucessWaitSeconds,
                basicAuthUsername: basicAuthUsername || null,
                basicAuthPassword: basicAuthPassword || null,
                loggingEnabled: loggingEnabled,
                log: function (msg) {
                    if (this.loggingEnabled === true) {
                        if (typeof window.console == 'undefined') { return; };
                        console.log("golongpoll client[\"" + this.category + "\"]: " + msg);
                    }
                }
            };

            (function poll() {
                var params = {
                    category: client.category,
                    timeout: client.pollTimeoutSeconds,
                    since_time: client.sinceTime,
                    last_id: !client.lastId ? "" : client.lastId
                };
                var esc = encodeURIComponent;
                var query = Object.keys(params)
                    .map(function(k) {return esc(k) + '=' + esc(params[k]);})
                    .join('&');

                var pollUrl = client.url + "?" + query

                var xmlHttp = new XMLHttpRequest();
                xmlHttp.onreadystatechange = function () {
                    var req = xmlHttp;
                        if (req.readyState === 4) {
                            if (req.status === 200) {
                                var data = JSON.parse(req.responseText);

                                if (data && data.events && data.events.length > 0) {
                                    // got events, process them
                                    // NOTE: these events are in chronological order (oldest first)
                                    for (var i = 0; i < data.events.length; i++) {
                                        var event = data.events[i];
                                        // Update sinceTime to only request events that occurred after this one.
                                        client.sinceTime = event.timestamp;
                                        client.lastId = event.id; // used with sinceTime to pick up where we left off
                                        client.onEvent(event);
                                        if (!client.running) { // check if it's time to quit before continuing
                                            return;
                                        }
                                    }
                                    if (!client.running) { // check if it's time to quit before continuing
                                        return;
                                    }
                                    // success!  start next longpoll
                                    setTimeout(poll, client.sucessWaitSeconds * 1000);
                                    return;
                                } else if (data && data.timeout) {
                                    // no events within timeout window, start another longpoll:
                                    if (!client.running) { // check if it's time to quit before continuing
                                        return;
                                    }
                                    client.log("no events, requesting again.");
                                    setTimeout(poll, 0);
                                    return;
                                } else if (data && data.error) {

                                    client.log("got error response: " + data.error);
                                    if (!client.running) { // check if it's time to quit before continuing
                                        return;
                                    }
                                    if (!client.onFailure(data.error)) {
                                        client.log("stopping due to onFailure returning false");
                                        return;
                                    }
                                    setTimeout(poll, client.reattemptWaitSeconds * 1000);
                                    return;
                                } else {
                                    client.log("got unexpected response: " + data);
                                    if (!client.running) { // check if it's time to quit before continuing
                                        return;
                                    }
                                    if (!client.onFailure("client got unexpected response: " + data)) {
                                        client.log("stopping due to onFailure returning false.");
                                        return;
                                    }
                                    setTimeout(poll, client.reattemptWaitSeconds * 1000);
                                }
                            } else {
                                client.log("request FAILED, response status: " + req.status);
                                if (!client.running) { // check if it's time to quit before continuing
                                    return;
                                }
                                if (!client.onFailure("request FAILED, response status: " + req.status)) {
                                    client.log("stopping due to onFailure returning false.");
                                    return;
                                }
                                setTimeout(poll, client.reattemptWaitSeconds * 1000)
                                return;
                            }
                        }
                };
                // NOTE: includes optional user/password for basic auth
                xmlHttp.open("GET", pollUrl, true, client.basicAuthUsername, client.basicAuthPassword); // true for asynchronous
                xmlHttp.send(null);
            })();

        return client;
    }
};





</script>
<script>
	var client = golongpoll.newClient({
		url: "/chatbot/events",
		category: "from-chatbot",
		// NOTE: without setting sinceTime here, defaults to only new events (sinceTime of now)
		loggingEnabled: true,
		onEvent: function (event) {
			// TODO: style this, add formatted timestamp from event.timestamp
			$("#conversation").append("<p>BOT: " + event.data + "</p>");
		},
	});

	// newClient returns null if failed.
	if (!client) {
		alert("Failed to create golongpoll client.");
	} else {
		console.log(client);
	}
</script>
</body>
</html>`)
}
