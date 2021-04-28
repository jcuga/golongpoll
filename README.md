# golongpoll [![Build Status](https://travis-ci.com/jcuga/golongpoll.svg?branch=master)](https://travis-ci.com/jcuga/golongpoll) [![codecov](https://codecov.io/gh/jcuga/golongpoll/branch/master/graph/badge.svg)](https://codecov.io/gh/jcuga/golongpoll)  [![GoDoc](https://godoc.org/github.com/jcuga/golongpoll?status.svg)](https://godoc.org/github.com/jcuga/golongpoll) [![Go Report Card](https://goreportcard.com/badge/jcuga/golongpoll)](https://goreportcard.com/report/jcuga/golongpoll)
Golang long polling library. Makes web pub-sub easy via HTTP long-poll servers and clients.  Supports golang v1.7 and up.

## Resources
* [go docs](https://pkg.go.dev/github.com/jcuga/golongpoll)
* [examples](/examples/README.md) - shows how to use the longpoll server and go/js clients.
* [go client](/client/README.md)
* [javascript client](/js-client/README.md)
* [longpoll http api](/HttpLongPollAPI.md) - in case you want to create your own client.

## QuickStart
To create a longpoll server:
```
import (
  "github.com/jcuga/golongpoll"
)

// This uses the default/empty options. See section on customizing, and Options go docs.
manager, err := golongpoll.StartLongpoll(golongpoll.Options{})

// Expose pub-sub. You could omit the publish handler if you don't want
// to allow clients to publish. For example, if clients only subscribe to data.
if err == nil {
  http.HandleFunc("/events", manager.SubscriptionHandler)
  http.HandleFunc("/publish", manager.PublishHandler)
  http.ListenAndServe("127.0.0.1:8101", nil)
} else {
  // handle error creating longpoll manager--typically this means a bad option.
}
```

The above snippet will create a [LongpollManager](https://pkg.go.dev/github.com/jcuga/golongpoll#LongpollManager) which has a `SubscriptionHandler` and a `PublishHandler` that can served via http (or using https).  When created, the manager spins up a separate goroutine that handles the plumbing for pub-sub.

`LongpollManager` also has a [Publish](https://pkg.go.dev/github.com/jcuga/golongpoll#LongpollManager.Publish) function that can be used to publish events. You can call `manager.Publish("some-category", "some data here")` OR expose the `manager.PublishHandler` and allow publishing of events via the [longpoll http api](/HttpLongPollAPI.md).  For publishing within the same program as the manager/server, calling `manager.Publish()` does not use networking--under the hood it uses the go channels that are part of the pub-sub plumbing.  You could also wrap the manager in an http handler closure that calls publish as desired.

See the [Examples](/examples/README.md) on how to use the golang and javascript clients as well as how to wrap the `manager.PublishHandler` or call `manager.Publish()` directly.

## How it Works
You can think of the longpoll manager as a goroutine that uses channels to service pub-sub requests.  The manager has a `map[string]eventBuffer` (actually a `map[string]*expiringBuffer`) that holds events per category as well as a data structure (another sort of map) for the subscription request book-keeping.  The `PublishHandler` and `SubscribeHandler` interact with the manager goroutine via channels.

The events are stored using in memory buffers that have a configured max number of events per category.  Optionally, the events can be automatically removed based on a time-to-live setting.  Since this is all in-memory, there is an optional add-on for auto-persisting and repopulating data from disk.  This allows events to persist across program restarts (not the default option/behavior!) One can also create their own custom add-on as well.  See the `Customizing` section.

One important limitation/design-decision to be aware of: the `SubscriptionHandler` supports subscribing to a *single cateogry*.  If you want to subscribe to more than one category, you must make more than one call to the subscription handler--or create multiple clients each with a different category.  Note however that clients are free to publish to more than one categor--to any category really, unless the manager's publish handler is not being served or there is wrapping handler logic that forbids this.  Whether or not this limitation is a big deal depends on how you are using categories. This decision reduces the internal complexity and is likely not to change any time soon. 

## Customizing
See [golongpoll.Options](https://pkg.go.dev/github.com/jcuga/golongpoll#Options) on how to configure the longpoll manager.  This includes:
* `MaxEventBufferSize` - for the max number of events per category, after which oldest-first is truncated. Defaults to 250.
* `EventTimeToLiveSeconds` - how long events exist in the buffer, defaults to forever (as long as `MaxEventBufferSize` isn't reached).
* `AddOn` - optional way to provide custom behavior. The only add-on at the moment is [FilePersistorAddOn](/fileaddon.go) (Usage [example](/examples/filepersist/filepersist.go)). See [AddOn interface](/addons.go) for creating your own custom add-on.

Remember, you don't have to expose `LongpollManager.SubscriptionHandler` and `PublishHandler` directly (or at all).  You can wrap them with your own http handler that adds additional logic or validation before invoking the inner handler.  See the [authentication example](/examples/authentication/auth.go) for how to require auth via header data before those handlers get called.  For publishing, you can also call `manager.Publish()` directly, or wrap the manager in a closure http handler and create a custom handler that can publish data.
