# golongpoll [![Build Status](https://travis-ci.org/jcuga/golongpoll.svg?branch=master)](https://travis-ci.org/jcuga/golongpoll) [![Coverage Status](https://coveralls.io/repos/jcuga/golongpoll/badge.svg?branch=master&service=github)](https://coveralls.io/github/jcuga/golongpoll?branch=master) [![GoDoc](https://godoc.org/github.com/jcuga/golongpoll?status.svg)](https://godoc.org/github.com/jcuga/golongpoll)
golang HTTP longpolling library, making web pub-sub easy!

Table of contents
=================
  * [Basic Usage](#basic-usage)
  * [What is longpolling?](#what-is-longpolling)
    * [Pros and cons](#pros-and-cons)
    * [Longpolling versus Websockets versus SSE](#longpolling-versus-websockets-versus-sse)
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

What is longpolling
=================
todo
Pros and cons
-----
todo

Longpolling versus Websockets versus SSE
-----
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
todo
Events with JSON payloads
-----
todo

Wrapping subscriptions
-----
todo

Publishing events via the web
-----
todo
