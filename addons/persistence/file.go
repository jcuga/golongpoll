// Add-on that shows how to leverage golongpoll.Options.AddOn to add file
// persistence to longpoll events. Without creating a persistence add-on, all
// event data is lost on program end as the data is kept only in memory.
//
// Library users can create their own add-ons for things like storing events in
// a database, create their own audit log of events, or send data via RPC or
// network call.
//
// NOTE: the OnPublish callback is called within the LongpollManager's main
// goroutine. So blocking operations will cause the longpoll manager to block.
// Hence why this example will rely on a channel with separate goroutine to
// avoid blocking the caller.
package persistence

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jcuga/golongpoll"
	"log"
	"os"
	"time"
)

// FilePersistorAddOn implements the golongpoll.AddOn interface to provide
// file persistence for longpoll events. Use NewFilePersistor(string, int, int)
// to create a configured FilePersistorAddOn.
//
// NOTE: uses bufio.NewScanner which has a default max scanner buffer size of
// 64kb (65536). So any event whose JSON is that large will fail to be
// read back in completely. These events, and any other events whose JSON
// fails to unmarshal will be skipped.
type FilePersistorAddOn struct {
	// Filename to use for storing events. The file will be created if it
	// does not already exist.
	filename string
	// How large the underlying bufio.Writer's buffer is.
	writeBufferSize int
	// How often to flush buffered data to disk.
	writeFlushPeriodSeconds int
	// Channel for incoming published events.
	// To avoid blocking LongpollManager when it calls OnPublish(), we send
	// events to channel for processing in a separate goroutine.
	publishedEvents chan *golongpoll.Event
	// Channel used to signal when done flushing to disk during OnShutdown().
	shutdownDone chan bool
}

// NewFilePersistor creates a new FilePersistorAddOn with the provided options.
// filename is the file to use for storing event data.
// writeBufferSize is how large a buffer is used to buffer output before
// writing to file. This is simply the underlying bufio.Writer's buffer size.
// writeFlushPeriodSeconds is how often to flush buffer to disk even when
// the output buffer is not filled completely. This helps avoid data loss
// in the event of a program crash.
// Returns a new FilePersistorAddOn and nil error on success, or a nil
// addon and a non-nil error explaining what went wrong creating a new addon.
func NewFilePersistor(filename string, writeBufferSize int, writeFlushPeriodSeconds int) (*FilePersistorAddOn, error) {
	if writeBufferSize < 1 {
		return nil, errors.New("writeBufferSize must be > 0")
	}

	if writeFlushPeriodSeconds < 1 {
		return nil, errors.New("writeFlushPeriodSeconds must be > 0")
	}

	if len(filename) < 1 {
		return nil, errors.New("filename cannot be empty")
	}

	// Ensure we can use filename
	f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return nil, fmt.Errorf("Error opening file for filename. Error: %v", err)
	}
	f.Close()

	eventsIn := make(chan *golongpoll.Event, 100)

	fp := FilePersistorAddOn{
		filename:                filename,
		writeBufferSize:         writeBufferSize,
		writeFlushPeriodSeconds: writeFlushPeriodSeconds,
		publishedEvents:         eventsIn,
		shutdownDone:            make(chan bool, 1),
	}
	return &fp, nil
}

// OnLongpollStart returns a channel of events that is populated
// via a separate goroutine so that the calling LongpollManager can
// begin consuming events while we're still reading more events in
// from file. Note that the goroutine that adds events to this returned
// channel will call close() on the channel when it is out of
// initial events. The LongpollManager will wait for more events
// until the channel is closed.
func (fp *FilePersistorAddOn) OnLongpollStart() <-chan *golongpoll.Event {
	// return a channel to send initial events to.
	ch := make(chan *golongpoll.Event)
	// populate input events channel in own goroutine, which will
	// call close(ch) once it sends all events.
	go fp.getOnStartInputEvents(ch)
	// launch the FilePersistorAddOn's run goroutine. This will read from
	// a different channel to handle OnPublish() events.
	go fp.run()
	return ch
}

// OnPublish will write new events to file.
// Events are sent via channel to a separate goroutine so that the calling
// LongpollManager does not block on the file writing.
func (fp *FilePersistorAddOn) OnPublish(event *golongpoll.Event) {
	fp.publishedEvents <- event
}

