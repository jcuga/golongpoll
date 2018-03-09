package glpclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

const (
	DEFAULT_TIMEOUT   = 30
	DEFAULT_REATTEMPT = 30
)

type PollResponse struct {
	Events    []PollEvent `json:"events"`
	Timestamp int64       `json:"timestamp"`
}

// Taken from https://github.com/jcuga/golongpoll/blob/master/events.go
type PollEvent struct {
	// Timestamp is milliseconds since epoch to match javascrits Date.getTime()
	Timestamp int64  `json:"timestamp"`
	Category  string `json:"category"`
	// NOTE: Data can be anything that is able to passed to json.Marshal()
	Data json.RawMessage `json:"data"`
}

type Client struct {
	url      *url.URL
	category string
	// Timeout controls the timeout in all the requests, can be changed after instantiating the client
	Timeout uint64
	// Reattempt controls the amount of time the client waits to reconnect to the server after a failure
	Reattempt uint64
	// Will get all the events data
	EventsChan chan json.RawMessage
	// Flag that tracks the current run ID
	runID uint64
	// The HTTP client to perform the requests, any changes on this should be done prior to starting the client the first time
	HttpClient *http.Client
}

// Instantiate a new client to connect to a given URL and send the events into a channel
// The URL shouldn't contain any GET parameters although its fine if it contains some but category, since_time and timeout will be overriten
// stubChanData must either be an empty structure of the events data or a map[string]interface{} if the events do not follow a specific structure
func NewClient(url *url.URL, category string) *Client {
	return &Client{
		url:        url,
		category:   category,
		Timeout:    DEFAULT_TIMEOUT,
		Reattempt:  DEFAULT_REATTEMPT,
		EventsChan: make(chan json.RawMessage),
		HttpClient: &http.Client{},
	}
}

func (c *Client) Start() {
	u := c.url
	fmt.Println("Now observing changes on", u.String())

	atomic.AddUint64(&(c.runID), 1)
	currentRunID := atomic.LoadUint64(&(c.runID))

	go func(runID uint64, u *url.URL) {
		since := time.Now().Unix() * 1000
		for {
			pr, err := c.fetchEvents(since)

			if err != nil {
				fmt.Println(err)
				fmt.Printf("Reattempting to connect to %s in %d seconds \n", u.String(), c.Reattempt)
				time.Sleep(time.Duration(c.Reattempt) * time.Second)
				continue
			}

			// We check that its still the same runID as when this goroutine was started
			clientRunID := atomic.LoadUint64(&(c.runID))
			if clientRunID != runID {
				fmt.Printf("Client on URL %s has been stopped, not sending events \n", u.String())
				return
			}

			if len(pr.Events) > 0 {
				fmt.Println("Got", len(pr.Events), "event(s) from URL", u.String())
				for _, event := range pr.Events {
					since = event.Timestamp
					c.EventsChan <- event.Data
				}
			} else {
				// Only push timestamp forward if its greater than the last we checked
				if pr.Timestamp > since {
					since = pr.Timestamp
				}
			}
		}
	}(currentRunID, u)
}

func (c *Client) Stop() {
	// Changing the runID will have any previous goroutine
	atomic.AddUint64(&(c.runID), 1)
}

func (c Client) fetchEvents(since int64) (PollResponse, error) {
	u := c.url
	fmt.Println("Checking for changes events since", since, "on URL", u.String())

	query := u.Query()
	query.Set("category", c.category)
	query.Set("since_time", fmt.Sprintf("%d", since))
	query.Set("timeout", fmt.Sprintf("%d", c.Timeout))
	u.RawQuery = query.Encode()

	resp, err := c.HttpClient.Get(u.String())
	if err != nil {
		msg := fmt.Sprintf("Error while connecting to %s to observe changes. Error was: %s", u, err)
		return PollResponse{}, errors.New(msg)
	}

	decoder := json.NewDecoder(resp.Body)
	defer resp.Body.Close()

	var pr PollResponse
	err = decoder.Decode(&pr)
	if err != nil {
		fmt.Sprintf("Error while decoding poll response: %s", err)
		return PollResponse{}, err
	}

	return pr, nil
}
