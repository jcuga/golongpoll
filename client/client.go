// Provides a client for longpoll servers serving events using
// LongpollManager.SubscriptionHandler.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jcuga/golongpoll"
	"log"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

// ClientOptions for configuring client behavior.
type ClientOptions struct {
	// Url is the longpoll server's subscription handler's URL to hit when
	// polling for events. This is a URL pointing to where
	// LongpollManager.SubscriptionHandler is being served or wrapped by another
	// handler.
	Url url.URL
	// Category is the subscription category to poll.
	Category string
	// PollTimeoutSeconds is the timeout arg the client sends to the longpoll
	// server, which dictates how long the server should keep the request idle
	// before issuing a timeout response when there hasn't been any event data
	// to respond with. Defaults to 45 seconds.
	// NOTE: if hitting a longpoll server behind a poxy/webserver, make sure
	// the PollTimeoutSeconds is less than that proxy's HTTP timeout setting!
	PollTimeoutSeconds uint
	// ReattemptWaitSeconds controls the amount of time the client waits to
	// reattempt polling the server after a failure response.
	// Defaults to 30 seconds.
	ReattemptWaitSeconds uint
	// SuccessWaitSeconds is how long to wait, in seconds, after receiving
	// events before requesting more events. Defaults to zero, no wait.
	SuccessWaitSeconds uint
	// HttpClient is an optional http.Client to use when polling the server.
	// Defaults to go's deafult http.Client.
	HttpClient *http.Client
	// BasicAuthUsername is an optional username to be used for basic HTTP
	// authentication when polling the server.
	BasicAuthUsername string
	// BasicAuthUsername is an optional password to be used for basic HTTP
	// authentication when polling the server.
	BasicAuthPassword string
	// Whether or not logging should be enabled
	LoggingEnabled bool
	// Optional callback for HTTP longpoll request failures.
	// The client will stop if the provided callback returns false.
	OnFailure func(err error) bool
	// ExtraHeaders has optional HTTP headers to include in lonpoll requests.
	// Useful if you wrap LongpollManager.SubscriptionHandler with additional
	// authentication or other logic that uses headers.
	ExtraHeaders []HeaderKeyValue
}

// HeaderKeyValue is a HTTP header key-value pair.
type HeaderKeyValue struct {
	Key   string
	Value string
}

// Client for polling a longpoll server.
type Client struct {
	// Flag that signals when the event polling goroutine should quit.
	// NOTE: using sys/atomic.AddUint64 on fields require them to be 64bit
	// aligned. The start of an allocated struct are guaranteed to be aligned,
	// so placing at start here fixes a cryptic panic I got after adding
	// a new field to this struct and then runID kept breaking.
	// see: https://stackoverflow.com/questions/28670232/atomic-addint64-causes-invalid-memory-address-or-nil-pointer-dereference
	runID uint64
	// options dictating client behavior
	options ClientOptions
	// Populated with received longpoll events.
	events chan *golongpoll.Event
	// flag whether or not Client.Start has been called--enforces use-only-once.
	started bool
	// flag whether or not Client.Stop has been called--enforces use-only-once.
	stopped bool
}

// NewClient creates a new Client configured with the given ClientOptions.
// Returns new client and nil error or nil client with a non-nil error.
func NewClient(opts ClientOptions) (*Client, error) {
	if len(opts.Category) == 0 {
		return nil, errors.New("opts.Category cannot be empty")
	}

	// Require both basic auth user/password, or neither
	if (len(opts.BasicAuthUsername) > 0 && len(opts.BasicAuthPassword) == 0) ||
		(len(opts.BasicAuthPassword) > 0 && len(opts.BasicAuthUsername) == 0) {
		return nil, errors.New("missing opts.BasicAuthUsername or opts.BasicAuthPassword value. Must have both or neither.")
	}

	// Set defaults if missing/zero/nil
	if opts.PollTimeoutSeconds == 0 {
		opts.PollTimeoutSeconds = 45
	}

	if opts.ReattemptWaitSeconds == 0 {
		opts.ReattemptWaitSeconds = 30
	}

	if opts.HttpClient == nil {
		opts.HttpClient = &http.Client{}
	}

	client := &Client{
		options: opts,
		events:  make(chan *golongpoll.Event, 100),
	}

	return client, nil
}

