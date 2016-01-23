package golongpoll

import (
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/nu7hatch/gouuid"
)

const (
	// Magic number to represent 'Forever' in
	// LongpollOptions.EventTimeToLiveSeconds
	FOREVER = -1001
)

// LongpollManager provides an interface to interact with the internal
// longpolling pup-sub goroutine.
//
// This allows you to publish events via Publish()
// If for some reason you want to stop the pub-sub goroutine at any time
// you can call Shutdown() and all longpolling will be disabled.  Note that the
// pub-sub goroutine will exit on program exit, so for most simple programs,
// calling Shutdown() is not necessary.
//
// A LongpollManager is created with each subscriptionManager that gets created
// by CreateLongpollManager()
// This interface also exposes the HTTP handler that client code can attach to
// a URL like so:
//		mux := http.NewServeMux()
//		mux.HandleFunc("/custom/path/to/events", manager.SubscriptionHandler)
// Note, this http handler can be wrapped by another function (try capturing the
// manager in a closure) to add additional validation, access control, or other
// functionality on top of the subscription handler.
//
// You can have another http handler publish events by capturing the manager in
// a closure and calling manager.Publish() from inside a http handler.  See the
// advanced example (examples/advanced/advanced.go)
//
// If for some reason you want multiple goroutines handling different pub-sub
// channels, you can simply create multiple LongpollManagers.
type LongpollManager struct {
	subManager          *subscriptionManager
	eventsIn            chan<- lpEvent
	stopSignal          chan<- bool
	SubscriptionHandler func(w http.ResponseWriter, r *http.Request)
}

// Publish an event for a given subscription category.  This event can have any
// arbitrary data that is convert-able to JSON via the standard's json.Marshal()
// the category param must be a non-empty string no longer than 1024,
// otherwise you get an error.
func (m *LongpollManager) Publish(category string, data interface{}) error {
	if len(category) == 0 {
		return errors.New("empty category")
	}
	if len(category) > 1024 {
		return errors.New("category cannot be longer than 1024")
	}
	m.eventsIn <- lpEvent{timeToEpochMilliseconds(time.Now()), category, data}
	return nil
}

// Shutdown allows the internal goroutine that handles the longpull pup-sub
// to be stopped.  This may be useful if you want to turn off longpolling
// without terminating your program.  After a shutdown, you can't call Publish()
// or get any new results from the SubscriptionHandler.  Multiple calls to
// this function on the same manager will result in a panic.
func (m *LongpollManager) Shutdown() {
	close(m.stopSignal)
}

// Options for LongpollManager
type Options struct {
	// Whether or not to print logs
	LoggingEnabled bool

	// Max client timeout seconds  to be accepted by the SubscriptionHandler
	// (The 'timeout' HTTP query param)
	MaxLongpollTimeoutSeconds int

	// How many events to buffer per Category before discarding oldest events
	// due to buffer being exhausted.  Larger buffer sizes are useful for
	// high volumes of events in the same categories.  But for low-volumes,
	// smaller buffer sizes are more efficient.
	MaxEventBufferSize int

	// How long (seconds) events remain in their respective category's
	// EventBuffer before being deleted. (Deletes old events even if buffer has
	// the room.  Useful to save space if you don't need old events)
	// You can use a large MaxEventBufferSize to handle spikes in event volumes
	// in a single category but have a relatively short EventTimeToLiveSeconds
	// value to save space in the more common low-volume case.
	// If you want events to remain in the buffer as long as there is room per
	// MaxEventBufferSize, then use the magic value longpoll.Forever here.
	EventTimeToLiveSeconds int

	// Whether or not to delete an event as soon as it is retrieved via an
	// HTTP longpoll.  Saves on space if clients only interested in seing an
	// event once and never again.  But be careful if two clients share the
	// same subscription category, enabling this means only one of the
	// clients will see any given event.
	DeleteEventAfterFirstRetrieval bool
}

// TODO: function comments
func StartDefaultLongpoll() (*LongpollManager, error) {
	return StartLongpoll(Options{
		LoggingEnabled:            true,
		MaxLongpollTimeoutSeconds: 180,
		MaxEventBufferSize:        250,
		// TODO: appropriate default event TTL?
		EventTimeToLiveSeconds:         60 * 60 * 24, // events retained for 24 hours
		DeleteEventAfterFirstRetrieval: false,
	})
}

