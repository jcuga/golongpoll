// Provides example of how one can wrap SubscriptionHandler and PublishHandler
// to provide authentication. One could do other things like limit what categories
// can be published to, or what sort of data can be published and by whom.
// This also provides examples of how the javascript and golang clients
// can provide http basic auth or other header data for authentication.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/jcuga/golongpoll"
	"github.com/jcuga/golongpoll/client"
)

func main() {
	staticClientJs := flag.String("clientJs", "./js-client/client.js", "where the static js-client/client.js is located relative to where this binary runs")
	serveAddr := flag.String("serve", "127.0.0.1:8080", "Address to serve HTTP on.")
	flag.Parse()

	manager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled: true,
	})
	if err != nil {
		log.Fatalf("Failed to create manager: %q", err)
	}

	http.HandleFunc("/", HomePage)
	http.HandleFunc("/js/client.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, *staticClientJs)
	})
	http.HandleFunc("/basic-events", WithBasicAuth(manager.SubscriptionHandler))
	http.HandleFunc("/basic-publish", WithBasicAuth(manager.PublishHandler))
	http.HandleFunc("/header-events", WithMockHeaderAuth(manager.SubscriptionHandler))
	http.HandleFunc("/header-publish", WithMockHeaderAuth(manager.PublishHandler))
	fmt.Printf("Serving webpage at http://%s\n", *serveAddr)

	go clientWithBasicAuth(*serveAddr)
	go clientWithMockHeaderAuth(*serveAddr)

	http.ListenAndServe(*serveAddr, nil)
}