// Start begins Client's longpolling request-loop goroutine. The pollSince
// argument dictates the point in time we wish to see events since.
// Callers could pass time.Now() to only see new events, or a time in the
// past to start event consumption in the past. Use Client.Stop()
// to cease polling for new events. Read incoming events from the returned
// channel. The returned channel will be closed by client.Stop() or if
// ClientOptions.OnFailure returns false.
// Clients can only be started once and will panic otherwise.
// To resume long polling after stopping, create a new client via
// NewClient(ClientOptions).
func (c *Client) Start(pollSince time.Time) <-chan *golongpoll.Event {
	if c.stopped {
		panic("golongpoll Client cannot be started after already stopped.")
	}

	if c.started {
		panic("golongpoll Client cannot be started more than once.")
	}

	c.started = true
	u := c.options.Url

	atomic.AddUint64(&(c.runID), 1)
	currentRunID := atomic.LoadUint64(&(c.runID))

	go func(runID uint64, u url.URL, pollSince time.Time) {
		since := pollSince.Unix() * 1000 // HTTP API takes milliseconds
		lastID := ""                     // last received eventID, used to fix issue #19

		if c.options.LoggingEnabled {
			log.Printf("INFO - golongpoll.Client.Start - Start polling for events, %s\n", c.getCommonLogFields(since, lastID))
		}

		for {
			if c.doQuit(runID) {
				return
			}

			pollResp, err := c.fetchEvents(since, lastID)

			if err != nil {
				if c.options.LoggingEnabled {
					log.Printf("ERROR - golongpoll.Client.run - fetchEvents error: %v, %s\n", err, c.getCommonLogFields(since, lastID))
				}

				if c.options.OnFailure != nil {
					if !c.options.OnFailure(err) {
						if c.options.LoggingEnabled {
							log.Printf("WARN - golongpoll.Client.run - Stopping due to OnFailure callback, %s\n", c.getCommonLogFields(since, lastID))
						}

						close(c.events)
						return
					}
				}

				// Don't bother sleeping if it's time to quit
				if c.doQuit(runID) {
					return
				}

				if c.options.LoggingEnabled {
					log.Printf("INFO - golongpoll.Client.run - Reattempting in %d seconds, %s\n", c.options.ReattemptWaitSeconds,
						c.getCommonLogFields(since, lastID))
				}
				time.Sleep(time.Duration(c.options.ReattemptWaitSeconds) * time.Second)
				continue
			}

			// Stop sending events if time to quit. Checking now since this did
			// an HTTP request since last time we checked.
			if c.doQuit(runID) {
				return
			}

			eventsLen := len(pollResp.Events)
			if eventsLen > 0 {
				if c.options.LoggingEnabled {
					log.Printf("INFO - golongpoll.Client.run - Got %d event(s), %s\n", eventsLen, c.getCommonLogFields(since, lastID))
				}

				// Check if it's time to quit before sending new events to
				// channel if there's any chance we may have to wait
				// for channel space to become available.
				// That way, if clients call client.Stop() and don't read from
				// the events channel anymore, we don't get stuck in this
				// goroutine waiting for callers to read from the channel so we
				// can send remaining data to it.
				doQuitCheck := eventsLen >= cap(c.events)-len(c.events)

				for _, event := range pollResp.Events {
					since = event.Timestamp
					lastID = event.ID.String()

					if doQuitCheck && c.doQuit(runID) {
						return
					}

					c.events <- event
				}

				if c.options.SuccessWaitSeconds > 0 {
					time.Sleep(time.Duration(c.options.SuccessWaitSeconds) * time.Second)
				}
			} else {
				// Only push timestamp forward if its greater than the last we checked
				if pollResp.Timestamp > since {
					since = pollResp.Timestamp
				} else if pollResp.Timestamp == 0 {
					// here we have no events, no error message, and a zero/no timestamp
					// expect to get one of those.
					if c.options.LoggingEnabled {
						log.Printf("WARN - golongpoll.Client.run - got response without events, timeout, or error. %s\n",
							c.getCommonLogFields(since, lastID))
					}
				}
			}
		}
	}(currentRunID, u, pollSince)

	return c.events
}