// TODO: function comments
func StartLongpoll(opts Options) (*LongpollManager, error) {
	if opts.MaxEventBufferSize < 1 {
		return nil, errors.New("Options.MaxEventBufferSize must be at least 1")
	}
	if opts.MaxLongpollTimeoutSeconds < 1 {
		return nil, errors.New("Options.MaxLongpollTimeoutSeconds must be at least 1")
	}
	// TTL must be positive, non-zero, or the magic FOREVER value (a negative const)
	if opts.EventTimeToLiveSeconds < 1 && opts.EventTimeToLiveSeconds != FOREVER {
		return nil, errors.New("options.EventTimeToLiveSeconds must be at least 1 or the constant longpoll.FOREVER")
	}
	channelSize := 100
	clientRequestChan := make(chan clientSubscription, channelSize)
	clientTimeoutChan := make(chan clientCategoryPair, channelSize)
	events := make(chan lpEvent, channelSize)
	// never has a send, only a close, so no larger capacity needed:
	quit := make(chan bool, 1)
	subManager := subscriptionManager{
		clientSubscriptions:            clientRequestChan,
		ClientTimeouts:                 clientTimeoutChan,
		Events:                         events,
		ClientSubChannels:              make(map[string]map[uuid.UUID]chan<- []lpEvent),
		SubEventBuffer:                 make(map[string]*eventBuffer),
		Quit:                           quit,
		LoggingEnabled:                 opts.LoggingEnabled,
		MaxEventBufferSize:             opts.MaxEventBufferSize,
		EventTimeToLiveSeconds:         opts.EventTimeToLiveSeconds,
		DeleteEventAfterFirstRetrieval: opts.DeleteEventAfterFirstRetrieval,
	}
	// Start subscription manager
	go subManager.run()
	LongpollManager := LongpollManager{
		&subManager,
		events,
		quit,
		getLongPollSubscriptionHandler(opts.MaxLongpollTimeoutSeconds, clientRequestChan, clientTimeoutChan, opts.LoggingEnabled),
	}
	return &LongpollManager, nil
}

/// DEPRECATED.  Use StartDefaultLongpoll or StartCustomLongpoll instead
// Creates a basic LongpollManager and pub-sub goroutine connected via channels
// that are exposed via LongpollManager's Publish() function and
// SubscriptionHandler field which can get used as an http handler function.
// This basic LongpollManager's default options should cover most client's
// longpolling needs without having to worry about details.
// This uses the following options:
// maxTimeoutSeconds of 180.
// eventBufferSize size 250 (buffers this many most recent events per
// subscription category) and loggingEnabled as true.
//
// Creates a basic LongpollManager with the default settings.
// This manager is an interface on top of an internal goroutine that uses
// channels to support event pub-sub.
//
// The default settings should handle most use cases, unless you expect to have
// a large number of events on a given subscription category within a short
// amount of time.  Perhaps having more than 50 events a second on a single
// subscription category would be the time to start considering tweaking the
// settings via CreateCustomManager.
//
// The default settings are:  a max longpoll timeout window of 180 seconds,
// the per-subscription-category event buffer size of 250 events,
// and logging enabled.
func CreateManager() (*LongpollManager, error) {
	return StartLongpoll(Options{
		LoggingEnabled:            true,
		MaxLongpollTimeoutSeconds: 180,
		MaxEventBufferSize:        250,
		// Original behavior maintained, events are never deleted unless they're
		// pushed out by max buffer size exceeded.
		EventTimeToLiveSeconds:         FOREVER,
		DeleteEventAfterFirstRetrieval: false,
	})
}

/// DEPRECATED.  Use StartCustomLongpoll instead.
// Creates a custom LongpollManager and pub-sub goroutine connected via channels
// that are exposed via LongpollManager's Publish() function and
// SubscriptionHandler field which can get used as an http handler function.
//
// The options are as follows.
// maxTimeoutSeconds, the max number of seconds a longpoll web request can wait
// before returning a timeout response in the event of no events on that
// subscription category.
//
// eventBufferSize, the number of events that get buffered per subscription
// category before we start throwing the oldest events out.  These buffers are
// used to support longpolling for events in the past, and giving a better
// guarantee that any events that occurred between client's longpoll requests
// can still be seen by their next request.
//
// loggingEnabled, whether or not log statements are printed out.
func CreateCustomManager(maxTimeoutSeconds, eventBufferSize int, loggingEnabled bool) (*LongpollManager, error) {
	return StartLongpoll(Options{
		LoggingEnabled:            loggingEnabled,
		MaxLongpollTimeoutSeconds: maxTimeoutSeconds,
		MaxEventBufferSize:        eventBufferSize,
		// Original behavior maintained, events are never deleted unless they're
		// pushed out by max buffer size exceeded.
		EventTimeToLiveSeconds:         FOREVER,
		DeleteEventAfterFirstRetrieval: false,
	})
}

