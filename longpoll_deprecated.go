package golongpoll

import (
	"log"
)

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
	log.Printf("WARNING: the function golongpoll.CreateManager is deprectated and should no longer be used.\n")
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
func CreateCustomManager(maxTimeoutSeconds, eventBufferSize int,
	loggingEnabled bool) (*LongpollManager, error) {
	log.Printf("WARNING: the function golongpoll.CreateCustomManager is deprectated and should no longer be used..\n")
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
