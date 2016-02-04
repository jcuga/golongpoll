package golongpoll

import (
	"container/heap"
	"container/list"
	"testing"
	"time"
)

func Test_priorityQueue_Len(t *testing.T) {
	pq := make(priorityQueue, 0)
	if pq.Len() != 0 {
		t.Errorf("priorityQueue had unexpected Len().  was: %d, expected: %d",
			pq.Len(), 0)
	}
	// add an item
	now_ms := timeToEpochMilliseconds(time.Now())
	buf := &eventBuffer{
		list.New(),
		100,
		now_ms,
	}
	expiringBuf := &expiringBuffer{
		eventBuffer_ptr: buf,
		category:        "some random category",
		priority:        now_ms,
	}
	heap.Push(&pq, expiringBuf)
	if pq.Len() != 1 {
		t.Errorf("priorityQueue had unexpected Len().  was: %d, expected: %d",
			pq.Len(), 1)
	}
	// add another
	buf2 := &eventBuffer{
		list.New(),
		100,
		now_ms,
	}
	expiringBuf2 := &expiringBuffer{
		eventBuffer_ptr: buf2,
		category:        "some different category",
		priority:        now_ms,
	}
	heap.Push(&pq, expiringBuf2)
	if pq.Len() != 2 {
		t.Errorf("priorityQueue had unexpected Len().  was: %d, expected: %d",
			pq.Len(), 2)
	}
	// Remove an item
	pq.Pop()
	if pq.Len() != 1 {
		t.Errorf("priorityQueue had unexpected Len().  was: %d, expected: %d",
			pq.Len(), 1)
	}
	pq.Pop()
	if pq.Len() != 0 {
		t.Errorf("priorityQueue had unexpected Len().  was: %d, expected: %d",
			pq.Len(), 0)
	}
}

func Test_priorityQueue(t *testing.T) {
	now_ms := timeToEpochMilliseconds(time.Now())
	pq := make(priorityQueue, 0)
	heap.Init(&pq)

	if _, e := pq.peakTopPriority(); e == nil {
		t.Errorf("No error returned when calling peakTopPriority on an empty priorityQueue")
	}

	buf1 := &eventBuffer{
		list.New(),
		100,
		now_ms,
	}
	expiringBuf1 := &expiringBuffer{
		eventBuffer_ptr: buf1,
		category:        "some random category",
		priority:        10003, // lower number is a higher ( priority 1 > priority 2)
	}
	heap.Push(&pq, expiringBuf1)
	if p, e := pq.peakTopPriority(); e != nil {
		t.Errorf("Error returned when calling peakTopPriority: %v", e)
	} else {
		if p != 10003 {
			t.Errorf("Unexpected peakTopPriority result.  was: %d, expected: %d.",
				10003, p)
		}
	}

	buf2 := &eventBuffer{
		list.New(),
		100,
		now_ms,
	}
	expiringBuf2 := &expiringBuffer{
		eventBuffer_ptr: buf2,
		category:        "some random category",
		priority:        10001,
	}
	heap.Push(&pq, expiringBuf2)
	if p, e := pq.peakTopPriority(); e != nil {
		t.Errorf("Error returned when calling peakTopPriority: %v", e)
	} else {
		if p != 10001 {
			t.Errorf("Unexpected peakTopPriority result.  was: %d, expected: %d.",
				10001, p)
		}
	}

	buf3 := &eventBuffer{
		list.New(),
		100,
		now_ms,
	}
	expiringBuf3 := &expiringBuffer{
		eventBuffer_ptr: buf3,
		category:        "some random category",
		priority:        10051,
	}
	heap.Push(&pq, expiringBuf3)
	if p, e := pq.peakTopPriority(); e != nil {
		t.Errorf("Error returned when calling peakTopPriority: %v", e)
	} else {
		if p != 10001 {
			t.Errorf("Unexpected peakTopPriority result.  was: %d, expected: %d.",
				10001, p)
		}
	}

	buf4 := &eventBuffer{
		list.New(),
		100,
		now_ms,
	}
	expiringBuf4 := &expiringBuffer{
		eventBuffer_ptr: buf4,
		category:        "some random category",
		priority:        10011,
	}
	heap.Push(&pq, expiringBuf4)
	if p, e := pq.peakTopPriority(); e != nil {
		t.Errorf("Error returned when calling peakTopPriority: %v", e)
	} else {
		if p != 10001 {
			t.Errorf("Unexpected peakTopPriority result.  was: %d, expected: %d.",
				10001, p)
		}
	}

	if item := heap.Pop(&pq).(*expiringBuffer); item != expiringBuf2 {
		t.Errorf("Expected popped item != expiringBuf2")
	}
	if p, _ := pq.peakTopPriority(); p != 10003 {
		t.Errorf("Unexpected peakTopPriority result.  was: %d, expected: %d.",
			10003, p)
	}

	if item := heap.Pop(&pq).(*expiringBuffer); item != expiringBuf1 {
		t.Errorf("Expected popped item != expiringBuf1")
	}
	if p, _ := pq.peakTopPriority(); p != 10011 {
		t.Errorf("Unexpected peakTopPriority result.  was: %d, expected: %d.",
			10011, p)
	}

	// Now stir the pot by updating expiringBuf3 to higher priority than expiringBuf4
	pq.updatePriority(expiringBuf3, 10008)
	if p, _ := pq.peakTopPriority(); p != 10008 {
		t.Errorf("Unexpected peakTopPriority result.  was: %d, expected: %d.",
			10008, p)
	}

	if item := heap.Pop(&pq).(*expiringBuffer); item != expiringBuf3 {
		t.Errorf("Expected popped item != expiringBuf3")
	}
	if p, _ := pq.peakTopPriority(); p != 10011 {
		t.Errorf("Unexpected peakTopPriority result.  was: %d, expected: %d.",
			10011, p)
	}
	if item := heap.Pop(&pq).(*expiringBuffer); item != expiringBuf4 {
		t.Errorf("Expected popped item != expiringBuf4")
	}

	if _, e := pq.peakTopPriority(); e == nil {
		t.Errorf("No error returned when calling peakTopPriority on an empty priorityQueue")
	}
}
