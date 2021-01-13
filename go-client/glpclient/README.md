# golongpoll client [![GoDoc](https://godoc.org/github.com/jcuga/golongpoll/go-client?status.svg)](https://godoc.org/github.com/jcuga/golongpoll/go-client)

# Basic usage

You will first need to create the client configuring it with the URL of the golongpoll server. Each client must be started after being instantiated.

When an event is received, it will be sent in the EventsChan of the client.

```
import (
  "fmt" 

  "github.com/jcuga/golongpoll/go-client"
)

u, _ := url.Parse("http://127.0.0.1/events")

// Second argument is the category on which we want to receive events
c := glpclient.NewClient(u, "news")
c.Start()

for {
  select {
  case e := <-c.EventsChan:
    fmt.Printf("Got event %s", string(e.Data))
  }
}

c.Stop()
```

# Options

 * `Timeout`: Defines the timeout used when calling the longpoll server
 * `Reattempt`: Defines the amount of time the client waits after an unsuccessful connection.
 * `BasicAuthUsername`: When defined along with the BasicAuthPassword, it will use the credentials in basic HTTP authentication
 * `BasicAuthPassword`: When defined along with the BasicAuthUsername, it will use the credentials in basic HTTP authentication

The client's HTTP client can be overiden. Example:

```
u, _ := url.Parse("http://127.0.0.1/events")
c := glpclient.NewClient(u, "news")

tr := &http.Transport{
	MaxIdleConns:       10,
	IdleConnTimeout:    30 * time.Second,
	DisableCompression: true,
}
c.HttpClient= &http.Client{Transport: tr}

...
```

# Receiving JSON

The `Data` attribute of the received events is a *json.RawMessage that can be used with json.Unmarshal. Example:

```
e := <-c.EventsChan
var data map[string]string
err := json.Unmarshal(e.Data, &data)
if err != nil {
  t.Error("Error while decoding event from channel")
}
```

