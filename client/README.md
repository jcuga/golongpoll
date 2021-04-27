# golongpoll golang client
If you want go code to interact with the longpoll server via HTTP (i.e. hit `LongpollManager.SubscriptionHandler` and/or `LongpollManager.PublishHandler`), then you can use this go client.

See [client go docs](https://pkg.go.dev/github.com/jcuga/golongpoll/client) for full options and details.

If you want to write your own client, see [HttpLongPollAPI.md](/HttpLongPollAPI.md).

## Quickstart

### Creating a Client
Use `NewClient` with `ClientOptions`.
```
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

```
for event := range c.Start(time.Now()) {
    // do something with each event
}
```

Note these examples use a for-range loop over the channel, you could do channel reads directly, or use with `select` as well.

Since `golongpoll.Event.Data` is of type `interface{}`, you have to convert to the expected type:
```
for event := range c.Start(time.Now()) {
    // assuming all events are strings
    msg := event.Data.(string)
    // do something with msg
}
```

If there can be multiple types of data, you can use a type switch:
```
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
```
c.Stop()
```

### Publish events
The client itself can optionally publish data as well as subscribe to events.  Simply call `Client.Publish(category string, data interface{})`

```
err: = c.Publish("news", "I am using golongpoll")
```

Publish returns a non-nil error on failure.

Note: to prevent clients from publishing events, one can simply not serve `LongpollManager.PublishHandler`, or wrap the handler in a function that adds auth/validation checks.

### Configuring basic auth or custom headers
You can use `ClientOptions.BasicAuthUsername`/`ClientOptions.BasicAuthPassword` or `ClientOptions.ExtraHeaders` which will cause the client to include basic auth or arbitrary HTTP headers in its publish and subscribe http requests.

See [ClientOptions docs](https://pkg.go.dev/github.com/jcuga/golongpoll/client#ClientOptions) for more details.

Also see the [authentication example](/examples/authentication/auth.go).