package plan

import (
	"container/heap"
	"time"
)

type eventCandidate struct {
	at   time.Time
	ref  string
	plan *Plan
}

func (a eventCandidate) before(b eventCandidate) bool {
	if !a.at.Equal(b.at) {
		return a.at.Before(b.at)
	}
	return a.ref < b.ref
}

// The latest retained candidate is first, so later events can be discarded
// without allocating their public payload. Keep one extra to detect another page.
type eventSelection []eventCandidate

func (h eventSelection) Len() int           { return len(h) }
func (h eventSelection) Less(i, j int) bool { return h[j].before(h[i]) }
func (h eventSelection) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *eventSelection) Push(value any)    { *h = append(*h, value.(eventCandidate)) }
func (h *eventSelection) Pop() any {
	old := *h
	value := old[len(old)-1]
	old[len(old)-1] = eventCandidate{}
	*h = old[:len(old)-1]
	return value
}
func (h *eventSelection) offer(value eventCandidate, limit int) bool {
	if len(*h) < limit {
		heap.Push(h, value)
		return true
	}
	if !value.before((*h)[0]) {
		return false
	}
	(*h)[0] = value
	heap.Fix(h, 0)
	return true
}
