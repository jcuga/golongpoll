# Golongpoll Examples
These instructions assume you are running them from within the ./examples directory.  Note the `-clientJs` flag sent to many of these examples. This is used to tell the http server where to find the static `js-client/client.js` file.

## Chatbot
This example demonstrates using both the javascript and go clients to create a dummy chatbot app. The webpage uses the javascript client while the go webserver spins up a goroutine that uses the go client to subscribe to messages sent by the webpage and respond (publish) with response messages.

To Build:
```
go build -o chatbot_example chatbot/chatbot.go
```
Note that we rename the output `chatbot_example` since there's already a folder called `chatbot`

To Run:
```
./chatbot_example -clientJs ../js-client/client.js
```
Then visit: `http://127.0.0.1:8101/chatbot`

## FilePersist
In this example, you can publish events via webpage and they'll be saved to file via the `FilePersistorAddOn`.  Test this out by publishing some events, then kill the program, restart and observe the events still show up.

To Build:
```
go build -o filepersist_example filepersist/filepersist.go
```

To Run:
```
./filepersist_example -clientJs ../js-client/client.js -persistTo ./filepersist_example.data
```
Then visit: `http://127.0.0.1:8102/filepersist`

## Stressor
A simple server that shows published events on a given category and command line client(s) to stress the server via the longpoll manager's publish HTTP handler.

### Server
To Build:
```
go build -o stressor_server stressor/server/main.go
```

To Run:
```
./stressor_server -clientJs ../js-client/client.js -serve "127.0.0.1:8080" -category test123
```
Then visit: `http://127.0.0.1:8080/`

### Client
To Build:
```
go build -o stressor_client stressor/client/main.go
```

To Run:
```
./stressor_client -publishUrl "http://127.0.0.1:8080/publish" -category test123 -delayMs 100 -count 1000
```

## Authentication
Shows how `LongpollManager.SubscriptionHandler` and `LongpollManager.PublishHandler` can be wrapped to require authentication.  The javascript and golang clients can be configured to supply HTTP basic auth or HTTP headers containing auth data.

To Build:
```
go build -o auth_example authentication/auth.go
```

To Run:
```
./auth_example -clientJs ../js-client/client.js -serve "127.0.0.1:8080"
```
Then visit the HTTP address passed to `-serve`, in this case: `http://127.0.0.1:8080`

## microchat
Simple dummy chat server that uses the standard publish/subscribe hooks.

To Build:
```
go build -o microchat_example microchat/microchat.go
```

To Run:
```
./microchat_example -clientJs ../js-client/client.js -serve "127.0.0.1:8080"
```

Then visit `http://127.0.0.1:8080/` in multiple browsers to chat.