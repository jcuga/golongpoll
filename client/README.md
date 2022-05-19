# golongpoll golang client
If you want go code to interact with the longpoll server via HTTP (i.e. hit `LongpollManager.SubscriptionHandler` and/or `LongpollManager.PublishHandler`), then you can use this go client.

See [client go docs](https://pkg.go.dev/github.com/jcuga/golongpoll/client) for full options and details.

If you want to write your own client, see [HttpLongPollAPI.md](/HttpLongPollAPI.md).

## Quickstart

### Creating a Client
Use `NewClient` with `ClientOptions`.
```go
import (
    "net/url"
    "github.com/jcuga/golongpoll/client"
)

func main() {
    subUrl, _ := url.Parse("http://127.0.0.1:8080/path/to/SubscriptionHandler")
    pubUrl, _ := url.Parse("http://127.0.0.1:8080/path/to/PublishHandler")

    c, err := client.NewClient(client.ClientOptions{
        SubscribeUrl:   *subUrl,
        PublishUrl:     *pubUrl,
        Category:       "some-category",
        // See ClientOptions go docs for more options
    })
    if err != nil {
        fmt.Println("FAILED TO CREATE LONGPOLL CLIENT: ", err)
        return
    }

    // use client...
}
```
This will create the client but not yet start its subscription goroutine that polls for new events on the category `ClientOptions.Category`.  To start polling, call `Client.Start(pollSince time.Time)`

### Starting event subscription poll goroutine
`Client.Start(pollSince time.Time)` starts http longpolling for new events in its own goroutine and returns a channel of `golongpoll.Event` that will convey any new events since the `pollSince` argument.  This example only asks for new events (since now), but you could specify a time in the past.

```go
for event := range c.Start(time.Now()) {
    // do something with each event
}
```

Note these examples use a for-range loop over the channel, you could do channel reads directly, or use with `select` as well.

Since `golongpoll.Event.Data` is of type `interface{}`, you have to convert to the expected type:
```go
for event := range c.Start(time.Now()) {
    // assuming all events are strings
    msg := event.Data.(string)
    // do something with msg
}
```

If there can be multiple types of data, you can use a type switch:
```go
for event := range c.Start(time.Now()) {
    switch v := i.(type) {
    case string:
        // do something with string
    case SomeCustomType1:
        // do something with SomeCustomType1
    case SomeCustomType2:
        // do something with SomeCustomType2
    default:
        // handle unexected type
    }
}
```

### Stop polling for subscribed events
To stop the client's longpoll subscription goroutine which will also close the channel returned by `Client.Start`, call `Client.Stop()`
```go
c.Stop()
```

### Publish events
The client itself can optionally publish data as well as subscribe to events.  Simply call `Client.Publish(category string, data interface{})`

```go
err := c.Publish("news", "I am using golongpoll")
```

Publish returns a non-nil error on failure.

Note: to prevent clients from publishing events, one can simply not serve `LongpollManager.PublishHandler`, or wrap the handler in a function that adds auth/validation checks.

### Handling Subscription Failures

Calls to `client.Publish(category, data)` return an `error`.  What about failures when hitting the subscription handler?

Subscription failures can be handled by providing a `ClientOptions.OnFailure` function of type `func(err error) bool`.  This function is invoked when an error occurs attempting a longpoll subscription.  If the callback returns `false` the client stops it's subscription.  For example, to only allow a certain number of failed subscriptions, we use an `OnFailure` handler with a retry counter:

```go
// Use a closure to capture the retry counter:
func getOnFailureHandler() func(error) bool {
	retries := 0
	return func(err error) bool {
		retries += 1
		if retries <= 3 {
			log.Printf("OnFailure - retry count: %d - keep trying\n", retries)
			return true
		}

		log.Printf("OnFailure - retry count: %d - stop trying\n", retries)
		return false
	}
}

// ...

// Use the failure handler:
opts := ClientOptions{
    // ...
    OnFailure:  getOnFailureHandler(),
}
c, err := NewClient(opts)

```

Remember, one can also enable logging via `ClientOptions.LoggingEnabled` to see failure logs.


### Configuring basic auth or custom headers
You can use `ClientOptions.BasicAuthUsername`/`ClientOptions.BasicAuthPassword` or `ClientOptions.ExtraHeaders` which will cause the client to include basic auth or arbitrary HTTP headers in its publish and subscribe http requests.

See [ClientOptions docs](https://pkg.go.dev/github.com/jcuga/golongpoll/client#ClientOptions) for more details.

Also see the [authentication example](/examples/authentication/auth.go).
