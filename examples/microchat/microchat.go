// Super simple chat server with some pre-defined rooms and login-less posting
// using display names. No attempt is made at security.
// This shows how one could use categories and the longpoll pub-sub to make
// a chat server with individual rooms/topics.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/jcuga/golongpoll"
)

func main() {
	listenAddress := flag.String("serve", "127.0.0.1:8080", "address:port to serve.")
	staticClientJs := flag.String("clientJs", "./js-client/client.js", "where the static js-client/client.js is located relative to where this binary runs")
	flag.Parse()

	http.HandleFunc("/js/client.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, *staticClientJs)
	})

	// NOTE: to have chats persist across program runs, use Options.AddOn: FilePersistorAddOn.
	manager, err := golongpoll.StartLongpoll(golongpoll.Options{
		// How many chats per topic to hang on to:
		MaxEventBufferSize: 1000,
		LoggingEnabled:     true,
	})
	if err != nil {
		log.Fatalf("Failed to create chat longpoll manager: %q\n", err)
	}

	http.HandleFunc("/", IndexPage)
	http.HandleFunc("/topic/news", TopicPage("news"))
	http.HandleFunc("/topic/sports", TopicPage("sports"))
	http.HandleFunc("/topic/politics", TopicPage("politics"))
	http.HandleFunc("/topic/humor", TopicPage("humor"))
	// NOTE: using the plain publish handler.  If one wanted to add
	// additional behavior like escaping or validating data, one could make a
	// http handler function that has a closure capturing the longpoll manager
	// and then call manager.Publish directly.
	http.HandleFunc("/post", manager.PublishHandler)
	http.HandleFunc("/events", manager.SubscriptionHandler)
	log.Printf("Launching chat server on http://%s\n", *listenAddress)
	http.ListenAndServe(*listenAddress, nil)
}

type ChatPost struct {
	DisplayName string `json:"display_name"`
	Message     string `json:"message"`
	Topic       string `json:"topic"`
}

func IndexPage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
	<html>
	<head>
		<title>golongpoll microchat</title>
	</head>
	<body>
		<h2>Chat Rooms</h2>
		<ul>
			<li><a href="/topic/news">News</a></li>
			<li><a href="/topic/sports">Sports</a></li>
			<li><a href="/topic/politics">Politics</a></li>
			<li><a href="/topic/humor">Humor</a></li>
		</ul>
	</body>
	</html>`)
}

func TopicPage(topic string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `
		<html>
		<head>
			<title>golongpoll microchat</title>
		</head>
		<body>
			<a href="/">Select Room</a>
			<h2>Chat Room: %s</h2>
			<ul id="chats"></ul>
			Display Name:
			<input id="display-name-input" type="text" /><br/>
			Message:
			<input id="chat-input" type="text" /><br/>
			<button id="chat-send">Send</button>
			<!-- Serving the gonlongpoll js client at this address: -->
			<script src="/js/client.js"></script>
		<script>
			var client = golongpoll.newClient({
				subscribeUrl: "/events",
				category: "%s",
				publishUrl: "/post",
				// get events since last 24 hours
				sinceTime: Date.now() - (24 * 60 * 60 * 1000),
				loggingEnabled: true,
				onEvent: function (event) {
					// NOTE: this does NOTE escape the event.data.chat field!
					// In a real webpage, one would want to do so to avoid arbitrary html/js injected here!
					document.getElementById("chats").insertAdjacentHTML('beforeend', "<li><i>" + (new Date(event.timestamp).toLocaleTimeString()) +
						"</i> <b>" + event.data["display_name"] + "</b>: " + event.data["chat"] + "</li>");
				},
			});

			// newClient returns null if failed.
			if (!client) {
				alert("Failed to create golongpoll client.");
			} else {
				console.log(client);
			}

			document.getElementById("chat-send").onclick = function(event) {
				var msg = document.getElementById("chat-input").value;
				if (msg.length == 0) {
					alert("message cannot be empty");
					return;
				}
				var displayName = document.getElementById("display-name-input").value;
				if (displayName.length == 0) {
					alert("display name cannot be empty");
					return;
				}
				client.publish("%s", {display_name: displayName, chat: msg},
					function () {
						document.getElementById("chat-input").value = '';
					},
					function(status, resp) {
						alert("publish post request failed. status: " + status + ", resp: " + resp);
					}
				);
			};
		</script>
		</body>
		</html>`, topic, topic, topic)
	}
}