// Stop signals to the client's event polling goroutine to stop.
// Upon receiving the stop signal, the client's goroutine will close the
// Events channel and stop running.  This function call does not block on the
// client's goroutine stopping.  Callers can wait for the Events channel
// returned by Client.Start() to be closed to know when that goroutine has
// finished executing. Note that said channel could also be closed due to
// ClientOptions.OnFailure returning false. Either way a closed channel
// indicates the client has stopped polling for event data.
// Clients can only be stopped once, multiple calls to this will panic.
// To resume long polling, create a new client via NewClient(ClientOptions).
func (c *Client) Stop() {
	if c.stopped {
		panic("golongpoll Client cannot be stopped more than once.")
	}

	if !c.started {
		panic("golongpoll Client cannot be stopped, never started.")
	}

	c.stopped = true

	if c.options.LoggingEnabled {
		log.Printf("INFO - golongpoll.Client.Stop - Sending stop signal. url: %s, category: \"%v\", timeout: %v,\n",
			c.options.Url.String(), c.options.Category, c.options.PollTimeoutSeconds)
	}

	// Changing the runID will have any previous goroutine ignore any events it may receive
	atomic.AddUint64(&(c.runID), 1)
}

// Checks if time to quit and if so closes event channel.
func (c *Client) doQuit(runID uint64) bool {
	newRunID := atomic.LoadUint64(&(c.runID))
	if newRunID != runID {
		if c.options.LoggingEnabled {
			log.Printf("INFO - golongpoll.Client.run - received stop signal, stopping. url: %s, category: \"%v\", timeout: %v,\n",
				c.options.Url.String(), c.options.Category, c.options.PollTimeoutSeconds)
		}
		close(c.events)
		return true
	}
	return false
}

func (c *Client) getCommonLogFields(since int64, lastID string) string {
	return fmt.Sprintf("url: %s, category: \"%v\", timeout: %v, since_time: %v, last_id: %v",
		c.options.Url.String(), c.options.Category, c.options.PollTimeoutSeconds, since, lastID)
}

// Relevant parts of HTTP longpolling API's resposne data
type pollResponse struct {
	Events []*golongpoll.Event `json:"events"`
	// Set for timeout responses
	Timestamp int64 `json:"timestamp"`
	// API error responses could have an informative error here. Empty on success.
	ErrorMessage string `json:"error"`
}

// Call the longpoll server to get the events since a specific timestamp
func (c Client) fetchEvents(since int64, lastID string) (*pollResponse, error) {
	u := c.options.Url
	if c.options.LoggingEnabled {
		log.Printf("INFO - golongpoll.Client.fetchEvents - Polling Events. %s\n", c.getCommonLogFields(since, lastID))
	}

	query := u.Query()
	query.Set("category", c.options.Category)
	query.Set("since_time", fmt.Sprintf("%d", since))
	if len(lastID) > 0 {
		query.Set("last_id", lastID)
	}
	query.Set("timeout", fmt.Sprintf("%d", c.options.PollTimeoutSeconds))
	u.RawQuery = query.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("HTTP request error: %v", err))
	}
	if len(c.options.BasicAuthUsername) > 0 && len(c.options.BasicAuthPassword) > 0 {
		req.SetBasicAuth(c.options.BasicAuthUsername, c.options.BasicAuthPassword)
	}

	// Optional headers to include in request--can be used for extra authentication
	// if the LongpollManager.SubscriptionHandler is wrapped with additional checks.
	for _, kvpair := range c.options.ExtraHeaders {
		req.Header.Add(kvpair.Key, kvpair.Value)
	}

	resp, err := c.options.HttpClient.Do(req)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Error connecting to %v, error: %s", u, err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(fmt.Sprintf("HTTP response code: %d", resp.StatusCode))
	}

	decoder := json.NewDecoder(resp.Body)

	var pollResp pollResponse
	err = decoder.Decode(&pollResp)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Error decoding poll response: %v", err))
	}

	if len(pollResp.ErrorMessage) > 0 {
		return nil, errors.New(fmt.Sprintf("Longpoll API error message: %s", pollResp.ErrorMessage))
	}

	return &pollResp, nil
}
