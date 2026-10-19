package qq

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestCheckCredentials(t *testing.T) {
	for _, valid := range []bool{true, false} {
		calls := 0
		h := HTTPClient{client: func(origin string) *http.Client {
			return &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if origin != tokenOrigin || req.URL.Path != "/app/getAppAccessToken" {
					t.Fatal("credential check used wrong endpoint")
				}
				if valid {
					return response(`{"access_token":"fixture-token"}`, 200), nil
				}
				return response(`{}`, 200), nil
			})}
		}}
		var client Client = h
		err := client.Check(t.Context(), Credentials{"123456", "fixture-secret", "user"})
		if calls != 1 || valid && err != nil || !valid && failure(err) != "credentials_required" {
			t.Fatalf("valid=%v calls=%d error=%v", valid, calls, err)
		}
	}
}

func TestListenStopsOnInvalidCredentialsAndCancellation(t *testing.T) {
	for _, reason := range []string{"credentials_required", "cancel"} {
		t.Run(reason, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			calls, disconnected := 0, 0
			h := HTTPClient{client: func(string) *http.Client {
				return &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
					calls++
					if reason == "credentials_required" {
						return response(`{}`, 200), nil
					}
					return response(`{}`, 503), nil
				})}
			}}
			var client Client = h
			err := client.Listen(ctx, Credentials{"123456", "fixture-secret", "user"}, func(Incoming) error {
				t.Error("delivered a message without authentication")
				return nil
			}, func(connected bool) {
				if connected {
					t.Error("reported connected after failed authentication")
				}
				disconnected++
				cancel()
			})
			if calls != 1 {
				t.Fatalf("retried after credentials failure or cancellation: %d", calls)
			}
			if reason == "credentials_required" {
				if failure(err) != reason || disconnected != 0 {
					t.Fatalf("credential failure: %v, notifications=%d", err, disconnected)
				}
			} else if !errors.Is(err, context.Canceled) || disconnected != 1 {
				t.Fatalf("cancellation during reconnect: %v, notifications=%d", err, disconnected)
			}
		})
	}
}

type lifecycleClient struct {
	fakeClient
	started chan struct{}
	stopped chan struct{}
}

func (c *lifecycleClient) Listen(ctx context.Context, _ Credentials, _ func(Incoming) error, state func(bool)) error {
	defer close(c.stopped)
	state(true)
	close(c.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestRunStartsAndJoinsListener(t *testing.T) {
	f := setup(t)
	f.bind(t)
	client := &lifecycleClient{started: make(chan struct{}), stopped: make(chan struct{})}
	f.s.options.Client = client
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			must(t, err)
		case <-time.After(5 * time.Second):
			t.Error("Run did not stop after cancellation")
		}
		select {
		case <-client.stopped:
		default:
			t.Error("Run returned without joining listener")
		}
	})
	select {
	case <-client.started:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not restore the persisted binding")
	}
	status, err := f.s.Status(t.Context(), f.owner, "assistant")
	must(t, err)
	if status.Channel != "qq" || status.State != "connected" {
		t.Fatalf("listener state not persisted: %+v", status)
	}
}
