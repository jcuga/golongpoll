package golongpoll

import ()

// AddOn provides a way to add behavior to longpolling.
// For example: FilePersistorAddOn in addons/persistence/file.go provides a way
// to persist events to file to reuse across LongpollManager runs.
type AddOn interface {
	// OnLongpollStart is called from StartLongpoll() before LongpollManager's
	// run goroutine starts. This should return a channel of events to
	// pre-populate within the manager. If Options.EventTimeToLiveSeconds
	// is set, then events that already too old will be skipped.
	// AddOn implementers can provide their own pre-filtering of events
	// based on the TTL for efficiency to avoid sending expired events on
	// this channel in the first place.
	//
	// The AddOn must close the returned channel when it is done sending
	// initial events to it as the LongpollManager will read from the
	// channel until it is closed. The LongpollManager's main goroutine
	// will not launch until after all data is read from this channel.
	// NOTE: if an AddOn does not wish to pre-populate any events, then
	// simply return an empty channel that is already closed.
	//
	// IMPORTANT: the events sent to the returned channel must be in
	// chronological order! The longpoll manager will panic if events are sent
	// out of order. The reason for this is that the longpoll manager
	// does not have to sort published events internally since publishing
	// happens in real time. The fact that events from Publish naturally
	// have chronological timestamps allows for many efficiencies and
	// simplifications.
	OnLongpollStart() <-chan *Event

	// OnPublish will be called with any event published by the LongpollManager.
	// Note that the manager blocks on this function completing, so if
	// an AddOn is going to perform any slow operations like a file write,
	// database insert, or a network call, then it should do so it a separate
	// goroutine to avoid blocking the manager. See FilePersistorAddOn for an
	// example of how OnPublish can do its work in its own goroutine.
	OnPublish(*Event)

	// OnShutdown will be called on LongpollManager.Shutdown().
	// AddOns can perform any cleanup or flushing before exit.
	// The LongpollManager will block on this function during shutdown.
	// Example use: FilePersistorAddOn will flush any buffered file write data
	// to disk on shutdown.
	OnShutdown()
}

// TrivialAddOn is a trivial implementation of the AddOn interface.
// AddOn implementers can embed the TrivialAddOn if they only care to
// implement some of the required functions.
//
// For example, if you wanted an addon that simply logged when an event
// is published, you could do:
//
// type LoggerAddOn struct {
//   TrivialAddOn
// }
//
// func (lad *LoggerAddOn) OnPublish(event *Event) {
//   log.Printf("Event was published: %v\n", event)
// }
type TrivialAddOn struct{}

// OnPublish does nothing
func (tad *TrivialAddOn) OnPublish(event *Event) {
	return
}

// OnShutdown does nothing
func (tad *TrivialAddOn) OnShutdown() {
	return
}

// OnLongpollStart returns an empty, closed events channel.
func (tad *TrivialAddOn) OnLongpollStart() <-chan *Event {
	trivial := make(chan *Event)
	close(trivial)
	return trivial
}
