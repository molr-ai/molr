package runtimeclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Event signals snapshot invalidation or a change in connection state.
type Event struct {
	Connected bool
	Err       error
}

// Watch resumes committed events after reconnect. A ready handshake always
// invalidates snapshots, including the first subscription, closing the read race.
func (c *Client) Watch(ctx context.Context, notify func(Event)) {
	cursor := ""
	delay := time.Second
	for ctx.Err() == nil {
		err := c.stream(ctx, &cursor, func() { delay = time.Second; notify(Event{Connected: true}) })
		if ctx.Err() != nil {
			return
		}
		notify(Event{Err: err})
		var apiErr *Error
		pause := delay
		if errors.As(err, &apiErr) {
			if apiErr.Status == 401 || apiErr.Status == 403 {
				return
			}
			if apiErr.Status == 400 {
				cursor = ""
			}
			if apiErr.RetryAfter > pause {
				pause = apiErr.RetryAfter
			}
		}
		timer := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(30*time.Second, delay*2)
	}
}

func (c *Client) stream(ctx context.Context, cursor *string, changed func()) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchdog := time.AfterFunc(20*time.Second, cancel)
	defer watchdog.Stop()
	req, err := c.request(ctx, "GET", "/api/events/stream", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if *cursor != "" {
		req.Header.Set("Last-Event-ID", *cursor)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("实时连接中断，正在重连")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return responseError(resp)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return errors.New("Runtime 未返回事件流")
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	event, id, data := "", "", ""
	ready := false
	for scanner.Scan() {
		if ready {
			watchdog.Reset(45 * time.Second)
		}
		line := scanner.Text()
		if line == "" {
			if event == "ready" || event == "update" {
				if id != "" {
					*cursor = id
				}
				if event == "ready" {
					ready = true
					watchdog.Reset(45 * time.Second)
				}
				var visible struct{ Topic string }
				_ = json.Unmarshal([]byte(data), &visible)
				if event == "ready" || visible.Topic == "" || strings.HasPrefix(visible.Topic, "message.") || strings.HasPrefix(visible.Topic, "assistant.") || visible.Topic == "permission.request" || visible.Topic == "conversation.read" {
					changed()
				}
			}
			event, id, data = "", "", ""
		} else if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(line[6:])
		} else if strings.HasPrefix(line, "data:") {
			data += strings.TrimSpace(line[5:])
		} else if strings.HasPrefix(line, "id:") {
			id = strings.TrimSpace(line[3:])
		}
	}
	if err := scanner.Err(); err != nil {
		return errors.New("实时连接中断，正在重连")
	}
	return errors.New("实时连接中断，正在重连")
}
