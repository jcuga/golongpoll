# Golongpoll HTTP API
There is an official [golang client](../client/) and [javascript client](../js-client/) library for golongpoll, but custom clients can be written to conform to the followign specs.

Note that the URLs for publish and subscribe are up to you--wherever you want to expose the two handlers.

```
manager, _ := golongpoll.StartLongpoll(golongpoll.Options{})

// serve event url
mux := http.NewServeMux()
mux.HandleFunc("/events", manager.SubscriptionHandler)
mux.HandleFunc("/publish", manager.PublishHandler)
server := &http.Server{Addr: "127.0.0.1:8081", Handler: mux}
```

One can also wrap these handlers with their own logic instead of serving them directly. See [Examples](../examples/README.md), namely the `authentication` one on how to wrap these with header-based auth checks.

## Subscribe
`LongpollManager.SubscriptionHandler`

### Request Query Parameters
The subscription handler use the following `HTTP GET` query parameters:

* `category` - required.  This is the subscription category to subscribe to.  This can be any string as lont as it's between 1-1024 in length. Currently only one category per request is supported.
* `timeout` - required.  This is how long the server should hold on to this request (in seconds) when there are no events to reply with.  Long polling involves the server waiting until events become available before responding, up until a timeout.  Values must be between 1 and whatever `Options.MaxLongpollTimeoutSeconds` is set to when creating the manager via `StartLongpoll(options)`.  Be aware that this should be set to less than any connection timeout setting in any proxy/webserver sitting in front of the application.  For example: `nginx` has a default timeout of 60 seconds, so one would want to use some value less than that to avoid a connection timeout before receiving a HTTP 200 with a API level longpoll timeout in the JSON response (see example responses below).
* `since_time` - optional. This is the time in UTC epoch milliseconds to get events since.  Without this, defaults to current time, meaning the client will only see new events.
* `last_id` - optional, but must include `since_time` if including this.  This is the last seen event's `id` value.  This is used in conjunction with `since_time` set to the last seen event's timestamp to pick up where the previously seen event left off.  This is necessary to avoid missing events in the scenario where clients get an event but a 2nd event was published within the same millisecond. When requesting events since the first event's timestamp and not updating the `last_id` param, the 2nd event with the same timestamp would not be returned. See examples below.

### Response JSON
The response is HTTP 200 with JSON that has one of three forms.

1. Events: `{"events":[{"timestamp":1616040198889,"category":"foobar","data":"coool beans dawg","id":"7c17587a-3437-4861-bef7-8f29905589e4"}]}`
    * `events` is an array of event objects.  If we get the `events` key in our json, then the array should be non-empty. (otherwise we'll get an error or timeout response instead). Each `event` object in the array has the following fields:
      * `timestamp` is the event's publish timestamp in epoch milliseconds.
      * `category` is the subscribed category
      * `id` is the event's publish id.  This can be used in the `last_id` query param along with `since_time` set to the `timestamp` value to ask for data starting with the event immediately following the one with this id/timestamp value.
      * `data` is arbitrary string/JSON data.  Whatever is published by `LongpollManager.Publish(interface{})`, converted to JSON.
2. Timeout Message: `{"timeout":"no events before timeout","timestamp":1616210844178}`
3. Error response: `{"error": "Invalid or missing 'timeout' arg.  Must be 1-110."}`

### Examples

#### Get New Events
In this example, `since_time` is not provided so we only ask for new events (published since now).  Note that the response is an array of event objects.
```
curl -v "http://127.0.0.1:8081/events?timeout=30&category=foobar"

< HTTP/1.1 200 OK
< Content-Type: application/json
<
* Connection #0 to host 127.0.0.1 left intact
{"events":[{"timestamp":1616040198889,"category":"foobar","data":"coool beans","id":"7c17587a-3437-4861-bef7-8f29905589e4"}]}
```

#### Get Events After Last Received Event
To continue the stream of events without gaps, set `since_time` and `last_id` to the last event's `timestamp` and `id` respectively.  The events are returned in chronological order, so the last event in the `events` array is the latest event received.

```
curl -v "http://127.0.0.1:8081/events?timeout=30&category=foobar&since_time=1616040198889&last_id=7c17587a-3437-4861-bef7-8f29905589e4"

< HTTP/1.1 200 OK
< Content-Type: application/json
<
* Connection #0 to host 127.0.0.1 left intact
{"events":[{"timestamp":1616040210001,"category":"foobar","data":"longpolling is cool","id":"bbf15731-ce12-40d8-8297-bfdef4a327d2"}, {"timestamp":1616040230445,"category":"foobar","data":"TOTALLY is coooooool","id":"875c4900-f031-421f-b56d-d154c2ea1522"}]}
```

#### Timeout Response
When there are no events within the client supplied `timeout` query parameter, the server responds with an API-level timeout response.  This is not to be confused with an HTTP 408/timeout code.  This is an HTTP 200 with a timeout in the JSON body.  In the event of receiving an API timeout response, clients can simply begin a new request for events with the same query parameters until events become available.  Remember to update the `since_time` and `last_id` when a new event finally does become available.

```
curl -v "http://127.0.0.1:8081/events?category=foobar&timeout=10&since_time=1615265400748"

< HTTP/1.1 200 OK
< Content-Type: application/json
<
* Connection #0 to host 127.0.0.1 left intact

{"timeout":"no events before timeout","timestamp":1616210844178}
```

#### Error Response
If there is an API-level error like an invalid GET query param, you'll receive an HTTP 200 with an error in the JSON body:

```
curl -v "http://127.0.0.1:8081/events?category=foobar&since_time=1615265400748"

< HTTP/1.1 200 OK
< Content-Type: application/json
<
* Connection #0 to host 127.0.0.1 left intact

{"error": "Invalid or missing 'timeout' arg.  Must be 1-110."}
```
The issue above is that the required parameter `timeout` was not included in the request.

## Publish
todo
