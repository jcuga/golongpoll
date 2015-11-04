package longpoll

import (
	"container/list"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	// TODO: peg version of libraries if versions tagged
	"github.com/nu7hatch/gouuid"
)

// TODO: reconsider how to expose interface/interaction with manager
// TODO: expose way to shut manager down
// TODO: flag/setting as to whether or not we log
// TOOD: buffer size option, internal chan size option?

// Expose event channel type:
type EventChannel chan Event

// Expose ability to create event:
func NewEvent(eventTime time.Time, category string, data interface{}) (Event, error) {
	timestamp := timeToEpochMilliseconds(eventTime)
	// TODO: enforce valid time, other stuff?
	// TODO: don't allow trivial timestamp (time 0/1970)
	// TODO: enforce string is not empty/whitespace
	return Event{timestamp, category, data}, nil
}

// Expose way to start longpoll subscription channel and event ajax handler:
func StartLongpollManager() (chan Event, func(w http.ResponseWriter, r *http.Request)) {
	// TODO: make channel sizes a const or config
	clientRequestChan := make(chan clientSubscription, 100)
	clientTimeoutChan := make(chan clientCategoryPair, 100)
	events := make(chan Event, 100)
	quit := make(chan bool, 1)

	subManager := subscriptionManager{
		clientSubscriptions: clientRequestChan,
		ClientTimeouts:      clientTimeoutChan,
		Events:              events,
		ClientSubChannels:   make(map[string]map[uuid.UUID]chan<- []Event),
		SubEventBuffer:      make(map[string]eventBuffer),
		Quit:                quit,
		MaxEventBufferSize:  1000,
	}

	// Start subscription manager
	go subManager.Run()
	return events, getLongPollSubscriptionHandler(clientRequestChan, clientTimeoutChan)
}

// TODO: make example go program with sample javascript client

type clientSubscription struct {
	clientCategoryPair
	// used to ensure no events skipped between long polls
	LastEventTime time.Time
	// we channel arrays of events since we need to send everything a client
	// cares about in a single channel send.  This makes channel receives a
	// one shot deal.
	Events chan []Event
}

func newclientSubscription(subscriptionCategory string, lastEventTime time.Time) (*clientSubscription, error) {
	u, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}
	subscription := clientSubscription{
		clientCategoryPair{*u, subscriptionCategory},
		lastEventTime,
		make(chan []Event, 1),
	}
	return &subscription, nil
}

