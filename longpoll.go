package golongpoll

import (
	"container/heap"
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gofrs/uuid"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

const (
	// forever is a magic number to represent 'Forever' in
	// LongpollOptions.EventTimeToLiveSeconds
	forever = -1001
)

// LongpollManager is used to interact with the internal longpolling pup-sub
// goroutine that is launched via StartLongpoll(Options).
//
// LongpollManager.SubscriptionHandler can be served directly or wrapped in an
// Http handler to add custom behavior like authentication. Events can be
// published via LongpollManager.Publish(). The manager can be stopped via
// Shutdown() or ShutdownWithTimeout(seconds int).
//
// If for some reason you want multiple goroutines handling different pub-sub
// channels, you can simply create multiple LongpollManagers and serve their
// subscription handlers on separate URLs.
type LongpollManager struct {
	subManager *subscriptionManager
	eventsIn   chan<- *Event
	stopSignal chan<- bool
	// SubscriptionHandler is a Http handler function that can be served
	// directly or wrapped within another handler function that adds additional
	// behavior like authentication or business logic.
	SubscriptionHandler func(w http.ResponseWriter, r *http.Request)
	// flag whether or not StartLongpoll has been called
	started bool
	// flag whether or not LongpollManager.Shutdown has been called--enforces
	// use-only-once.
	stopped bool
}

// Publish an event for a given subscription category.  This event can have any
// arbitrary data that is convert-able to JSON via the standard's json.Marshal()
// the category param must be a non-empty string no longer than 1024,
// otherwise you get an error. Cannot be called after LongpollManager.Shutdown()
// or LongpollManager.ShutdownWithTimeout(seconds int).
func (m *LongpollManager) Publish(category string, data interface{}) error {
	if !m.started {
		panic("LongpollManager cannot call Publish, never started. LongpollManager must be created via StartLongPoll(Options).")
	}

	if m.stopped {
		panic("LongpollManager cannot call Publish, already stopped.")
	}

	if len(category) == 0 {
		return errors.New("empty category")
	}
	if len(category) > 1024 {
		return errors.New("category cannot be longer than 1024")
	}

	u, err := uuid.NewV4()
	if err != nil {
		return err
	}

	m.eventsIn <- &Event{timeToEpochMilliseconds(time.Now()), category, data, u}
	return nil
}

// Shutdown will stop the LongpollManager's run goroutine and call Addon.OnShutdown.
// This will block on the Addon's shutdown call if an AddOn is provided.
// In addition to allowing a graceful shutdown, this can be useful if you want
// to turn off longpolling without terminating your program.
// After a shutdown, you can't call Publish() or get any new results from the
// SubscriptionHandler. Multiple calls to this function on the same manager will
// result in a panic.
func (m *LongpollManager) Shutdown() {
	if m.stopped {
		panic("LongpollManager cannot be stopped more than once.")
	}

	if !m.started {
		panic("LongpollManager cannot be stopped, never started. LongpollManager must be created via StartLongPoll(Options).")
	}

	m.stopped = true
	close(m.stopSignal)
	<-m.subManager.shutdownDone
}

// ShutdownWithTimeout will call Shutdown but only block for a provided
// amount of time when waiting for the shutdown to complete.
// Returns an error on timeout, otherwise nil. This can only be called once
// otherwise it will panic.
func (m *LongpollManager) ShutdownWithTimeout(seconds int) error {
	if m.stopped {
		panic("LongpollManager cannot be stopped more than once.")
	}

	if !m.started {
		panic("LongpollManager cannot be stopped, never started. LongpollManager must be created via StartLongPoll(Options).")
	}

	m.stopped = true
	close(m.stopSignal)
	select {
	case <-m.subManager.shutdownDone:
		return nil
	case <-time.After(time.Duration(seconds) * time.Second):
		return errors.New("LongpollManager Shutdown timeout exceeded.")
	}
}

// Options for LongpollManager that get sent to StartLongpoll(options)
type Options struct {
	// Whether or not to print non-error logs about longpolling.
	// Useful mainly for debugging, defaults to false.
	// NOTE: this will log every event's contents which can be spammy!
	LoggingEnabled bool

	// Max client timeout seconds to be accepted by the SubscriptionHandler
	// (The 'timeout' HTTP query param).  Defaults to 110.
	// NOTE: if serving behind a proxy/webserver, make sure the max allowed
	// timeout here is less than that server's configured HTTP timeout!
	// Typically, servers will have a 60 or 120 second timeout by default.
	MaxLongpollTimeoutSeconds int

	// How many events to buffer per subscriptoin category before discarding
	// oldest events due to buffer being exhausted.  Larger buffer sizes are
	// useful for high volumes of events in the same categories.  But for
	// low-volumes, smaller buffer sizes are more efficient.  Defaults to 250.
	MaxEventBufferSize int

	// How long (seconds) events remain in their respective category's
	// eventBuffer before being deleted. Deletes old events even if buffer has
	// the room.  Useful to save space if you don't need old events.
	// You can use a large MaxEventBufferSize to handle spikes in event volumes
	// in a single category but have a relatively short EventTimeToLiveSeconds
	// value to save space in the more common low-volume case.
	// Defaults to infinite/forever TTL.
	EventTimeToLiveSeconds int

	// Whether or not to delete an event as soon as it is retrieved via an
	// HTTP longpoll.  Saves on space if clients only interested in seeing an
	// event once and never again.  Meant mostly for scenarios where events
	// act as a sort of notification and each subscription category is assigned
	// to a single client.  As soon as any client(s) pull down this event, it's
	// gone forever.  Notice how multiple clients can get the event if there
	// are multiple clients actively in the middle of a longpoll when a new
	// event occurs.  This event gets sent to all listening clients and then
	// the event skips being placed in a buffer and is gone forever.
	DeleteEventAfterFirstRetrieval bool

	// Optional add-on to add behavior like event persistence to longpolling.
	AddOn AddOn
}

// StartLongpoll creates a LongpollManager, starts the internal pub-sub goroutine
// and returns the manager reference which you can use anywhere to Publish() events
// or attach a URL to the manager's SubscriptionHandler member.  This function
// takes an Options struct that configures the longpoll behavior.
// If Options.EventTimeToLiveSeconds is omitted, the default is forever.
func StartLongpoll(opts Options) (*LongpollManager, error) {
	// default if not specified (likely struct skipped defining this field)
	if opts.MaxLongpollTimeoutSeconds == 0 {
		opts.MaxLongpollTimeoutSeconds = 110
	}
	// default if not specified (likely struct skipped defining this field)
	if opts.MaxEventBufferSize == 0 {
		opts.MaxEventBufferSize = 250
	}
	// If TTL is zero, default to FOREVER
	if opts.EventTimeToLiveSeconds == 0 {
		opts.EventTimeToLiveSeconds = forever
	}
	if opts.MaxEventBufferSize < 1 {
		return nil, errors.New("Options.MaxEventBufferSize must be at least 1")
	}
	if opts.MaxLongpollTimeoutSeconds < 1 {
		return nil, errors.New("Options.MaxLongpollTimeoutSeconds must be at least 1")
	}
	// TTL must be positive, non-zero, or the magic forever value (a negative const)
	if opts.EventTimeToLiveSeconds < 1 && opts.EventTimeToLiveSeconds != forever {
		return nil, errors.New("options.EventTimeToLiveSeconds must be at least 1 or the constant longpoll.FOREVER")
	}
	channelSize := 100
	clientRequestChan := make(chan *clientSubscription, channelSize)
	clientTimeoutChan := make(chan *clientCategoryPair, channelSize)
	events := make(chan *Event, channelSize)
	// never has a send, only a close, so no larger capacity needed:
	quit := make(chan bool, 1)
	subManager := subscriptionManager{
		clientSubscriptions:            clientRequestChan,
		ClientTimeouts:                 clientTimeoutChan,
		Events:                         events,
		ClientSubChannels:              make(map[string]map[uuid.UUID]chan<- []*Event),
		SubEventBuffer:                 make(map[string]*expiringBuffer),
		Quit:                           quit,
		shutdownDone:                   make(chan bool, 1),
		LoggingEnabled:                 opts.LoggingEnabled,
		MaxLongpollTimeoutSeconds:      opts.MaxLongpollTimeoutSeconds,
		MaxEventBufferSize:             opts.MaxEventBufferSize,
		EventTimeToLiveSeconds:         opts.EventTimeToLiveSeconds,
		DeleteEventAfterFirstRetrieval: opts.DeleteEventAfterFirstRetrieval,
		// check for stale categories every 3 minutes.
		// remember we do expiration/cleanup on individual buffers whenever
		// activity occurs on that buffer's category (client request, event published)
		// so this periodic purge check is only needed to remove events on
		// categories that have been inactive for a while.
		staleCategoryPurgePeriodSeconds: 60 * 3,
		// set last purge time to present so we wait a full period before puring
		// if this defaulted to zero then we'd immediately do a purge which is unnecessary
		lastStaleCategoryPurgeTime: timeToEpochMilliseconds(time.Now()),
		// A priority queue (min heap) that keeps track of event buffers by their
		// last event time.  Used to know when to delete inactive categories/buffers.
		bufferPriorityQueue: make(priorityQueue, 0),
		AddOn:               opts.AddOn,
	}
	heap.Init(&subManager.bufferPriorityQueue)

	// Optionally prepopulate with data
	if subManager.AddOn != nil {
		startChan := subManager.AddOn.OnLongpollStart()

		// Don't add events if they would already be considered expired by the longpoll options.
		cutoffTime := int64(0)
		if subManager.EventTimeToLiveSeconds > 0 {
			cutoffTime = timeToEpochMilliseconds(time.Now()) - int64(subManager.EventTimeToLiveSeconds*1000)
		}

		currentEventTime := int64(0)
		for {
			event, ok := <-startChan
			if ok {
				if event.Timestamp < currentEventTime {
					// The internal datastructures assume data is added in chonological order.
					// Otherwise, lots of code would have to pay the penalty of ensuring
					// data is ordered and support insert-then-sort which I'm not prepared to
					// support.
					panic("Events supplied via AddOn.OnLongpollStart must be in chronological order (oldest first).")
				}

				currentEventTime = event.Timestamp

				if event.Timestamp > cutoffTime {
					subManager.handleNewEvent(event)
				}
			} else {
				// channel closed, we're done populating
				break
			}
		}
		// do any cleanup if options dictate it
		subManager.purgeStaleCategories()
	}

	// Start subscription manager
	go subManager.run()
	LongpollManager := LongpollManager{
		subManager: &subManager,
		eventsIn:   events,
		stopSignal: quit,
		SubscriptionHandler: getLongPollSubscriptionHandler(opts.MaxLongpollTimeoutSeconds,
			clientRequestChan, clientTimeoutChan, opts.LoggingEnabled),
		started: true,
	}
	return &LongpollManager, nil
}

type clientSubscription struct {
	clientCategoryPair
	// Used to limit events to after a specific time
	LastEventTime time.Time
	// Used in conjunction with LastEventTime to ensue no events are skipped
	LastEventID *uuid.UUID
	// we channel arrays of events since we need to send everything a client
	// cares about in a single channel send.  This makes channel receives a
	// one shot deal.
	Events chan []*Event
}

func newclientSubscription(subscriptionCategory string, lastEventTime time.Time, lastEventID *uuid.UUID) (*clientSubscription, error) {
	u, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}
	subscription := clientSubscription{
		clientCategoryPair{ClientUUID: u, SubscriptionCategory: subscriptionCategory},
		lastEventTime,
		lastEventID,
		make(chan []*Event, 1),
	}
	return &subscription, nil
}