type clientSubscription struct {
	clientCategoryPair
	// used to ensure no events skipped between long polls:
	LastEventTime time.Time
	// we channel arrays of events since we need to send everything a client
	// cares about in a single channel send.  This makes channel receives a
	// one shot deal.
	Events chan []lpEvent
}

func newclientSubscription(subscriptionCategory string, lastEventTime time.Time) (*clientSubscription, error) {
	u, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}
	subscription := clientSubscription{
		clientCategoryPair{*u, subscriptionCategory},
		lastEventTime,
		make(chan []lpEvent, 1),
	}
	return &subscription, nil
}

// get web handler that has closure around sub chanel and clientTimeout channnel
func getLongPollSubscriptionHandler(maxTimeoutSeconds int, subscriptionRequests chan clientSubscription,
	clientTimeouts chan<- clientCategoryPair, loggingEnabled bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		timeout, err := strconv.Atoi(r.URL.Query().Get("timeout"))
		if loggingEnabled {
			log.Println("Handling HTTP request at ", r.URL)
		}
		// We are going to return json no matter what:
		w.Header().Set("Content-Type", "application/json")
		// Don't cache response:
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate") // HTTP 1.1.
		w.Header().Set("Pragma", "no-cache")                                   // HTTP 1.0.
		w.Header().Set("Expires", "0")                                         // Proxies.
		if err != nil || timeout > maxTimeoutSeconds || timeout < 1 {
			if loggingEnabled {
				log.Printf("Error: Invalid timeout param.  Must be 1-%d. Got: %q.\n",
					maxTimeoutSeconds, r.URL.Query().Get("timeout"))
			}
			io.WriteString(w, fmt.Sprintf("{\"error\": \"Invalid timeout arg.  Must be 1-%d.\"}", maxTimeoutSeconds))
			return
		}
		category := r.URL.Query().Get("category")
		if len(category) == 0 || len(category) > 1024 {
			if loggingEnabled {
				log.Printf("Error: Invalid subscription category, must be 1-1024 characters long.\n")
			}
			io.WriteString(w, "{\"error\": \"Invalid subscription category, must be 1-1024 characters long.\"}")
			return
		}
		// Default to only looking for current events
		lastEventTime := time.Now()
		// since_time is string of milliseconds since epoch
		lastEventTimeParam := r.URL.Query().Get("since_time")
		if len(lastEventTimeParam) > 0 {
			// Client is requesting any event from given timestamp
			// parse time
			var parseError error
			lastEventTime, parseError = millisecondStringToTime(lastEventTimeParam)
			if parseError != nil {
				if loggingEnabled {
					log.Printf("Error parsing last_event_time arg. Parm Value: %s, Error: %s.\n",
						lastEventTimeParam, err)
				}
				io.WriteString(w, "{\"error\": \"Invalid last_event_time arg.\"}")
				return
			}
		}
		subscription, err := newclientSubscription(category, lastEventTime)
		if err != nil {
			if loggingEnabled {
				log.Printf("Error creating new Subscription: %s.\n", err)
			}
			io.WriteString(w, "{\"error\": \"Error creating new Subscription.\"}")
			return
		}
		subscriptionRequests <- *subscription
		// Listens for connection close and un-register subscription in the
		// event that a client crashes or the connection goes down.  We don't
		// need to wait around to fulfill a subscription if no one is going to
		// receive it
		disconnectNotify := w.(http.CloseNotifier).CloseNotify()
		select {
		case <-time.After(time.Duration(timeout) * time.Second):
			// Lets the subscription manager know it can discard this request's
			// channel.
			clientTimeouts <- subscription.clientCategoryPair
			timeout_resp := makeTimeoutResponse(time.Now())
			if jsonData, err := json.Marshal(timeout_resp); err == nil {
				io.WriteString(w, string(jsonData))
			} else {
				io.WriteString(w, "{\"error\": \"json marshaller failed\"}")
			}
		case events := <-subscription.Events:
			// Consume event.  Subscription manager will automatically discard
			// this client's channel upon sending event
			// NOTE: event is actually []Event
			if jsonData, err := json.Marshal(eventResponse{&events}); err == nil {
				io.WriteString(w, string(jsonData))
			} else {
				io.WriteString(w, "{\"error\": \"json marshaller failed\"}")
			}
		case <-disconnectNotify:
			// Client connection closed before any events occurred and before
			// the timeout was exceeded.  Tell manager to forget about this
			// client.
			clientTimeouts <- subscription.clientCategoryPair
		}
	}
}

