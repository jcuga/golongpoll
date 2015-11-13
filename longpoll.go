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
	return CreateCustomManager(180, 250, true)
}

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
	if eventBufferSize < 1 {
		return nil, errors.New("event buffer size must be at least 1")
	}
	if maxTimeoutSeconds < 1 {
		return nil, errors.New("max timeout seconds must be at least 1")
	}
	channelSize := 100
	clientRequestChan := make(chan clientSubscription, channelSize)
	clientTimeoutChan := make(chan clientCategoryPair, channelSize)
	events := make(chan lpEvent, channelSize)
	// never has a send, only a close, so no larger capacity needed:
	quit := make(chan bool, 1)
	subManager := subscriptionManager{
		clientSubscriptions: clientRequestChan,
		ClientTimeouts:      clientTimeoutChan,
		Events:              events,
		ClientSubChannels:   make(map[string]map[uuid.UUID]chan<- []lpEvent),
		SubEventBuffer:      make(map[string]*eventBuffer),
		Quit:                quit,
		MaxEventBufferSize:  eventBufferSize,
	}
	// Start subscription manager
	go subManager.run(loggingEnabled)
	LongpollManager := LongpollManager{
		&subManager,
		events,
		quit,
		getLongPollSubscriptionHandler(maxTimeoutSeconds, clientRequestChan, clientTimeoutChan, loggingEnabled),
	}
	return &LongpollManager, nil
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
		// Let's the goroutine checking for disconnect know to stop waiting for
		// a close notificaiton because the request was successfully fuffilled
		// Without this the goroutine would hang around for a while longer than
		// the request and then fire when the TCP connection closes.
		// This doesn't do any real harm, but its wasteful to leave running and
		// you'd see two logs about removing client which would seem weird.
		requestFuffilled := make(chan bool, 1)
		go func() {
			select {
			case <-disconnectNotify:
				clientTimeouts <- subscription.clientCategoryPair
			case <-requestFuffilled:
				// just let the goroutine reach end of execution
				break
			}
		}()
		select {
		case <-time.After(time.Duration(timeout) * time.Second):
			// Lets the subscription manager know it can discard this request's
			// channel.
			clientTimeouts <- subscription.clientCategoryPair
			io.WriteString(w, "{\"timeout\": \"no events before timeout\"}")
		case events := <-subscription.Events:
			// Consume event.  Subscription manager will automatically discard
			// this client's channel upon sending event
			// NOTE: event is actually []Event
			if jsonData, err := json.Marshal(eventResponse{&events}); err == nil {
				io.WriteString(w, string(jsonData))
			} else {
				io.WriteString(w, "{\"error\": \"json marshaller failed\"}")
			}
		}
		// cancel disconnect detection
		close(requestFuffilled)
	}
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
	Quit <-chan bool
	// How big the buffers are (1-n) before events are discareded FIFO
	MaxEventBufferSize int
}

// This should be fired off in its own goroutine
func (sm *subscriptionManager) run(loggingEnabled bool) error {
	if loggingEnabled {
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
				// We have a buffer for this sub category, check for buffered events
				if events, err := buf.GetEventsSince(newClient.LastEventTime); err == nil && len(events) > 0 {
					doQueueRequest = false
					if loggingEnabled {
						log.Printf("SubscriptionManager: Skip adding client, sending %d events. (Category: %q Client: %s)",
							len(events), newClient.SubscriptionCategory, newClient.ClientUUID.String())
					}
					// Send client buffered events.  Client will immediately consume
					// and end long poll request, so no need to have manager store
					newClient.Events <- events
				} else if err != nil {
					if loggingEnabled {
						log.Printf("Error getting events from event buffer: %s.", err)
					}
				}
			}
			if doQueueRequest {
				// Couldn't find any immediate events, store for future:
				categoryClients, found := sm.ClientSubChannels[newClient.SubscriptionCategory]
				if !found {
					// first request for this sub category, add client chan map entry
					categoryClients = make(map[uuid.UUID]chan<- []lpEvent)
					sm.ClientSubChannels[newClient.SubscriptionCategory] = categoryClients
				}
				if loggingEnabled {
					log.Printf("SubscriptionManager: Adding Client (Category: %q Client: %s)",
						newClient.SubscriptionCategory, newClient.ClientUUID.String())
				}
				// TODO: unit tests to ensure clients add/skip behavior correct 'n tight
				categoryClients[newClient.ClientUUID] = newClient.Events
			}
		case disconnect := <-sm.ClientTimeouts:
			if subCategoryClients, found := sm.ClientSubChannels[disconnect.SubscriptionCategory]; found {
				// NOTE:  The delete function doesn't return anything, and will do nothing if the
				// specified key doesn't exist.
				delete(subCategoryClients, disconnect.ClientUUID)
				if loggingEnabled {
					log.Printf("SubscriptionManager: Removing Client (Category: %q Client: %s)",
						disconnect.SubscriptionCategory, disconnect.ClientUUID.String())
				}
			} else {
				// Sub category entry not found.  Weird.  Log this!
				if loggingEnabled {
					log.Printf("Warning: cleint disconnect for non-existing subscription category: %q",
						disconnect.SubscriptionCategory)
				}
			}
		case event := <-sm.Events:
			// Send event to any listening client's channels
			if clients, found := sm.ClientSubChannels[event.Category]; found && len(clients) > 0 {
				if loggingEnabled {
					log.Printf("SubscriptionManager: forwarding event to %d clients. (event: %v)", len(clients), event)
				}
				for clientUUID, clientChan := range clients {
					if loggingEnabled {
						log.Printf("SubscriptionManager: sending event to client: %s", clientUUID.String())
					}
					clientChan <- []lpEvent{event}
					// boot this client subscription since we found events
					// In longpolling, subscriptions only last until there is
					// data (happening here) or a timeout (handled by the
					//disconnect case above)
					// NOTE: it IS safe to delete map entries as you iterate
					// SEE: http://stackoverflow.com/questions/23229975/is-it-safe-to-remove-selected-keys-from-golang-map-within-a-range-loop
					if loggingEnabled {
						log.Printf("SubscriptionManager: Removing client after event send: %s", clientUUID.String())
					}
					delete(clients, clientUUID)
				}
			}
			// Add event buffer for this event's subscription category if doesn't exit
			buf, bufFound := sm.SubEventBuffer[event.Category]
			if !bufFound {
				buf = &eventBuffer{
					list.New(),
					sm.MaxEventBufferSize,
				}
				sm.SubEventBuffer[event.Category] = buf
			}
			if loggingEnabled {
				log.Printf("SubscriptionManager: queue event: %v.", event)
			}
			// queue event in event buffer
			if qErr := buf.QueueEvent(&event); qErr != nil {
				if loggingEnabled {
					log.Printf("Error: failed to queue event.  err: %s", qErr)
				}
			}
		case _ = <-sm.Quit:
			if loggingEnabled {
				log.Printf("SubscriptionManager: received quit signal, stopping.")
			}
			return nil
		}
	}
}
