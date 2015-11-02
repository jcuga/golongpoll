package longpolling

import (
    "container/list"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "strconv"
    "time"

    "github.com/nu7hatch/gouuid"

    "github.com/jcuga/proxyblock/utils"
)

func StartLongpollManager() (chan Event, func(w http.ResponseWriter, r *http.Request)) {
    // TODO: make channel sizes a const or config
    clientRequestChan := make(chan ClientSubscription, 100)
    clientTimeoutChan := make(chan ClientSubPair, 100)
    events := make(chan Event, 100)
    quit := make(chan bool, 1)

    subManager := SubscriptionManager{
        ClientSubscriptions: clientRequestChan,
        ClientTimeouts:      clientTimeoutChan,
        Events:              events,
        ClientSubChannels:   make(map[string]map[uuid.UUID]chan<- []Event),
        SubEventBuffer:      make(map[string]EventBuffer),
        Quit:                quit,
        MaxEventBufferSize:  1000,
    }

    // Start subscription manager
    go subManager.Run()
    return events, getLongPollSubscriptionHandler(clientRequestChan, clientTimeoutChan)
}

// TODO: put sub manager and related event code in its own file leaving only
// lightweight example client

type ClientSubscription struct {
    ClientSubPair
    // used to ensure no events skipped between long polls
    LastEventTime time.Time
    // we channel arrays of events since we need to send everything a client
    // cares about in a single channel send.  This makes channel receives a
    // one shot deal.
    Events chan []Event
}

func NewClientSubscription(subscriptionCategory string, lastEventTime time.Time) (*ClientSubscription, error) {
    u, err := uuid.NewV4()
    if err != nil {
        return nil, err
    }
    subscription := ClientSubscription{
        ClientSubPair{*u, subscriptionCategory},
        lastEventTime,
        make(chan []Event, 1),
    }
    return &subscription, nil
}

// get web handler that has closure around sub chanel and clientTimeout channnel
func getLongPollSubscriptionHandler(subscriptionRequests chan ClientSubscription,
    clientTimeouts chan<- ClientSubPair) func(w http.ResponseWriter, r *http.Request) {
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
            lastEventTime, parseError = utils.MillisecondStringToTime(lastEventTimeParam)
            if parseError != nil {
                log.Printf(fmt.Sprintf(
                    "Error parsing last_event_time arg. Parm Value: %s, Error: %s.\n", lastEventTimeParam, err))
                io.WriteString(w, "{\"error\": \"Invalid last_event_time arg.\"}")
                return
            }
        }
        subscription, err := NewClientSubscription(category, lastEventTime)
        subscriptionRequests <- *subscription
        select {
        case <-time.After(time.Duration(timeout) * time.Second):
            // Lets the subscription manager know it can discard this request's
            // channel.
            clientTimeouts <- subscription.ClientSubPair
            io.WriteString(w, "{\"timeout\": \"no events before timeout\"}")
        case event := <-subscription.Events:
            // Consume event.  Subscription manager will automatically discard
            // this client's channel upon sending event
            // NOTE: event is actually []Event
            if jsonData, err := json.Marshal(EventResponse{&event}); err == nil {
                io.WriteString(w, string(jsonData))
            } else {
                io.WriteString(w, "{\"error\": \"json marshaller failed\"}")
            }
        }
    }
}

type EventResponse struct {
    Events *[]Event `json:"events"`
}

type Event struct {
    Timestamp int64  `json:"timestamp"` // milliseconds since epoch to match javascrits Date.getTime()
    Category  string `json:"category"`
    Data      string `json:"data"` // TODO: eventually make byte[] instead?
}

type ClientSubPair struct {
    ClientUUID           uuid.UUID
    SubscriptionCategory string
}

// TODO: make types/members private where ever it makes sense
type SubscriptionManager struct {
    ClientSubscriptions chan ClientSubscription
    ClientTimeouts      <-chan ClientSubPair
    Events              <-chan Event
    // Contains all client sub channels grouped first by sub id then by
    // client uuid
    ClientSubChannels map[string]map[uuid.UUID]chan<- []Event
    SubEventBuffer    map[string]EventBuffer // TODO: ptr to EventBuffer instead of actual value?
    // channel to inform manager to stop running
    Quit <-chan bool
    // How big the buffers are (1-n) before events are discareded FIFO
    // TODO: enforce sane range 1-n where n isn't batshit crazy
    MaxEventBufferSize int
}

// TODO: add func to create sub manager that adds vars for chan and buf sizes
// with validation

func (sm *SubscriptionManager) Run() error {
    log.Printf("SubscriptionManager: Starting run.")
    for {
        select {
        case newClient := <-sm.ClientSubscriptions:
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
                buf = EventBuffer{
                    list.New(),
                    sm.MaxEventBufferSize,
                }
                sm.SubEventBuffer[event.Category] = buf
            }
            log.Printf("SubscriptionManager: queue event: %v.", event)
            // queue event in event buffer
            buf.QueueEvent(&event)
        case _ = <-sm.Quit:
            log.Printf("SubscriptionManager: received quit signal, stopping.")
            return nil
        }
    }
}

type EventBuffer struct {
    // Doubly linked list of events where new events are added to the back/tail
    // and old events are removed from the front/root
    // NOTE: this is efficient for front/back operations since it is
    // implemented as a ring with root.prev being the tail
    // SEE: https://golang.org/src/container/list/list.go
    *list.List
    MaxBufferSize int
}

func (eb *EventBuffer) QueueEvent(event *Event) error {
    // Cull our buffer if we're at max capacity
    if eb.List.Len() > eb.MaxBufferSize {
        oldestEvent := eb.List.Back()
        if oldestEvent != nil {
            eb.List.Remove(oldestEvent)
        }
    }
    // Add event to front of our list
    eb.List.PushFront(event)
    return nil
}

func (eb *EventBuffer) GetEventsSince(since time.Time) ([]Event, error) {
    events := make([]Event, 0) // NOTE: having init cap > 0 has zero value Event
    // structs which we don't want!
    // events are bufferd FIFO with the most recent event in the front of the
    // buffer (list)
    for element := eb.List.Front(); element != nil; element = element.Next() {
        event, ok := element.Value.(*Event)
        if !ok {
            return nil, fmt.Errorf("Found non-event type in event buffer.")
        }
        // event time is after since time arg? convert since to epoch ms
        if event.Timestamp > utils.TimeToEpochMilliseconds(since) {
            events = append(events, Event{event.Timestamp, event.Category, event.Data})
        } else {
            // we've made it to events we've seen before, stop searching
            break
        }
    }
    // NOTE: events has the most recent event first followed by any older events
    // that occurred since client's last seen event
    // TODO: consider reversing order?  or is it an advantage to have
    // newest first so handled with more priority?  TODO: make this an option?
    return events, nil
}


// adapted from:
// http://stackoverflow.com/questions/13294649/how-to-parse-a-milliseconds-since-epoch-timestamp-string-in-go
func MillisecondStringToTime(ms string) (time.Time, error) {
    msInt, err := strconv.ParseInt(ms, 10, 64)
    if err != nil {
        return time.Time{}, err
    }
    return time.Unix(0, msInt*int64(time.Millisecond)), nil
}

// adapted from:
// http://stackoverflow.com/questions/24122821/go-golang-time-now-unixnano-convert-to-milliseconds
func TimeToEpochMilliseconds(t time.Time) int64 {
    return t.UnixNano() / int64(time.Millisecond)
}
