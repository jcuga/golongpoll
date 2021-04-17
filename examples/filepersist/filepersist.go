// This example uses the FilePersistorAddOn to persist event data to file,
// allowing us to retain events across multiple program runs.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/jcuga/golongpoll"
)

func main() {
	// Tell http.ServeFile where to get the client js file.
	// If running from root of checkout, use: -clientJs ./js-client/client.js
	// if in ./examples, use: -clientJs ../js-client/client.js
	// if in ./examples/filepersist, use: -clientJs ../../js-client/client.js
	// If using go 1.16 or higher, can simply use the embed directive instead of having to do this here, but supporting older versions.
	staticClientJs := flag.String("clientJs", "./js-client/client.js", "where the static js-client/client.js is located relative to where this binary runs")
	persistFilename := flag.String("persistTo", "./filepersist_example.data", "where to store event json data.")
	flag.Parse()

	// Note the two options after filename: writeBufferSize and writeFlushPeriodSeconds.
	// Instead of immediately writing data to disk, it is buffered with periodic flushes.
	filePersistor, err := golongpoll.NewFilePersistor(*persistFilename, 4096, 2)
	if err != nil {
		fmt.Printf("Failed to create file persistor, error: %v", err)
		return
	}

	manager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled: true,
		AddOn:          filePersistor,
	})
	if err != nil {
		log.Fatalf("Failed to create manager: %q", err)
	}

	http.HandleFunc("/filepersist", filePersistorExampleHomepage)
	http.HandleFunc("/js/client.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, *staticClientJs)
	})
	http.HandleFunc("/filepersist/events", manager.SubscriptionHandler)
	http.HandleFunc("/filepersist/publish", manager.PublishHandler)
	fmt.Println("Serving webpage at http://127.0.0.1:8102/filepersist")
	http.ListenAndServe("127.0.0.1:8102", nil)
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
		manager.Publish("fileaddon-example", data)
	}
}

func filePersistorExampleHomepage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
<html>
<head>
    <title>golongpoll filepersist</title>
</head>
<body>
	<h1>golongpoll filepersist</h1>
    <p>Try generating some event data, then stop and retart the program and reload the webpage.
	Events from the previous program run should still show up since they were stored to file via FilePersistorAddOn.OnPublish()
	and loaded back in via FilePersistorAddOn.OnLongpollStart()</p>

	<p>Note the client is requesting all events on the category "fileaddon-example" since the past hour,
	so only seeing persisted events published within the last hour. Change the sinceTime option sent to golongpoll.newClient to configure that behavior.</p>


	<p>FilePersistorAddOn has writeBufferSize and writeFlushPeriodSeconds options to dictate how quickly data is flushed to disk.
	If you set writeFlushPeriodSeconds to a large value and kill the program via Control+C before the time has elapsed since
	last event publish, you can lose that data. However, you can set a smaller period value, or properly handle shutdown
	signals with a handler that calls LongpollManager.Shutdown() and the FilePersistorAddOn's OnShutdown() function will
	be called and no data will be lost even if the flush period has not elapsed yet.</p>


	<input id="publish-input"type="text" /><button id="publish-btn">Publish</button>
    <ul id="events"></ul>
    <!-- Serving the gonlongpoll js client at this address: -->
    <script src="/js/client.js"></script>
<script>

	var client = golongpoll.newClient({
		subscribeUrl: "/filepersist/events",
		category: "fileaddon-example",
		publishUrl: "/filepersist/publish",
		// get events since last hour
		sinceTime: Date.now() - (60 * 60 * 1000),
		loggingEnabled: true,
		// Not needed in this example, but showing that you can provide extra headers which can be useful
		// if you wrapped the LongpollManager.SubscriptionHandler with some authentication layer.
		extraRequestHeaders: [ {key: "Extra-Header-1", value: "One"}, { key: "Extra-Header-2", value: "Two"}],
		onEvent: function (event) {
			document.getElementById("events").insertAdjacentHTML('beforeend', "<li>" + (new Date(event.timestamp).toLocaleTimeString()) + ": " + event.data + "</li>");
		},
	});

	// newClient returns null if failed.
	if (!client) {
		alert("Failed to create golongpoll client.");
	} else {
		console.log(client);
	}

	document.getElementById("publish-btn").onclick = function(event) {
		var data = document.getElementById("publish-input").value;
		if (data.length == 0) {
			alert("input cannot be empty");
			return;
		}
		client.publish("fileaddon-example", data,
			function () {
				document.getElementById("publish-input").value = '';
			},
			function(resp) {
				alert("publish post request failed: " + resp);
			}
			);
	};

</script>
</body>
</html>`)
}
