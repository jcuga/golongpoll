package golongpoll

import (
	"testing"
	"time"
)

func Test_TrivialAddOn(t *testing.T) {
	var tad AddOn
	tad = &TrivialAddOn{}

	ch := tad.OnLongpollStart()
	select {
	case e, ok := <-ch:
		if ok {
			t.Fatal("Expected closed channel")
		}
		if e != nil {
			t.Fatal("Expected nil data with closed channel")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Should have an immediately closed channel.")
	}

	tad.OnPublish(nil)
	tad.OnShutdown()
}