// get web handler that has closure around sub chanel and clientTimeout channnel
func getLongPollSubscriptionHandler(maxTimeoutSeconds int, subscriptionRequests chan *clientSubscription,
	clientTimeouts chan<- *clientCategoryPair, loggingEnabled bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		timeout, err := strconv.Atoi(r.URL.Query().Get("timeout"))
		if loggingEnabled {
			log.Println("INFO - golongpoll.subscriptionHandler - Handling HTTP request at ", r.URL)
		}
		// We are going to return json no matter what:
		w.Header().Set("Content-Type", "application/json")
		// Don't cache response:
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate") // HTTP 1.1.
		w.Header().Set("Pragma", "no-cache")                                   // HTTP 1.0.
		w.Header().Set("Expires", "0")                                         // Proxies.
		if err != nil || timeout > maxTimeoutSeconds || timeout < 1 {
			log.Printf("WARN - golongpoll.subscriptionHandler - Invalid or missing 'timeout' param. Must be 1-%d. Got: %q.\n",
				maxTimeoutSeconds, r.URL.Query().Get("timeout"))
			io.WriteString(w, fmt.Sprintf("{\"error\": \"Invalid or missing 'timeout' arg.  Must be 1-%d.\"}", maxTimeoutSeconds))
			return
		}
		category := r.URL.Query().Get("category")
		if len(category) == 0 || len(category) > 1024 {
			log.Printf("WARN - golongpoll.subscriptionHandler - Invalid or missing subscription 'category', must be 1-1024 characters long.\n")
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
				log.Printf("WARN - golongpoll.subscriptionHandler - Error parsing since_time arg. Parm Value: %s, Error: %s.\n",
					lastEventTimeParam, parseError)
				io.WriteString(w, "{\"error\": \"Invalid 'since_time' arg.\"}")
				return
			}
		}

		var lastEventID *uuid.UUID

		lastIdParam := r.URL.Query().Get("last_id")

		if len(lastIdParam) > 0 {
			// further restricting since_time to additionally get events since given last even ID.
			// this handles scenario where multiple events have the same timestamp and we don't
			// want to miss the other events with the same timestamp (issue #19).
			if len(lastEventTimeParam) == 0 {
				log.Printf("WARN - golongpoll.subscriptionHandler - Invalid request: last_id without since_time.\n")
				io.WriteString(w, "{\"error\": \"Must provide 'since_time' arg when providing 'last_id'.\"}")
				return
			}

			lastEventIDVal, uuidErr := uuid.FromString(lastIdParam)

			if uuidErr != nil {
				log.Printf("WARN - golongpoll.subscriptionHandler - Invalid request: last_id was not a valid UUID.\n")
				io.WriteString(w, "{\"error\": \"Param 'last_id' was not a valid UUID.\"}")
				return
			}

			lastEventID = &lastEventIDVal
		}

		subscription, err := newclientSubscription(category, lastEventTime, lastEventID)
		if err != nil {
			log.Printf("ERROR - golongpoll.subscriptionHandler - Error creating new Subscription: %s.\n", err)
			io.WriteString(w, "{\"error\": \"Error creating new Subscription.\"}")
			return
		}
		subscriptionRequests <- subscription
		// Listens for connection close and un-register subscription in the
		// event that a client crashes or the connection goes down.  We don't
		// need to wait around to fulfill a subscription if no one is going to
		// receive it
		disconnectNotify := w.(http.CloseNotifier).CloseNotify()
		select {
		case <-time.After(time.Duration(timeout) * time.Second):
			// Lets the subscription manager know it can discard this request's
			// channel.
			clientTimeouts <- &subscription.clientCategoryPair
			timeoutResp := makeTimeoutResponse(time.Now())
			if jsonData, err := json.Marshal(timeoutResp); err == nil {
				io.WriteString(w, string(jsonData))
			} else {
				io.WriteString(w, "{\"error\": \"json marshaller failed\"}")
			}
		case events := <-subscription.Events:
			// Consume event.  Subscription manager will automatically discard
			// this client's channel upon sending event
			// NOTE: event is actually []Event
			if jsonData, err := json.Marshal(eventResponse{events}); err == nil {
				io.WriteString(w, string(jsonData))
			} else {
				io.WriteString(w, "{\"error\": \"json marshaller failed\"}")
			}
		case <-disconnectNotify:
			// Client connection closed before any events occurred and before
			// the timeout was exceeded.  Tell manager to forget about this
			// client.
			clientTimeouts <- &subscription.clientCategoryPair
		}
	}
}