func WithBasicAuth(handler func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok {
			fmt.Println("Error parsing basic auth")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if u != "user123" {
			fmt.Printf("Username provided is incorrect: %s\n", u)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if p != "pa$$w0rd" {
			fmt.Printf("Password provided is incorrect: %s\n", u)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// NOTE: could add other checks/logic here as desired.

		// passed auth check, execute oiginal handler
		handler(w, r)
	}
}

func WithMockHeaderAuth(handler func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-MOCK-AUTH-TOKEN")
		if token != "abcdefg123456789" {
			fmt.Printf("token provided is incorrect: %s\n", token)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// NOTE: could add other checks/logic here as desired.

		// passed auth check, execute oiginal handler
		handler(w, r)
	}
}

// Demonstrates how a client can be configured to use http basic auth.
// This will subscribe to auth-protected events and then publish to that
// same auth-protected category acknowledging that it was able to see the event.
func clientWithBasicAuth(serveAddr string) {
	subUrl, err := url.Parse(fmt.Sprintf("http://%s/basic-events", serveAddr))
	if err != nil {
		panic(err)
	}

	pubUrl, err := url.Parse(fmt.Sprintf("http://%s/basic-publish", serveAddr))
	if err != nil {
		panic(err)
	}

	c, err := client.NewClient(client.ClientOptions{
		SubscribeUrl:      *subUrl,
		PublishUrl:        *pubUrl,
		Category:          "test-basic-in",
		BasicAuthUsername: "user123",
		BasicAuthPassword: "pa$$w0rd",
		LoggingEnabled:    true,
	})
	if err != nil {
		fmt.Println("FAILED TO CREATE LONGPOLL CLIENT: ", err)
		return
	}

	for event := range c.Start(time.Now()) {
		msg := event.Data.(string)
		if pubErr := c.Publish("test-basic-out", fmt.Sprintf("go client (basic auth) saw message: %s", msg)); pubErr != nil {
			fmt.Printf("ERROR publishing event: %v\n", pubErr)
		}
	}
}

// Demonstrates how a client can be configured to use header based auth.
// This will subscribe to auth-protected events and then publish to that
// same auth-protected category acknowledging that it was able to see the event.
func clientWithMockHeaderAuth(serveAddr string) {
	subUrl, err := url.Parse(fmt.Sprintf("http://%s/header-events", serveAddr))
	if err != nil {
		panic(err)
	}

	pubUrl, err := url.Parse(fmt.Sprintf("http://%s/header-publish", serveAddr))
	if err != nil {
		panic(err)
	}

	c, err := client.NewClient(client.ClientOptions{
		SubscribeUrl:   *subUrl,
		PublishUrl:     *pubUrl,
		ExtraHeaders:   []client.HeaderKeyValue{{Key: "X-MOCK-AUTH-TOKEN", Value: "abcdefg123456789"}},
		Category:       "test-header-in",
		LoggingEnabled: true,
	})
	if err != nil {
		fmt.Println("FAILED TO CREATE LONGPOLL CLIENT: ", err)
		return
	}

	for event := range c.Start(time.Now()) {
		msg := event.Data.(string)
		if pubErr := c.Publish("test-header-out", fmt.Sprintf("go client (header auth) saw message: %s", msg)); pubErr != nil {
			fmt.Printf("ERROR publishing event: %v\n", pubErr)
		}
	}
}

func HomePage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
	<html>
	<head>
		<title>golongpoll auth</title>
			</head>
	<body>
		<h1>golongpoll auth</h1>
		<p>Try publishing events that are protected by different auth mechanisms. Note there are server-side goroutines with longpoll clients
			that will listen for events published by this page and acknowledge them back via a similarly named category. ("out" versus "in")</p>
		<h2>HTTP Basic Auth Events</h2>
		<p>Showing all events protected by HTTP basic auth.</p>
		<ul id="basic-auth-events"></ul>
		<input id="send-basic-input"type="text" /><button id="send-basic-btn">Publish</button>

		<h2>HTTP Header Based Auth Events</h2>
		<p>Showing all events protected by mock HTTP header based auth.</p>
		<ul id="header-auth-events"></ul>
		<input id="send-header-input"type="text" /><button id="send-header-btn">Publish</button>

		<!-- Serving the gonlongpoll js client at this address: -->
		<script src="/js/client.js"></script>
		<script>

			var basicClient = golongpoll.newClient({
				subscribeUrl: "/basic-events",
				category: "test-basic-out",
				publishUrl: "/basic-publish",
				// NOTE: without setting sinceTime here, defaults to only new events (sinceTime of now)
				loggingEnabled: false,
				basicAuthUsername: "user123",
				basicAuthPassword: "pa$$w0rd",
				onEvent: function (event) {
					document.getElementById("basic-auth-events").insertAdjacentHTML('beforeend',"<li>" + (new Date(event.timestamp).toLocaleTimeString()) + ": " + event.data + "</li>");
				},
			});

			// newClient returns null if failed.
			if (!basicClient) {
				alert("Failed to create golongpoll client.");
			} else {
				console.log(basicClient);
			}

			var headerClient = golongpoll.newClient({
				subscribeUrl: "/header-events",
				category: "test-header-out",
				publishUrl: "/header-publish",
				// NOTE: without setting sinceTime here, defaults to only new events (sinceTime of now)
				loggingEnabled: false,
				extraRequestHeaders: [{key: "X-MOCK-AUTH-TOKEN", value: "abcdefg123456789"}],
				onEvent: function (event) {
					document.getElementById("header-auth-events").insertAdjacentHTML('beforeend',"<li>" + (new Date(event.timestamp).toLocaleTimeString()) + ": " + event.data + "</li>");
				},
			});

			// newClient returns null if failed.
			if (!headerClient) {
				alert("Failed to create golongpoll client.");
			} else {
				console.log(headerClient);
			}

			document.getElementById("send-basic-btn").onclick = function(event) {
				var data = document.getElementById("send-basic-input").value;
				if (data.length == 0) {
					alert("input cannot be empty");
					return;
				}
				basicClient.publish("test-basic-in", data,
					function () {
						document.getElementById("send-basic-input").value = '';
					},
					function(status, resp) {
						alert("post request failed. status: " + status + ", resp: " + resp);
					});
				};

			document.getElementById("send-header-btn").onclick = function(event) {
				var data = document.getElementById("send-header-input").value;
				if (data.length == 0) {
					alert("input cannot be empty");
					return;
				}
				headerClient.publish("test-header-in", data,
					function () {
						document.getElementById("send-header-input").value = '';
					},
					function(status, resp) {
						alert("post request failed. status: " + status + ", resp: " + resp);
					});
				};

		</script>
	</body>
	</html>`)
}
