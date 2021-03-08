// Add-on that shows how to leverage golongpoll.Options.OnLongpollStart and OnPublishHook callbacks
// to add file persistence to longpoll events. Without creating a persistence add-on,
// all event data is lost on program end as the data is kept only in memory.
//
// Library users can create their own add-ons for things like storing events in a database
// or they can do other things as well.  Other ideas could be to create their own audit
// log of events via the OnPublishHook, or pre-populate some event data via OnLongpollStart.
//
// NOTE: the OnPublish callback is called within the LongpollManager's main goroutine.
// So blocking operations will cause the longpoll manager to block.
// Hence why this example will rely on a channel with separate goroutine to avoid
// blocking the caller.
package persistence

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jcuga/golongpoll"
	"os"
	"time"
)

type FilePersistor struct {
	// Filename to use for storing events. The file will be created if it
	// does not already exist.
	filename                string
	writeBufferSize         int
	writeFlushPeriodSeconds int
	// channel for incoming published events.
	// To avoid blocking calling LongpollManager when it calls
	// OnPublish, we send events to channel for processing in a separate goroutine.
	publishedEvents chan *golongpoll.Event
	// Channel used to signal when done flushing to disk on shutdown.
	shutdownDone chan bool
}

// TODO: comments
func NewFilePersistor(filename string, writeBufferSize int, writeFlushPeriodSeconds int) (*FilePersistor, error) {
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

	fp := FilePersistor{
		filename:                filename,
		writeBufferSize:         writeBufferSize,
		writeFlushPeriodSeconds: writeFlushPeriodSeconds,
		publishedEvents:         eventsIn,
		shutdownDone:            make(chan bool, 1),
	}
	return &fp, nil
}

func (fp *FilePersistor) OnLongpollStart() <-chan *golongpoll.Event {
	// return a channel to send initial events to
	ch := make(chan *golongpoll.Event)
	// populate input events channel in own goroutine
	go fp.getOnStartInputEvents(ch)
	// launch the FilePersistor's run goroutine. This will read from (a different) channel to handle OnPublish callbacks.
	go fp.run()
	return ch
}

func (fp *FilePersistor) OnPublish(event *golongpoll.Event) {
	// Event handled in a separate goroutine so calling LongpollManager doesn't block
	// waiting for us to write to file.
	fp.publishedEvents <- event
}

func (fp *FilePersistor) OnShutdown() {
	// Signal to stop working and finish up.
	close(fp.publishedEvents)
	select {
	case <-time.After(10 * time.Second):
		break
	}
	// wait for signal that flush to disk has finished (channel will be closed)
	<-fp.shutdownDone
}

func (fp *FilePersistor) getOnStartInputEvents(ch chan *golongpoll.Event) {
	f, err := os.Open(fp.filename)
	if err != nil {
		// TODO: way to return error? cause longpoll manager to not start?
		fmt.Printf("DEBUG >> ERROR >> failed to open file: %v\n", err)
		close(ch)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// TODO: what is the bufio scanner's max buffer size? call out in comments supported max event json serialized size
		var event golongpoll.Event
		var line = scanner.Bytes()
		// skip any blank lines as we prepend newline before writing event json.
		// NOTE: doing prepend instead of append so if an event getting written out is
		// stopped prematurely due to an ungraceful shutdown, we will start a subsequent
		// write on it's own line.
		if len(line) == 0 {
			continue
		}
		if err := json.Unmarshal(line, &event); err != nil {
			// TODO: log, do other things?
			fmt.Printf("Failed to unmarshal event json, error: %v\n", err)
			continue
		} else {
			fmt.Printf("DEBUG >> ADDING ON START EVENT ....  boom!")
			ch <- &event
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Printf("Error scanning file: %v\n", err)
		// TODO: log, return error? what?
		//fmt.Fprintln(os.Stderr, "reading standard input:", err)
	}

	close(ch) // NOTE: important to close--or calling LongpollManager will hang waiting for more channel data.
}

func (fp *FilePersistor) run() {
	lastFlushTime := time.Now()
	outFile, err := os.OpenFile(fp.filename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0755) // NOTE: O_APPEND is critical here as was getting weird file data wrapping around across restarts!
	if err != nil {
		fmt.Printf(" ERROR >>>> failed to open file: %v\n", err) // TODO: remove me/replace with something else
		// TODO: do anything else? log? return error? then calling code can check to know cut out early.
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

			eventJson, _ := json.Marshal(event) // TODO: check err and return it? or log error and skip?
			w.WriteString("\n")                 // NOTE: adding newline before instead of after in case last event wasn't fully written out to completion
			w.Write(eventJson)                  // TODO: check returned error (ignore bytes written return?)

			if IsTimeToFlush(lastFlushTime, fp.writeFlushPeriodSeconds) {
				w.Flush()
				lastFlushTime = time.Now()
			}

		case <-time.After(time.Duration(500) * time.Millisecond):
			if IsTimeToFlush(lastFlushTime, fp.writeFlushPeriodSeconds) {
				w.Flush()
				lastFlushTime = time.Now()
			}
		}
	}
}

func IsTimeToFlush(lastFlushTime time.Time, writeFlushPeriodSeconds int) bool {
	diff := time.Now().Sub(lastFlushTime)
	return diff.Seconds() >= float64(writeFlushPeriodSeconds)
}