// eventResponse is the json response that carries longpoll events.
type timeoutResponse struct {
	TimeoutMessage string `json:"timeout"`
	Timestamp      int64  `json:"timestamp"`
}

func makeTimeoutResponse(t time.Time) *timeoutResponse {
	return &timeoutResponse{
		TimeoutMessage: "no events before timeout",
		Timestamp:      timeToEpochMilliseconds(t),
	}
}

type clientCategoryPair struct {
	ClientUUID           uuid.UUID
	SubscriptionCategory string
}

type subscriptionManager struct {
	clientSubscriptions chan *clientSubscription
	ClientTimeouts      <-chan *clientCategoryPair
	Events              <-chan *Event
	// Contains all client sub channels grouped first by sub id then by
	// client uuid
	ClientSubChannels map[string]map[uuid.UUID]chan<- []*Event
	SubEventBuffer    map[string]*expiringBuffer
	// channel to inform manager to stop running
	Quit           <-chan bool
	shutdownDone   chan bool
	LoggingEnabled bool
	// Max allowed timeout seconds when clients requesting a longpoll
	// This is to validate the 'timeout' query param
	MaxLongpollTimeoutSeconds int
	// How big the buffers are (1-n) before events are discareded FIFO
	MaxEventBufferSize int
	// How long events can stay in their eventBuffer
	EventTimeToLiveSeconds int
	// Whether or not to delete an event after the first time it is served via
	// HTTP
	DeleteEventAfterFirstRetrieval bool
	// How often we check for stale event buffers and delete them
	staleCategoryPurgePeriodSeconds int
	// Last time in millisecondss since epoch that performed a stale category purge
	lastStaleCategoryPurgeTime int64
	// PriorityQueue/heap that keeps event buffers in oldest-first order
	// so we can easily delete eventBuffer/categories that have expired data
	bufferPriorityQueue priorityQueue
	// Optional add-on to add behavior like event persistence to longpolling.
	AddOn AddOn
}

