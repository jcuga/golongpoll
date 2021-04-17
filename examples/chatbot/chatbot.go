// This example creates a dummy chatbot while demonstrating the following features:
// 1) Golang client used by the trivial chatbot
// 2) Javascript client used by UI.
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jcuga/golongpoll"
	"github.com/jcuga/golongpoll/client"
)

func main() {
	// Tell http.ServeFile where to get the client js file.
	// If running from root of checkout, use: -clientJs ./js-client/client.js
	// if in ./examples, use: -clientJs ../js-client/client.js
	// if in ./examples/chatbot, use: -clientJs ../../js-client/client.js
	// If using go 1.16 or higher, can simply use the embed directive instead of having to do this here, but supporting older versions.
	staticClientJs := flag.String("clientJs", "./js-client/client.js", "where the static js-client/client.js is located relative to where this binary runs")
	flag.Parse()

	manager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled: true,
	})
	if err != nil {
		log.Fatalf("Failed to create manager: %q", err)
	}

	http.HandleFunc("/chatbot", chatBotExampleHomepage)
	http.HandleFunc("/js/client.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, *staticClientJs)
	})
	http.HandleFunc("/chatbot/events", manager.SubscriptionHandler)
	http.HandleFunc("/chatbot/send", manager.PublishHandler)
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
		SubscribeUrl:   *u,
		Category:       "to-chatbot",
		LoggingEnabled: true,
	})
	if err != nil {
		fmt.Println("FAILED TO CREATE LONGPOLL CLIENT: ", err)
		return
	}

	dummyResponse := []string{
		"Mmhmm...",
		"Oh, interesting.",
		"Go on...",
		"You're telling me!",
		"Ha! That's funny!",
		"True, true. Very true.",
		"If I had a nickel for every time that happened to me!",
		"You tell such interesting stories.",
		"And then you found $20?",
		"I'm listening.",
		"Oh... I see.",
		"No!",
		"Yes!",
		"Maybe.",
		"For sure!",
		"Totally.",
	}

	// chatbot will only listen to new events (on or after now())
	for event := range c.Start(time.Now()) {
		// assuming all events are strings--which they are in this example
		msg := event.Data.(string)
		normMsg := strings.ToLower(msg)

		// Special responses for certain keywords, otherwise a random canned response.
		reply := ""
		if strings.Contains(normMsg, "taco") {
			reply = "I. LOVE. TACOS!!!!!!"
		} else if strings.Contains(normMsg, "coffee") {
			reply = "Need. M O R E.  Coffee!"
		} else if strings.Contains(normMsg, "cat") {
			reply = "BARK! BARK! BARK! Go away cat! GET!  BARRRRRK BARK bArK BaRk BAR-KUH!"
		} else {
			reply = dummyResponse[rand.Intn(len(dummyResponse))]
		}

		lpManager.Publish("from-chatbot", reply)
	}

	// Note: would only get here if c.Stop() was called--which in this example it never is.
	// You could put c.Stop() in one of the if blocks above to test this out.
	// For example, if talking about cat's is the last straw, the bot could stop responding.
	fmt.Println("Chatbot golongpoll.Client out of events, stopping.")
}

func chatBotExampleHomepage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
<html>
<head>
    <title>golongpoll chatbot</title>

    <style>
        p.msg-me {
            color: #000000;
            background-color: #00AAFF;
            padding: 1em;
            margin: 1em 1em 1em 4em;
            border: 2px solid #000000;
            border-radius: 1em;
        }

        p.msg-bot {
            color: #000000;
            background-color: #CCCCCC;
            padding: 1em;
            margin: 1em 4em 1em 1em;
            border: 2px solid #000000;
            border-radius: 1em;
        }
</style>
</head>
<body>
	<h1>golongpoll chatbot</h1>
    <p>Try conversing with this totally-not-fake chatbot.  Perhaps ask about tacos or coffee.  Do you have any cats?</p>

    <div id="conversation"></div>
	<input id="send-input"type="text" /><button id="send-btn">Send</button>

    <p>
        Notice that this is a contrived example that uses a single subscription category for to-chatbot and from-chatbot.
        If you open another browser window, you'll see the chatbot responses there as well. One could add a senderID in the
        messages (try using a json object instead of a plain message string) to the chatbot and have clients listen on their
        own channel based on their sender ID. The chatbot could then reply to the specific client's channel.
        The publish and subscribe http handlers could be wrapped in functions that require authentication as well.
    </p>

    <!-- Serving the gonlongpoll js client at this address: -->
    <script src="/js/client.js"></script>

	<script>
		var client = golongpoll.newClient({
			subscribeUrl: "/chatbot/events",
			category: "from-chatbot",
			publishUrl: "/chatbot/send",
			// NOTE: without setting sinceTime here, defaults to only new events (sinceTime of now)
			loggingEnabled: true,
			onEvent: function (event) {
				document.getElementById("conversation").insertAdjacentHTML('beforeend',"<p class=\"msg-bot\">" + event.data + "</p>");
			},
		});

		// newClient returns null if failed.
		if (!client) {
			alert("Failed to create golongpoll client.");
		} else {
			console.log(client);
		}

		document.getElementById("send-btn").onclick = function(event) {
			var data = document.getElementById("send-input").value;
			if (data.length == 0) {
				alert("input cannot be empty");
				return;
			}
			client.publish("to-chatbot", data,
				function () {
					document.getElementById("conversation").insertAdjacentHTML('beforeend',"<p class=\"msg-me\">" + data + "</p>");
					document.getElementById("send-input").value = '';
				},
				function(resp) {
					alert("post request failed: " + resp);
				});
			};
	</script>
</body>
</html>`)
}
