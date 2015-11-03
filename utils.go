package longpoll

import (
	"strconv"
	"time"
)

// adapted from:
// http://stackoverflow.com/questions/13294649/how-to-parse-a-milliseconds-since-epoch-timestamp-string-in-go
func millisecondStringToTime(ms string) (time.Time, error) {
	msInt, err := strconv.ParseInt(ms, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, msInt*int64(time.Millisecond)).In(time.UTC), nil
}

// adapted from:
// http://stackoverflow.com/questions/24122821/go-golang-time-now-unixnano-convert-to-milliseconds
func timeToEpochMilliseconds(t time.Time) int64 {
	return t.UnixNano() / int64(time.Millisecond)
}