// This should be fired off in its own goroutine
func (sm *subscriptionManager) run() error {
	if sm.LoggingEnabled {
		log.Println("INFO - golongpoll.run - Starting run.")
	}
	for {
		// NOTE: we check to see if its time to purge old buffers whenever
		// something happens or a period of inactivity has occurred.
		// An alternative would be to have another goroutine with a
		// select case time.After() but then you'd have concurrency issues
		// with access to the sm.SubEventBuffer and sm.bufferPriorityQueue objs
		// So instead of introducing mutexes we have this uglier manual time check calls
		select {
		case newClient := <-sm.clientSubscriptions:
			sm.handleNewClient(newClient)
			sm.seeIfTimeToPurgeStaleCategories()
		case disconnected := <-sm.ClientTimeouts:
			sm.handleClientDisconnect(disconnected)
			sm.seeIfTimeToPurgeStaleCategories()
		case event := <-sm.Events:
			// Optional hook on publish
			if sm.AddOn != nil {
				sm.AddOn.OnPublish(event)
			}
			sm.handleNewEvent(event)
			sm.seeIfTimeToPurgeStaleCategories()
		case <-time.After(time.Duration(5) * time.Second):
			sm.seeIfTimeToPurgeStaleCategories()
		case _ = <-sm.Quit:
			if sm.LoggingEnabled {
				log.Println("INFO - golongpoll.run - Received quit signal, stopping.")
			}

			// If a Publish() and Shutdown() occur one after the other from the
			// same goroutine, it is random whether or not the quit signal will
			// be seen before the published data, so on shutdown, see if there
			// are additional events before shutting down.
			select {
			case <-time.After(time.Duration(1) * time.Millisecond):
				break
			case event := <-sm.Events:
				if sm.AddOn != nil {
					sm.AddOn.OnPublish(event)
				}
				sm.handleNewEvent(event)
			}

			// optional shutdown callback
			if sm.AddOn != nil {
				sm.AddOn.OnShutdown()
			}

			// signal done shutting down
			close(sm.shutdownDone)

			// break out of our infinite loop/select
			return nil
		}
	}
}