// OnShutdown will signal the run goroutine to flush any remaining event data
// to disk and wait for it to complete.
func (fp *FilePersistorAddOn) OnShutdown() {
	// Signal to stop working flush to disk.
	close(fp.publishedEvents)
	// wait for signal that flush to disk has finished (channel will be closed)
	<-fp.shutdownDone
}

// reads previously stored events from file and sends them to the channel
// returned by OnLongpollStart().
// NOTE: uses bufio.NewScanner which has a default max scanner buffer size of
// 64kb (65536). So any event whose JSON is that large will fail to be
// read back in completely. These events, and any other events whose JSON
// fails to unmarshal will be skipped.
func (fp *FilePersistorAddOn) getOnStartInputEvents(ch chan *golongpoll.Event) {
	f, err := os.Open(fp.filename)
	if err != nil {
		log.Printf("FilePersistorAddOn - Failed to open event file, error: %v\n", err)
		close(ch)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event golongpoll.Event
		var line = scanner.Bytes()
		// skip any blank lines as we prepend newline before writing event json.
		// NOTE: doing prepend instead of append so if an event getting written
		// out is stopped prematurely due to an ungraceful shutdown, we will
		// start a subsequent write on it's own line.
		if len(line) == 0 {
			continue
		}
		if err := json.Unmarshal(line, &event); err != nil {
			log.Printf("FilePersistorAddOn - Failed to unmarshal event json, error: %v\n", err)
			continue
		} else {
			ch <- &event
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("FilePersistorAddOn - Error scanning file, error: %v\n", err)
	}

	// NOTE: important to close--or calling LongpollManager will hang waiting
	// for more channel data.
	close(ch)
}

// FilePersistorAddOn's run goroutine that reads OnPublish()'s events from a
// channel. This allows OnPublish to return without handling the file writing
// directly, thus LongpollManager is not blocking on file writes.
// This will also flush to disk any buffered data at the configured time
// interval. When OnShtudown() is called, this will flush any remaining data
// and stop.
func (fp *FilePersistorAddOn) run() {
	lastFlushTime := time.Now()
	// NOTE: O_APPEND is critical here as I was an idiot and could not figure
	// out why I was getting weird file data wrapping around across restarts!
	// Without this, we'll start at the beginning of the file instead of adding
	// to the end. *smacks palm on forehead*
	outFile, err := os.OpenFile(fp.filename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0755)
	if err != nil {
		log.Printf("FilePersistorAddOn - STOPPING! Failed to open file, error: %v\n", err)
		return
	}
	defer outFile.Close()
	w := bufio.NewWriterSize(outFile, fp.writeBufferSize)
	defer w.Flush()

	for {
		select {
		case event, ok := <-fp.publishedEvents:
			if !ok {
				// channel closed, flush any buffered data to disk and stop
				w.Flush()
				// signal finished shutting down, OnShutdown is blocking waiting for channel action
				close(fp.shutdownDone)
				return
			}

			eventJson, err := json.Marshal(event)
			if err != nil {
				log.Printf("FilePersistorAddOn - Failed to marshal event json, error: %v\n", err)
				continue
			}

			// NOTE: adding newline before instead of after in case last event wasn't fully written
			// out to completion
			w.WriteString("\n")
			_, err = w.Write(eventJson)

			if err != nil {
				log.Printf("FilePersistorAddOn - Failed to write event json to file, error: %v\n", err)
				continue
			}

			if isTimeToFlush(lastFlushTime, fp.writeFlushPeriodSeconds) {
				w.Flush()
				lastFlushTime = time.Now()
			}

		case <-time.After(time.Duration(500) * time.Millisecond):
			if isTimeToFlush(lastFlushTime, fp.writeFlushPeriodSeconds) {
				w.Flush()
				lastFlushTime = time.Now()
			}
		}
	}
}

func isTimeToFlush(lastFlushTime time.Time, writeFlushPeriodSeconds int) bool {
	diff := time.Now().Sub(lastFlushTime)
	return diff.Seconds() >= float64(writeFlushPeriodSeconds)
}