// eventResponse is the json response that carries longpoll events.
type timeoutResponse struct {
	TimeoutMessage string `json:"timeout"`
	Timestamp      int64  `json:"timestamp"`
}

func makeTimeoutResponse(t time.Time) *timeoutResponse {
	return &timeoutResponse{"no events before timeout",
		timeToEpochMilliseconds(t)}
}

type clientCategoryPair struct {
	ClientUUID           uuid.UUID
	SubscriptionCategory string
}

type subscriptionManager struct {
	clientSubscriptions chan clientSubscription
	ClientTimeouts      <-chan clientCategoryPair
	Events              <-chan lpEvent
	// Contains all client sub channels grouped first by sub id then by
	// client uuid
	ClientSubChannels map[string]map[uuid.UUID]chan<- []lpEvent
	SubEventBuffer    map[string]*eventBuffer
	// channel to inform manager to stop running
	Quit           <-chan bool
	LoggingEnabled bool
	// How big the buffers are (1-n) before events are discareded FIFO
	MaxEventBufferSize int
	// How long events can stay in their eventBuffer
	EventTimeToLiveSeconds int
	// Whether or not to delete an event after the first time it is served via
	// HTTP
	DeleteEventAfterFirstRetrieval bool
}

// This should be fired off in its own goroutine
func (sm *subscriptionManager) run() error {
	if sm.LoggingEnabled {
		log.Printf("SubscriptionManager: Starting run.")
	}
	for {
		select {
		case newClient := <-sm.clientSubscriptions:
			// before storing client sub request, see if we already have data in
			// the corresponding event buffer that we can use to fufil request
			// without storing it
			doQueueRequest := true
			if buf, found := sm.SubEventBuffer[newClient.SubscriptionCategory]; found {
				// First clean up anything that expired
				sm.checkExpiredEvents(buf)
				// We have a buffer for this sub category, check for buffered events
				if events, err := buf.GetEventsSince(newClient.LastEventTime, sm.DeleteEventAfterFirstRetrieval); err == nil && len(events) > 0 {
					doQueueRequest = false
					if sm.LoggingEnabled {
						log.Printf("SubscriptionManager: Skip adding client, sending %d events. (Category: %q Client: %s)",
							len(events), newClient.SubscriptionCategory, newClient.ClientUUID.String())
					}
					// Send client buffered events.  Client will immediately consume
					// and end long poll request, so no need to have manager store
					newClient.Events <- events
				} else if err != nil {
					if sm.LoggingEnabled {
						log.Printf("Error getting events from event buffer: %s.", err)
					}
				}
				// Buffer Could have been emptied due to the  DeleteEventAfterFirstRetrieval
				// or EventTimeToLiveSeconds options.
				sm.deleteBufferIfEmpty(buf, newClient.SubscriptionCategory)
			}
			if doQueueRequest {
				// Couldn't find any immediate events, store for future:
				categoryClients, found := sm.ClientSubChannels[newClient.SubscriptionCategory]
				if !found {
					// first request for this sub category, add client chan map entry
					categoryClients = make(map[uuid.UUID]chan<- []lpEvent)
					sm.ClientSubChannels[newClient.SubscriptionCategory] = categoryClients
				}
				if sm.LoggingEnabled {
					log.Printf("SubscriptionManager: Adding Client (Category: %q Client: %s)",
						newClient.SubscriptionCategory, newClient.ClientUUID.String())
				}
				categoryClients[newClient.ClientUUID] = newClient.Events
			}
		case disconnect := <-sm.ClientTimeouts:
			if subCategoryClients, found := sm.ClientSubChannels[disconnect.SubscriptionCategory]; found {
				// NOTE:  The delete function doesn't return anything, and will do nothing if the
				// specified key doesn't exist.
				delete(subCategoryClients, disconnect.ClientUUID)
				if sm.LoggingEnabled {
					log.Printf("SubscriptionManager: Removing Client (Category: %q Client: %s)",
						disconnect.SubscriptionCategory, disconnect.ClientUUID.String())
				}
			} else {
				// Sub category entry not found.  Weird.  Log this!
				if sm.LoggingEnabled {
					log.Printf("Warning: cleint disconnect for non-existing subscription category: %q",
						disconnect.SubscriptionCategory)
				}
			}
		case event := <-sm.Events:
			doBufferEvents := true
			// Send event to any listening client's channels
			if clients, found := sm.ClientSubChannels[event.Category]; found && len(clients) > 0 {
				if sm.DeleteEventAfterFirstRetrieval {
					// Configured to delete events from buffer after first retrieval by clients.
					// Now that we already have clients receiving, don't bother
					// buffering the event.
					// NOTE: this is wrapped by condition that clients are found
					// if no clients are found, we queue event even when we have
					// the delete-on-first option set because no-one has recieved
					// the event yet.
					doBufferEvents = false
				}
				if sm.LoggingEnabled {
					log.Printf("SubscriptionManager: forwarding event to %d clients. (event: %v)", len(clients), event)
				}
				for clientUUID, clientChan := range clients {
					if sm.LoggingEnabled {
						log.Printf("SubscriptionManager: sending event to client: %s", clientUUID.String())
					}
					clientChan <- []lpEvent{event}
					// boot this client subscription since we found events
					// In longpolling, subscriptions only last until there is
					// data (happening here) or a timeout (handled by the
					//disconnect case above)
					// NOTE: it IS safe to delete map entries as you iterate
					// SEE: http://stackoverflow.com/questions/23229975/is-it-safe-to-remove-selected-keys-from-golang-map-within-a-range-loop
					if sm.LoggingEnabled {
						log.Printf("SubscriptionManager: Removing client after event send: %s", clientUUID.String())
					}
					delete(clients, clientUUID)
					// TODO: remove entry from sm.ClientSubChannels if the sm.ClientSubChannels entry for that category is empty?
					// TODO: otherwise won't this container always be as big as the set of category ids ever called?
					// TODO: can this be made even simpler by deleting the sm.ClientSubChannels entry alltogether instead of one
					// client at a time and then checking if empty.  because wouldn't it always be empty if we remove them???
					// TODO: if so, move this to outside this loop (after loop) and delete entire category entry for client subscriptions
				}
			}

			bufFound := false
			var buf *eventBuffer = nil
			if doBufferEvents {
				// Add event buffer for this event's subscription category if doesn't exist
				buf, bufFound = sm.SubEventBuffer[event.Category]
				if !bufFound {
					buf = &eventBuffer{
						list.New(),
						sm.MaxEventBufferSize,
					}
					sm.SubEventBuffer[event.Category] = buf
				}
				if sm.LoggingEnabled {
					log.Printf("SubscriptionManager: queue event: %v.", event)
				}
				// queue event in event buffer
				if qErr := buf.QueueEvent(&event); qErr != nil {
					if sm.LoggingEnabled {
						log.Printf("Error: failed to queue event.  err: %s", qErr)
					}
				}
			} else {
				if sm.LoggingEnabled {
					log.Printf("SubscriptionManager: DeleteEventAfterFirstRetrieval: SKIP queue event: %v.", event)
				}
			}
			// Perform Event TTL check and empty buffer cleanup:
			if bufFound && buf != nil {
				sm.checkExpiredEvents(buf)
				sm.deleteBufferIfEmpty(buf, event.Category)
			}
		case _ = <-sm.Quit:
			if sm.LoggingEnabled {
				log.Printf("SubscriptionManager: received quit signal, stopping.")
			}
			return nil
		}
		// TODO: add some time.After case where we purge events with old TTL
		// by using a datastruct that maps event time to buffer.
		// this could be oldest event time to buffer mapping structure
		// where any event older than (now - TTL) gets removed and empty
		// buffers deleted.  also data struct would need to update in place
		// which if problematic, perhaps use most-recent time and just delete
		// entry for empty buffers.  then rely on TTL cleanup when events
		// are added/fetched.  Technically events can last longer than TTL
		// by a little bit, but any time a new event or client request on
		// that category occurs, the old events would first be removed.
		// TODO: remove any empty buffers
	}
}

func (sm *subscriptionManager) deleteBufferIfEmpty(buf *eventBuffer, category string) error {
	if buf.List.Len() == 0 {
		delete(sm.SubEventBuffer, category)
		if sm.LoggingEnabled {
			log.Printf("Deleting empty eventBuffer for category: %s", category)
		}
	}
	return nil
}

func (sm *subscriptionManager) checkExpiredEvents(buf *eventBuffer) error {
	// TODO: implement
	// TODO: make this func also work when doing periodic check against all
	// buffers.  Will this involve another data struct with timestamps?
	// if so, can we just do a timestamp check on that data struct and skip
	// having to check event timestamps one by one?
	// TODO: on the other hand, would it be just as fast to check timestamp of oldest event in buffer?
	// TODO: if error ever possible have calling code check value
	return nil
}