func (sm *subscriptionManager) seeIfTimeToPurgeStaleCategories() error {
	nowMs := timeToEpochMilliseconds(time.Now())
	if nowMs > (sm.lastStaleCategoryPurgeTime + int64(1000*sm.staleCategoryPurgePeriodSeconds)) {
		sm.lastStaleCategoryPurgeTime = nowMs
		return sm.purgeStaleCategories()
	}
	return nil
}

func (sm *subscriptionManager) handleNewClient(newClient *clientSubscription) error {
	var funcErr error
	// before storing client sub request, see if we already have data in
	// the corresponding event buffer that we can use to fufil request
	// without storing it
	doQueueRequest := true
	if expiringBuf, found := sm.SubEventBuffer[newClient.SubscriptionCategory]; found {
		// First clean up anything that expired
		sm.checkExpiredEvents(expiringBuf)
		// We have a buffer for this sub category, check for buffered events
		if events, err := expiringBuf.eventBufferPtr.GetEventsSince(newClient.LastEventTime,
			sm.DeleteEventAfterFirstRetrieval, newClient.LastEventID); err == nil && len(events) > 0 {
			doQueueRequest = false
			if sm.LoggingEnabled {
				log.Printf("DEBUG - golongpoll.handleNewClient - Skip adding client, sending %d events. (Category: %q Client: %s)\n",
					len(events), newClient.SubscriptionCategory, newClient.ClientUUID.String())
			}
			// Send client buffered events.  Client will immediately consume
			// and end long poll request, so no need to have manager store
			newClient.Events <- events
		} else if err != nil {
			funcErr = fmt.Errorf("Error getting events from event buffer: %s.\n", err)
			log.Printf("ERROR - golongpoll.handleNewClient - Error getting events from event buffer: %s.\n", err)
		}
		// Buffer Could have been emptied due to the  DeleteEventAfterFirstRetrieval
		// or EventTimeToLiveSeconds options.
		sm.deleteBufferIfEmpty(expiringBuf, newClient.SubscriptionCategory)
		// NOTE: expiringBuf may now be invalidated (if it was empty/deleted),
		// don't use ref anymore.
	}
	if doQueueRequest {
		// Couldn't find any immediate events, store for future:
		categoryClients, found := sm.ClientSubChannels[newClient.SubscriptionCategory]
		if !found {
			// first request for this sub category, add client chan map entry
			categoryClients = make(map[uuid.UUID]chan<- []*Event)
			sm.ClientSubChannels[newClient.SubscriptionCategory] = categoryClients
		}
		if sm.LoggingEnabled {
			log.Printf("DEBUG - golongpoll.handleNewClient - Adding Client (Category: %q Client: %s)\n",
				newClient.SubscriptionCategory, newClient.ClientUUID.String())
		}
		categoryClients[newClient.ClientUUID] = newClient.Events
	}
	return funcErr
}

