package golongpoll

import (
	"testing"
	"time"
)

func Test_millisecondStringToTime(t *testing.T) {
	inputs := []string{
		"0",
		"1429972200000",
		"1446508745000",
	}
	type tePair struct {
		Time  time.Time
		Error error
	}
	expectedOutputs := []tePair{
		{time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC), nil},
		{time.Date(2015, time.April, 25, 14, 30, 0, 0, time.UTC), nil},
		{time.Date(2015, time.November, 2, 23, 59, 5, 0, time.UTC), nil},
	}
	for index, input := range inputs {
		actualTime, actualError := millisecondStringToTime(input)
		if actualTime != expectedOutputs[index].Time ||
			actualError != expectedOutputs[index].Error {
			t.Errorf("Expected (%q, %q), got (%q, %q).",
				expectedOutputs[index].Time, expectedOutputs[index].Error,
				actualTime, actualError)
		}
	}
}

func Test_millisecondStringToTime_InvalidInput(t *testing.T) {
	inputs := []string{
		"",
		"0a",
		"a0",
		"-adsfjkl",
		"  ",
		"\t\b",
	}
	type tsPair struct {
		Time        time.Time
		ErrorString string
	}
	expectedOutputs := []tsPair{
		{time.Time{}, "strconv.ParseInt: parsing \"\": invalid syntax"},
		{time.Time{}, "strconv.ParseInt: parsing \"0a\": invalid syntax"},
		{time.Time{}, "strconv.ParseInt: parsing \"a0\": invalid syntax"},
		{time.Time{}, "strconv.ParseInt: parsing \"-adsfjkl\": invalid syntax"},
		{time.Time{}, "strconv.ParseInt: parsing \"  \": invalid syntax"},
		{time.Time{}, "strconv.ParseInt: parsing \"\\t\\b\": invalid syntax"},
	}
	for index, input := range inputs {
		actualTime, actualError := millisecondStringToTime(input)
		if actualTime != expectedOutputs[index].Time || actualError.Error() !=
			expectedOutputs[index].ErrorString {
			t.Errorf("Expected (%q, %q), got (%q, %q).",
				expectedOutputs[index].Time,
				expectedOutputs[index].ErrorString,
				actualTime, actualError.Error())
		}
	}
}

func Test_timeToEpochMilliseconds(t *testing.T) {
	inputs := []time.Time{
		time.Date(1969, time.December, 31, 23, 59, 59, 0, time.UTC),
		time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2015, time.April, 25, 14, 30, 0, 0, time.UTC),
		time.Date(2015, time.November, 2, 23, 59, 5, 0, time.UTC),
	}
	expectedOutputs := []int64{
		-1000,
		0,
		1429972200000,
		1446508745000,
	}
	for index, input := range inputs {
		actual := timeToEpochMilliseconds(input)
		if actual != expectedOutputs[index] {
			t.Errorf("Expected %d, got %d.", expectedOutputs[index], actual)
		}
	}
}
