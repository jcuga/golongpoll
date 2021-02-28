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

// TODO: also event TTL or other options to do pruning?
// TODO: does longpoll manager start also enforce TLL stuff on insert?
// TODO: update hook docs in longpoll.go and the impl here that calls out blocking calls and how to unblock via channels/goroutines
// TODO: use this in unit test to make sure follows iface and can be used as advertized

type FilePersistor struct {
	// Filename to use for storing events. The file will be created if it
	// does not already exist.
	filename string
	// How many events to buffer before writing to file.
	// This can be used to avoid excessive writes.
	// Setting to 1 will cause every event to be individually written to file.
	// This means less likelihood of losing data to a program crash, but
	// at the cost of more file writes. Note: if the program shuts down
	// gracefully and calls LonpollManager.Shutdown() then FilePersistor will
	// write all buffered events to file before stopping via the OnShutdown callback.
	writeBatchSize int
	// How often to flush events to file in lieu of hitting the batch size.
	// This is used along with writeBatchSize to ensure events make it
	// to file in the event of a program crash.  All buffered events will be written
	// via OnShutdown if calling code shuts down gracefully and calls LonpollManager.Shutdown().
	writeFlushPeriodSeconds int

	// channel for incoming published events.
	// To avoid blocking calling LongpollManager when it calls
	// OnPublish, we send events to channel for processing in a separate goroutine.
	publishedEvents chan golongpoll.Event
}

// Create a new FilePersistor with the desired options.
// filename is the file to write events to, and read back out of on longpoll start.
// writeBatchSize dictates how many events are buffered before written to file.
// writeFlushPeriodSeconds is the max amount of seconds elapse between flushing events
// to file in the event batch size has not been reached yet.
func NewFilePersistor(filename string, writeBatchSize int, writeFlushPeriodSeconds int) (*FilePersistor, error) {
	if writeBatchSize < 1 {
		return nil, errors.New("writeBatchSize must be > 0")
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

	eventsIn := make(chan golongpoll.Event, 100)

	fp := FilePersistor{
		filename:                filename,
		writeBatchSize:          writeBatchSize,
		writeFlushPeriodSeconds: writeFlushPeriodSeconds,
		publishedEvents:         eventsIn,
	}

	// TODO: launch goroutine with run func either here or on longpoll start
	go func() {
		lastFlushTime := time.Now()
		buf := make([]*golongpoll.Event, 0, writeBatchSize) // len=0, cap=writeBatchSize

		outFile, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0755)
		if err != nil {
			fmt.Printf(" ERROR >>>> failed to open file: %v\n", err) // TODO: remove me
			// TODO: do anything else? log? return error? then calling code can check to know cut out early.
			return
		}
		defer outFile.Close()
		w := bufio.NewWriter(outFile)
		defer w.Flush()

		fmt.Print(" DEBUG >>> started goroutine select")
		for {
			select {
			case event, ok := <-fp.publishedEvents:
				if !ok {
					fmt.Printf(" DEBUG >>>> got channel close, stoppings\n") // TODO: remove me
					// channel closed, flush any buffered data to disk and stop
					FlushToDisk(buf, w)
					return
				}
				fmt.Printf(" DEBUG >>>> adding to bufer\n") // TODO: remove me
				buf = append(buf, &event)
				if len(buf) >= writeBatchSize {
					FlushToDisk(buf, w)
					buf = nil // resets to empty slice. old data is free to be garbage collected
					lastFlushTime = time.Now()
				} else {
					if FlushIfTimeReached(lastFlushTime, writeFlushPeriodSeconds, buf, w) {
						lastFlushTime = time.Now()
						buf = nil
					}
				}
			case <-time.After(time.Duration(250) * time.Millisecond):
				if FlushIfTimeReached(lastFlushTime, writeFlushPeriodSeconds, buf, w) {
					lastFlushTime = time.Now()
					buf = nil
				}
			}
		}
	}()

	return &fp, nil
}

// Checks if configured amount of time has passed to merit a flush to disk.
func FlushIfTimeReached(lastFlushTime time.Time, writeFlushPeriodSeconds int, buf []*golongpoll.Event, w *bufio.Writer) bool {
	diff := time.Now().Sub(lastFlushTime)
	if diff.Seconds() >= float64(writeFlushPeriodSeconds) {
		FlushToDisk(buf, w)
		return true
	}

	return false
}

// TODO: update to return error if writes fail, handle higher up
func FlushToDisk(buf []*golongpoll.Event, w *bufio.Writer) {
	// TODO: confirm marshalled json escapes any newlines within inner fields so that each one is guaranteed to be on own line
	fmt.Printf(" DEBUG >>>> calling flush to disk\n") // TODO: remove me
	for _, event := range buf {
		eventJson, _ := json.Marshal(event) // TODO: check err and return it? or log error and skip?
		w.Write(eventJson)                  // TODO: check returned error (ignore bytes written return?)
		w.WriteString("\n")
		w.Flush() // TODO: just use bufio.Writer to begin with and don't buffer items here?
	}
}

func (fp *FilePersistor) OnPublish(event golongpoll.Event) {
	// Event handled in a separate goroutine so calling LongpollManager doesn't block
	// waiting for us to write to file.
	fp.publishedEvents <- event
}

// TODO: way to convey back that done doing shutdown cleanup? otherwise would have to arbitrarily wait...
// TODO: perhaps accepts a channel that can write to once done? or returns a channel?
// TODO: update longpoll Shutdown() to return channel to let caller know when shutdown is finished? or block until addon finishes? or both?
func (fp *FilePersistor) OnShutdown() {
	// Signal to stop working and finish up.
	close(fp.publishedEvents)
	// TODO: add plumbing to wait for other goroutine to signal done writing stuff out
	// TODO: block on this call until then?
	// TODO: lpmanager shutdown should wait for this to finsih--and possibly wait for its own internal routines to finish too if not already doing that...
}

// TODO: enforce TTL? probably not here...
func (fp *FilePersistor) OnLongpollStart() <-chan golongpoll.Event {
	ch := make(chan golongpoll.Event)

	go func() {
		// NOTE: https://golang.org/src/bufio/scan.go?s=3956:3993#L77
		// SEE: MaxScanTokenSize is set to 64kb
		// so any event that, when represented as JSON, is larger than
		// 64 kb, it would not fit in the scanner's buffer.
		// To handle larger data, need to provdie an explicit buffer with Scanner.Buffer.
		// TODO: option for providing this? or option to auto create size? maybe make
		// a variant of this add-on for custom large size buffer.
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
			var event golongpoll.Event
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				// TODO: log, do other things?
				fmt.Printf("Failed to unmarshal event json, error: %v\n", err)
				continue
			} else {
				fmt.Printf("DEBUG >> ADDING ON START EVENT ....  boom!")
				ch <- event
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Printf("Error scanning file: %v\n", err)
			// TODO: log, return error? what?
			//fmt.Fprintln(os.Stderr, "reading standard input:", err)
		}

		close(ch) // NOTE: important to close--or calling LongpollManager will hang waiting for more channel data.
	}()
	return ch
}