func (sm *subscriptionManager) handleClientDisconnect(disconnected *clientCategoryPair) error {
	var funcErr error
	if subCategoryClients, found := sm.ClientSubChannels[disconnected.SubscriptionCategory]; found {
		// NOTE:  The delete function doesn't return anything, and will do nothing if the
		// specified key doesn't exist.
		delete(subCategoryClients, disconnected.ClientUUID)
		if sm.LoggingEnabled {
			log.Printf("INFO - golongpoll.handleClientDisconnect - Removing Client (Category: %q Client: %s)\n",
				disconnected.SubscriptionCategory, disconnected.ClientUUID.String())
		}
		// Remove the client sub map entry for this category if there are
		// zero clients.  This keeps the ClientSubChannels map lean in
		// the event that there are many categories over time and we
		// would otherwise keep a bunch of empty sub maps
		if len(subCategoryClients) == 0 {
			delete(sm.ClientSubChannels, disconnected.SubscriptionCategory)
		}
	} else {
		// Sub category entry not found.  Weird.  Log this!
		log.Printf("WARN - golongpoll.handleClientDisconnect - Client disconnect for non-existing subscription category: %q\n",
			disconnected.SubscriptionCategory)
		funcErr = fmt.Errorf("Client disconnect for non-existing subscription category: %q\n",
			disconnected.SubscriptionCategory)
	}
	return funcErr
}