// get web handler that has closure around sub chanel and clientTimeout channnel
func getLongPollSubscriptionHandler(subscriptionRequests chan clientSubscription,
	clientTimeouts chan<- clientCategoryPair) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		timeout, err := strconv.Atoi(r.URL.Query().Get("timeout"))
		log.Println("Handling HTTP request at ", r.URL)
		// We are going to return json no matter what:
		w.Header().Set("Content-Type", "application/json")
		// Don't cache response:
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate") // HTTP 1.1.
		w.Header().Set("Pragma", "no-cache")                                   // HTTP 1.0.
		w.Header().Set("Expires", "0")                                         // Proxies.
		if err != nil || timeout > 180 || timeout < 1 {
			log.Printf("Error: Invalid timeout param.  Must be 1-180. Got: %q.\n",
				r.URL.Query().Get("timeout"))
			io.WriteString(w, "{\"error\": \"Invalid timeout arg.  Must be 1-180.\"}")
			return
		}
		category := r.URL.Query().Get("category")
		if len(category) == 0 || len(category) > 255 {
			// TODO: add any extra validation on category?
			log.Printf("Error: Invalid subscription category.\n")
			io.WriteString(w, "{\"error\": \"Invalid subscription category.\"}")
			return
		}
		if err != nil {
			log.Printf("Error creating new Subscription: %s.\n", err)
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
				log.Printf(fmt.Sprintf(
					"Error parsing last_event_time arg. Parm Value: %s, Error: %s.\n", lastEventTimeParam, err))
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

// TODO: make types/members private where ever it makes sense
type subscriptionManager struct {
	clientSubscriptions chan clientSubscription
	ClientTimeouts      <-chan clientCategoryPair
	Events              <-chan Event
	// Contains all client sub channels grouped first by sub id then by
	// client uuid
	ClientSubChannels map[string]map[uuid.UUID]chan<- []Event
	SubEventBuffer    map[string]eventBuffer // TODO: ptr to eventBuffer instead of actual value?
	// channel to inform manager to stop running
	Quit <-chan bool
	// How big the buffers are (1-n) before events are discareded FIFO
	// TODO: enforce sane range 1-n where n isn't batshit crazy
	MaxEventBufferSize int
}

// TODO: add func to create sub manager that adds vars for chan and buf sizes
// with validation

func (sm *subscriptionManager) Run() error {
	log.Printf("SubscriptionManager: Starting run.")
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
					log.Printf("SubscriptionManager: Skip adding client, sending %d events. (Category: %q Client: %s)",
						len(events), newClient.SubscriptionCategory, newClient.ClientUUID.String())
					fmt.Printf("EVENTS: %v\n", events)
					// Send client buffered events.  Client will immediately consume
					// and end long poll request, so no need to have manager store
					newClient.Events <- events
				} else if err != nil {
					log.Printf("Error getting events from event buffer: %s.", err)
				}
			}
			if doQueueRequest {
				// Couldn't find any immediate events, store for future:
				categoryClients, found := sm.ClientSubChannels[newClient.SubscriptionCategory]
				if !found {
					// first request for this sub category, add client chan map entry
					categoryClients = make(map[uuid.UUID]chan<- []Event)
					sm.ClientSubChannels[newClient.SubscriptionCategory] = categoryClients
				}
				log.Printf("SubscriptionManager: Adding Client (Category: %q Client: %s)",
					newClient.SubscriptionCategory, newClient.ClientUUID.String())
				// TODO: unit tests to ensure clients add/skip behavior correct 'n tight
				categoryClients[newClient.ClientUUID] = newClient.Events
			}
		case disconnect := <-sm.ClientTimeouts:
			if subCategoryClients, found := sm.ClientSubChannels[disconnect.SubscriptionCategory]; found {
				// NOTE:  The delete function doesn't return anything, and will do nothing if the
				// specified key doesn't exist.
				delete(subCategoryClients, disconnect.ClientUUID)
				log.Printf("SubscriptionManager: Removing Client (Category: %q Client: %s)",
					disconnect.SubscriptionCategory, disconnect.ClientUUID.String())
			} else {
				// Sub category entry not found.  Weird.  Log this!
				log.Printf("Warning: cleint disconnect for non-existing subscription category: %q",
					disconnect.SubscriptionCategory)
			}
		case event := <-sm.Events:
			// Send event to any listening client's channels
			if clients, found := sm.ClientSubChannels[event.Category]; found && len(clients) > 0 {
				log.Printf("SubscriptionManager: forwarding event to %d clients. (event: %v)", len(clients), event)
				for clientUUID, clientChan := range clients {
					log.Printf("SubscriptionManager: sending event to client: %s", clientUUID.String())
					clientChan <- []Event{event}
					// boot this client subscription since we found events
					// In longpolling, subscriptions only last until there is
					// data (happening here) or a timeout (handled by the
					//disconnect case above)
					// NOTE: it IS safe to delete map entries as you iterate
					// SEE: http://stackoverflow.com/questions/23229975/is-it-safe-to-remove-selected-keys-from-golang-map-within-a-range-loop
					log.Printf("SubscriptionManager: Removing client after event send: %s", clientUUID.String())
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
			log.Printf("SubscriptionManager: queue event: %v.", event)
			// queue event in event buffer
			if qErr := buf.QueueEvent(&event); qErr != nil {
				log.Printf("Error: failed to queue event.  err: %s", qErr)
			}
		case _ = <-sm.Quit:
			log.Printf("SubscriptionManager: received quit signal, stopping.")
			return nil
		}
	}
}
