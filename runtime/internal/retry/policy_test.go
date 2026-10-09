package retry

import (
	"math"
	"testing"
	"time"
)

func TestDefaultPolicy(t *testing.T) {
	p := Policy{}
	for attempt, seconds := range []int{0, 5, 10, 20, 40, 80, 160, 320, 640, 640, 640} {
		if got := p.Delay(attempt); got != time.Duration(seconds)*time.Second {
			t.Fatalf("attempt %d: got %v, want %ds", attempt, got, seconds)
		}
	}
	if p.Delay(math.MaxInt) != 640*time.Second {
		t.Fatal("large attempt count must saturate")
	}
}

func TestPolicyBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		policy  Policy
		attempt int
		want    time.Duration
	}{
		{"model first", Policy{2 * time.Second, 30 * time.Second}, 1, 2 * time.Second},
		{"model cap", Policy{2 * time.Second, 30 * time.Second}, 5, 30 * time.Second},
		{"base above cap", Policy{10 * time.Second, time.Second}, 1, time.Second},
		{"duration overflow", Policy{time.Duration(math.MaxInt64/2 + 1), time.Duration(math.MaxInt64)}, 2, time.Duration(math.MaxInt64)},
		{"no failure", Policy{}, -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.Delay(tc.attempt); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