func (sm *subscriptionManager) handleNewEvent(newEvent *Event) error {
	var funcErr error
	doBufferEvents := true
	// Send event to any listening client's channels
	if clients, found := sm.ClientSubChannels[newEvent.Category]; found && len(clients) > 0 {
		if sm.DeleteEventAfterFirstRetrieval {
			// Configured to delete events from buffer after first retrieval by clients.
			// Now that we already have clients receiving, don't bother
			// buffering the event.
			// NOTE: this is wrapped by condition that clients are found
			// if no clients are found, we queue the event even when we have
			// the delete-on-first option set because no-one has received
			// the event yet.
			doBufferEvents = false
		}
		if sm.LoggingEnabled {
			log.Printf("DEBUG - golongpoll.handleNewEvent - Forwarding event to %d clients. (event: %v)\n", len(clients), newEvent)
		}
		for clientUUID, clientChan := range clients {
			if sm.LoggingEnabled {
				log.Printf("DEBUG - golongpoll.handleNewEvent - Sending event to client: %s\n", clientUUID.String())
			}
			clientChan <- []*Event{newEvent}
		}
		// Remove all client subscriptions since we just sent all the
		// clients an event.  In longpolling, subscriptions only last
		// until there is data (which just got sent) or a timeout
		// (which is handled by the disconnect case).
		// Doing this also keeps the subscription map lean in the event
		// of many different subscription categories, we don't keep the
		// trivial/empty map entries.
		if sm.LoggingEnabled {
			log.Printf("DEBUG - golongpoll.handleNewEvent - Removing %d client subscriptions for: %s\n",
				len(clients), newEvent.Category)
		}
		delete(sm.ClientSubChannels, newEvent.Category)
	} // else no client subscriptions

	expiringBuf, bufFound := sm.SubEventBuffer[newEvent.Category]
	if doBufferEvents {
		// Add event buffer for this event's subscription category if doesn't exist
		if !bufFound {
			nowMs := timeToEpochMilliseconds(time.Now())
			buf := &eventBuffer{
				list.New(),
				sm.MaxEventBufferSize,
				nowMs,
			}
			expiringBuf = &expiringBuffer{
				eventBufferPtr: buf,
				category:       newEvent.Category,
				priority:       nowMs,
			}
			if sm.LoggingEnabled {
				log.Printf("DEBUG - golongpoll.handleNewEvent - Creating new eventBuffer for category: %q",
					newEvent.Category)
			}
			sm.SubEventBuffer[newEvent.Category] = expiringBuf
			sm.priorityQueueUpdateBufferCreated(expiringBuf)
		}
		// queue event in event buffer
		if qErr := expiringBuf.eventBufferPtr.QueueEvent(newEvent); qErr != nil {
			log.Printf("ERROR - golongpoll.handleNewEvent - Failed to queue event.  err: %s\n", qErr)
			funcErr = fmt.Errorf("Error: failed to queue event.  err: %s\n", qErr)
		} else {
			if sm.LoggingEnabled {
				log.Printf("INFO - golongpoll.handleNewEvent - Queued event: %v.\n", newEvent)
			}
			// Queued event successfully
			sm.priorityQueueUpdateNewEvent(expiringBuf, newEvent)
		}
	} else {
		if sm.LoggingEnabled {
			log.Printf("INFO - golongpoll.handleNewEvent - DeleteEventAfterFirstRetrieval: skip queue event: %v.\n", newEvent)
		}
	}
	// Perform Event TTL check and empty buffer cleanup:
	if bufFound && expiringBuf != nil {
		sm.checkExpiredEvents(expiringBuf)
		sm.deleteBufferIfEmpty(expiringBuf, newEvent.Category)
		// NOTE: expiringBuf may now be invalidated if it was deleted
	}
	return funcErr
}

func (sm *subscriptionManager) checkExpiredEvents(expiringBuf *expiringBuffer) error {
	if sm.EventTimeToLiveSeconds == forever {
		// Events can never expire. bail out early instead of wasting time.
		return nil
	}
	// determine what time is considered the threshold for expiration
	nowMs := timeToEpochMilliseconds(time.Now())
	expirationTime := nowMs - int64(sm.EventTimeToLiveSeconds*1000)
	return expiringBuf.eventBufferPtr.DeleteEventsOlderThan(expirationTime)
}

func (sm *subscriptionManager) deleteBufferIfEmpty(expiringBuf *expiringBuffer, category string) error {
	if expiringBuf.eventBufferPtr.List.Len() == 0 {
		if sm.LoggingEnabled {
			log.Printf("DEBUG - golongpoll.deleteBufferIfEmpty - Deleting empty eventBuffer for category: %q\n", category)
		}
		delete(sm.SubEventBuffer, category)
		sm.priorityQueueUpdateDeletedBuffer(expiringBuf)
	}
	return nil
}

