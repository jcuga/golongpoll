# golongpoll javascript client
If you want to use golongpoll subscribe and/or publish from the browser, you can use this javascript library.  Simply copy [this javascript](/js-client/client.js) as part of your app.  The [examples](/examples/README.md) in this repo serve the file along side the `LongpollManager` handlers.

This is fairly basic, old-style javascript.  If you want to write your own js client, see [HttpLongPollAPI.md](/HttpLongPollAPI.md).

## How to Use
See the function [golongpoll.newClient](/js-client/client.js) on how to use.  The options passed to this function mirror those used by the [golang client.ClientOptions](https://pkg.go.dev/github.com/jcuga/golongpoll/client#ClientOptions).  The function `newClient` returns the client on success, or `null` on failure due to bad config arguments. Once created via `newClient` the client will begin polling or the subscribed events on the `category` argument. Unlike the golang client, there is no need to call start to begin polling for events.

The main difference between this javascript client and the golang client are the following two options passed to `newClient`:

```js
    onEvent = function (event) {},
    onFailure = function (errorMsg) { return true; },
```

### onEvent
`onEvent` is a callback for when a new event is received for the subscribed cateogry.  The `event` arg is a json object of the form:

* `timestamp` - Timestamp is milliseconds since epoch to match javascrits Date.getTime(). This is the timestamp when the event was published.
* `category` - Category this event belongs to. Clients subscribe to a given category.
* `data` - Event data payload. This can be a string, json, or any json data type like a number, bool, etc.

Example with string payload: `{"timestamp": 1619395200021, "category": "some-cateogry", "data": "whatever-here"}`

Example with json payload: `{"timestamp": 1619395200021, "category": "some-cateogry", "data": {"foo": "bar", "goo": 123}}`

### onFailure
`onFailure` is an optional callback (can be omitted) that gets called when an error occurs while trying to long-poll for subscribed event data. The callback accepts an error string. If this callback returns `false`, then the client will stop polling for new events.  If returns `true` (default when omitted), the client will wait `client.reattemptWaitSeconds` before starting next poll request.

## To stop polling for events
```js
client.stop();
```

## Publish events
The client can publish in addition to receiving subscribed events.
```js
client.Publish("some-category", someData, onSuccess, onError);
```

For example:
```js
client.publish("some-category", data,
    function () {
        // optionally do somethign on success
    },
    function(status, resp) {
        // optionally do something with failure http status and response data
        alert("publish post request failed. status: " + status + ", resp: " + resp);
    });
};
```

Note: to prevent clients from publishing events, one can simply not serve `LongpollManager.PublishHandler`, or wrap the handler in a function that adds auth/validation checks. Also see the [authentication example](/examples/authentication/auth.go).

### Example
See [examples/microchat/microchat.go](/examples/microchat/microchat.go) for an example of how to use a js client to subscribe and publish data.