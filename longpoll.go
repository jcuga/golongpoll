package golongpoll

import (
	"container/list"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/nu7hatch/gouuid"
)

// Public interface to interact with the internall subscriptionManager.
// Allows you to publish events via Publish() and tell the manager to shut down
// via Shutdown().  A LongpollInterface is created with each subscriptionManager
// that gets created by CreateLongpollManager()
// This interface also exposes the HTTP handler that client code can attach to
// a URL like so:
//		mux := http.NewServeMux()
//		mux.HandleFunc("/custom/path/to/events", iface.SubscriptionHandler)
type LongpollInterface struct {
	eventsIn            chan<- lpEvent
	stopSignal          chan<- bool
	SubscriptionHandler func(w http.ResponseWriter, r *http.Request)
}

// Publish an event for a given subscription category.  This event can have any
// arbitrary data that is convert-able to JSON via the standard's json.Marshal()
// Category must be a non-empty string no longer than 1024,
// otherwise you get an error.
func (iface *LongpollInterface) Publish(category string, data interface{}) error {
	if len(category) == 0 {
		return errors.New("empty category")
	}
	if len(category) > 1024 {
		return errors.New("category cannot be longer than 1024")
	}
	iface.eventsIn <- lpEvent{timeToEpochMilliseconds(time.Now()), category, data}
	return nil
}

// Stop the subscriptionManager.  Useful if you want to stop longpolling without
// stopping your program.  Note that any ajax calls to the longpoll web handler
// would not get any new events once the manager is stopped.  You should also
// not call Publish() after calling Shutdown().  Calling Shutdown() more than
// once will result in a panic.
func (iface *LongpollInterface) Shutdown() {
	close(iface.stopSignal)
}

// Creates a basic longpolling subscription manager, starts it, and returns a
// LongpollInterface for clients to interact with.  The default options should
// cover most client's longpolling needs without having to worry about details.
// This uses the following options:
// channelSize (capacity of underlying event channels) of 100.
// eventBufferSize size 250 (buffers this many most recent events per
// subscription category) and loggingEnabled as true.
func CreateManager() (*LongpollInterface, error) {
	return CreateCustomManager(100, 250, true)
}

// Creates a custom longpolling subscription manager, starts it, and returns a
// LongpollInterface for clients to interact with.
// The subscription manager can be configured with the following params:
//
// channelSize: the capacity of the underlying channels that pipe client
// subscription requests, client timeouts, and new events to the manager.
// This must be at least 1, and is probably a good idea to be higher than that
// to support heavier loads without blocking on new channel sends.
// In general: the higher the number, the more memory being used by the manager,
// but the less likely that code is every blocking on a channel send waiting
// for the channel capacity to be freed up.
//
// eventBufferSize: this is how many events get buffered (per category).
// Buffering is important because it allows us to retain older events.
// If there are a lot of events occurring between the time a client has
// received a batch of new events, and the time when the client requests the
// next batch of events (clients typically request for more events as soon
// as they get a response, so the turnaround is typically a second or so)
// then we'd rely on the buffer to be large enough to retain at least that much
// time worth of events to ensure a complete stream of events to the web
// clients without any gaps.  In general, the larger the buffer, the more memory
// the manager uses, but the manager can support faster rates of event spawning.
// If you're publishing tons of events in very short amounts of time, you want
// larger buffers.
//
// loggingEnabled: whether or not we are outputting logs
func CreateCustomManager(channelSize, eventBufferSize int, loggingEnabled bool) (*LongpollInterface, error) {
	if channelSize < 1 {
		return nil, errors.New("channel size must be at least 1")
	}
	if eventBufferSize < 1 {
		return nil, errors.New("event buffer size must be at least 1")
	}
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
		SubEventBuffer:      make(map[string]eventBuffer),
		Quit:                quit,
		MaxEventBufferSize:  eventBufferSize,
	}
	// Start subscription manager
	go subManager.run(loggingEnabled)
	longpollInterface := LongpollInterface{
		events,
		quit,
		getLongPollSubscriptionHandler(clientRequestChan, clientTimeoutChan, loggingEnabled),
	}
	return &longpollInterface, nil
}

type clientSubscription struct {
	clientCategoryPair
	// used to ensure no events skipped between long polls
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
func getLongPollSubscriptionHandler(subscriptionRequests chan clientSubscription,
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
		if err != nil || timeout > 180 || timeout < 1 {
			if loggingEnabled {
				log.Printf("Error: Invalid timeout param.  Must be 1-180. Got: %q.\n",
					r.URL.Query().Get("timeout"))
			}
			io.WriteString(w, "{\"error\": \"Invalid timeout arg.  Must be 1-180.\"}")
			return
		}
		category := r.URL.Query().Get("category")
		if len(category) == 0 || len(category) > 1024 {
			if loggingEnabled {
				log.Printf("Error: Invalid subscription category.\n")
			}
			io.WriteString(w, "{\"error\": \"Invalid subscription category.\"}")
			return
		}
		if err != nil {
			if loggingEnabled {
				log.Printf("Error creating new Subscription: %s.\n", err)
			}
			io.WriteString(w, "{\"error\": \"Error creating new Subscription.\"}")
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
		subscriptionRequests <- *subscription
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
	SubEventBuffer    map[string]eventBuffer // TODO: ptr to eventBuffer instead of actual value?
	// channel to inform manager to stop running
	Quit <-chan bool
	// How big the buffers are (1-n) before events are discareded FIFO
	MaxEventBufferSize int
}

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
				buf = eventBuffer{
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