func (sm *subscriptionManager) purgeStaleCategories() error {
	if sm.EventTimeToLiveSeconds == forever {
		// Events never expire, don't bother checking here
		return nil
	}
	if sm.LoggingEnabled {
		log.Println("DEBUG - golongpoll.purgeStaleCategories - Performing stale category purge.")
	}
	nowMs := timeToEpochMilliseconds(time.Now())
	expirationTime := nowMs - int64(sm.EventTimeToLiveSeconds*1000)
	for sm.bufferPriorityQueue.Len() > 0 {
		topPriority, err := sm.bufferPriorityQueue.peekTopPriority()
		if err != nil {
			// queue is empty (threw empty buffer error) nothing to purge
			break
		}
		if topPriority > expirationTime {
			// The eventBuffer with the oldest most-recent-event-Timestamp is
			// still too recent to be expired, nothing to purge.
			break
		} else { // topPriority <= expirationTime
			// This buffer's most recent event is older than our TTL, so remove
			// the entire buffer.
			if item, ok := heap.Pop(&sm.bufferPriorityQueue).(*expiringBuffer); ok {
				if sm.LoggingEnabled {
					log.Printf("DEBUG - golongpoll.purgeStaleCategories - Purging expired eventBuffer for category: %q.\n",
						item.category)
				}
				// remove from our category-to-buffer map:
				delete(sm.SubEventBuffer, item.category)
				// invalidate references
				item.eventBufferPtr = nil
			} else {
				log.Printf("ERROR - golongpoll.purgeStaleCategories - Found item in bufferPriorityQueue of unexpected type when attempting a TTL purge.\n")
			}
		}
		// will continue until we either run out of heap/queue items or we found
		// a buffer that has events more recent than our TTL window which
		// means we will never find any older buffers.
	}
	return nil
}

// Wraps updates to SubscriptionManager.bufferPriorityQueue when a new
// eventBuffer is created for a given category.  In the event that we don't
// expire events (TTL == FOREVER), we don't bother paying the price of keeping
// the priority queue.
func (sm *subscriptionManager) priorityQueueUpdateBufferCreated(expiringBuf *expiringBuffer) error {
	if sm.EventTimeToLiveSeconds == forever {
		// don't bother keeping track
		return nil
	}
	// NOTE: this call has a complexity of O(log(n)) where n is len of heap
	heap.Push(&sm.bufferPriorityQueue, expiringBuf)
	return nil
}

// Wraps updates to SubscriptionManager.bufferPriorityQueue when a new Event
// is added to an eventBuffer.  In the event that we don't expire events
// (TTL == FOREVER), we don't bother paying the price of keeping the priority
// queue.
func (sm *subscriptionManager) priorityQueueUpdateNewEvent(expiringBuf *expiringBuffer, newEvent *Event) error {
	if sm.EventTimeToLiveSeconds == forever {
		// don't bother keeping track
		return nil
	}
	// Update the priority to be the new event's timestamp.
	// we keep the buffers in order of oldest last-event-timestamp
	// so we can fetch the most stale buffers first when we do
	// purgeStaleCategories()
	//
	// NOTE: this call is O(log(n)) where n is len of heap/priority queue
	sm.bufferPriorityQueue.updatePriority(expiringBuf, newEvent.Timestamp)
	return nil
}

// Wraps updates to SubscriptionManager.bufferPriorityQueue when an eventBuffer
// is deleted after becoming empty.  In the event that we don't
// expire events (TTL == FOREVER), we don't bother paying the price of keeping
// the priority queue.
// NOTE: This is called after an eventBuffer is deleted from sm.SubEventBuffer
// and we want to remove the corresponding buffer item from our priority queue
func (sm *subscriptionManager) priorityQueueUpdateDeletedBuffer(expiringBuf *expiringBuffer) error {
	if sm.EventTimeToLiveSeconds == forever {
		// don't bother keeping track
		return nil
	}
	// NOTE: this call is O(log(n)) where n is len of heap (queue)
	heap.Remove(&sm.bufferPriorityQueue, expiringBuf.index)
	expiringBuf.eventBufferPtr = nil // remove reference to eventBuffer
	return nil
}
