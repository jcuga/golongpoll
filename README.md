# golongpoll [![Build Status](https://travis-ci.org/jcuga/golongpoll.svg?branch=master)](https://travis-ci.org/jcuga/golongpoll) [![Coverage Status](https://coveralls.io/repos/jcuga/golongpoll/badge.svg?branch=master&service=github)](https://coveralls.io/github/jcuga/golongpoll?branch=master) [![GoDoc](https://godoc.org/github.com/jcuga/golongpoll?status.svg)](https://godoc.org/github.com/jcuga/golongpoll)
golang HTTP longpolling library, making web pub-sub easy!

Table of contents
=================
  * [Basic Usage](#basic-usage)
    * [HTTP Subscription Handler](#http-subscription-handler)
  * [What is longpolling?](#what-is-longpolling)
  * [Included examples](#included-examples)
    * [Basic](#basic)
    * [Advanced](#advanced)
  * [More advanced use](#more-advanced-use)
    * [Events with JSON payloads](#events-with-json-payloads)
    * [Wrapping subscriptions](#wrapping-subscriptions)
    * [Publishing events via the web](#publishing-events-via-the-web)

Basic usage
=================
To use, create a LongpollManager and then use it to pubish events and expose a http handler to subscribe to events.
```go
import	"github.com/jcuga/golongpoll"

// This launches a goroutine and creates channels for all the plumbing
manager, err := golongpoll.CreateManager()

// Expose events to browsers
// See subsection on how to interact with the subscription handler
http.HandleFunc("/events", manager.SubscriptionHandler)
http.ListenAndServe("127.0.0.1:8081", nil)

// Pass the manager around or create closures and publish:
manager.Publish("subscription-category", "Some data.  Can be string or any obj convertable to JSON")
manager.Publish("different-category", "More data")
```
Note that you can add extra access-control, validation, or other behavior on top of the manager's SubscriptionHandler.  See the [advanced example](#advanced).  This example also shows how to publish a more complicated payload JSON object.

You can also configure the LongpollManager by using
```go
golongpoll.CreateCustomManager(...)
```

HTTP Subscription Handler
-----
The ```LongpollManager``` has a field called ```SubscriptionHandler``` that you can attach as an ```http.HandleFunc```.

This http handler has the following URL query params as input.

* ```timeout``` number of seconds the server should wait until issuing a timeout response in the event there are no new events during the client's longpoll.  The default manager created via ```CreateManager``` has a max timeout of 180 seconds, but you can customize this by using ```CreateCustomManager```
* ```category``` the subscription category to subscribe to.  When you publish an event, you publish it on a specific category.
* ```since_time``` optional.  the number of milliseconds since epoch.  If not provided, defaults to current time.  This tells the longpoll server to only give you events that have occurred since this time.  

The response from this http handler is one of the following ```application/json``` responses:

* error response: ```{"error": "error message as to why request failed."}```
  * Perhaps you forgot to include a query param?  Or an invalid timeout? 
* timeout response: ```{"timeout": "no events before timeout"}``` 
  * This means no events occurred within the timeout window.  (given your ```since_time``` param) 
* event(s) response: ```{"events":[{"timestamp":1447218359843,"category":"farm","data":"Pig went 'Oink! Oink!'"}]}```
  * includes one or more event object.  If no events occurred, you should get a timeout instead. 

To receive a continuous stream of chronological events, you should keep hitting the http handler after each response, but with an updated ```since_time``` value equal to that of the last event's timestamp. 

You can see how to make these longpoll requests using jquery by viewing the example programs' code.

What is longpolling
=================
todo

Included examples
=================
There are two fully-functional example programs provided. 
Basic
-----
This program creates a default LongpollManager, shows how a goroutine can generate some events, and how to subscribe from a webpage.  See [basic.go](examples/basic/basic.go)

To run this example
```bash
go build examples/basic/basic.go
./basic
OR: ./basic.exe
```
Then visit:
```
http://127.0.0.1:8081/basic
```
And observe the events appearing every 0-5 seconds.

Advanced
-----
This program creates a custom LongpollManager, shows how an http handler can publish events, and how to subscribe from a webpage.  See [advanced.go](examples/advanced/advanced.go)

To run this example
```bash
go build examples/advanced/advanced.go
./advanced
OR: ./advanced.exe
```
Then visit:
```
http://127.0.0.1:8081/advanced
```
Try clicking around and notice the events showing up in the tables.  Try opening multiple windows as different users and observe events.  Toggle whether user's events are public or private.

More advanced use
=================
All of the below topics are demonstrated in the advanced example:
Events with JSON payloads
-----
Try passing any type that is convertable to JSON to ```Publish()```. If the type can be passed to encoding/json.Marshal(), it will work.

Wrapping subscriptions
-----
You can create your own http handler that calls ```LongpollManager.SubscriptionHandler``` to add your own layer of logic on top of the subscription handler.  Uses include: user authentication/access-control and limiting subscriptions to a known set of categories.

Publishing events via the web
-----
You can create a closure that captures the LongpollManager and attach it as an http handler function.  Within that function, simply call Publish(). 
