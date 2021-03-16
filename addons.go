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
