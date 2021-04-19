package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/jcuga/golongpoll"
)

func main() {
	staticClientJs := flag.String("clientJs", "./js-client/client.js", "where the static js-client/client.js is located relative to where this binary runs")
	serveAddr := flag.String("serve", "127.0.0.1:8080", "Address to serve HTTP on.")
	category := flag.String("category", "testing", "Longpoll category to display on webpage.")
	flag.Parse()

	manager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled: true,
	})
	if err != nil {
		log.Fatalf("Failed to create manager: %q", err)
	}

	http.HandleFunc("/", getStressorHomepage(*category))
	http.HandleFunc("/js/client.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, *staticClientJs)
	})
	http.HandleFunc("/events", manager.SubscriptionHandler)
	http.HandleFunc("/publish", manager.PublishHandler)
	fmt.Printf("Serving webpage at http://%s\n", *serveAddr)
	http.ListenAndServe(*serveAddr, nil)
}

func getStressorHomepage(category string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `
		<html>
		<head>
			<title>golongpoll stressor</title>
				</head>
		<body>
			<h1>golongpoll stressor</h1>
			<p>Showing all new events on category %s. Run one or more client_stressor programs to pump event data.</p>
			<ul id="events"></ul>

			<!-- Serving the gonlongpoll js client at this address: -->
			<script src="/js/client.js"></script>
			<script>
				var client = golongpoll.newClient({
					subscribeUrl: "/events",
					category: "%s",
					publishUrl: "/publish",
					// NOTE: without setting sinceTime here, defaults to only new events (sinceTime of now)
					loggingEnabled: false,
					onEvent: function (event) {
						document.getElementById("events").insertAdjacentHTML('beforeend',"<li>" + (new Date(event.timestamp).toLocaleTimeString()) + ": " + event.data + "</li>");
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
		</html>`, category, category)
	}
}
