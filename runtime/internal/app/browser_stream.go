package app

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"golang.org/x/net/websocket"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
)

func (s *shellTasks) browserFrames(ctx context.Context, p core.Principal, id, task core.ID, emit func(json.RawMessage) error) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if err := (access{s.store}).assistant(ctx, p, id); err != nil {
		return err
	}
	r := s.activeBrowser(p, id, task)
	if r == nil {
		return core.NewError(core.NotFound, "browser task unavailable")
	}
	select {
	case s.forwardSlots <- struct{}{}:
		defer func() { <-s.forwardSlots }()
	default:
		return core.NewError(core.RateLimited, "stream limit")
	}
	watch, err := s.identity.WatchPrincipal(p)
	if err != nil {
		return err
	}
	defer watch.Close()
	call, cancel := context.WithCancel(r.browserCtx)
	stopClient := context.AfterFunc(ctx, cancel)
	defer stopClient()
	defer cancel()
	stop := context.AfterFunc(r.browserCtx, cancel)
	defer stop()
	if _, err := watch.Check(call); err != nil {
		return err
	}
	conn, err := s.router.dialForward(call, r.scope.e, "local", browser.StreamPort)
	if err != nil {
		return core.NewError(core.Unavailable, "browser stream is starting or disconnected")
	}
	location, _ := url.Parse("ws://127.0.0.1:9223/?pacing=ack&maxFps=10")
	origin, _ := url.Parse("http://127.0.0.1:9223")
	ws, err := websocket.NewClient(&websocket.Config{Location: location, Origin: origin, Version: 13}, conn)
	if err != nil {
		conn.Close()
		return err
	}
	defer ws.Close()
	ws.MaxPayloadBytes = 12 << 20
	closeOnCancel := context.AfterFunc(call, func() { ws.Close() })
	defer closeOnCancel()
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() {
		defer close(done)
		for {
			left, err := watch.Check(call)
			if err != nil {
				cancel()
				return
			}
			if left <= 0 {
				left = time.Millisecond
			}
			t := time.NewTimer(left)
			select {
			case <-call.Done():
				t.Stop()
				return
			case <-watch.Changes():
				t.Stop()
			case <-t.C:
			}
		}
	}()
	// ACK pacing gives each observer one current frame, without a growing queue.
	for {
		var raw string
		if err := websocket.Message.Receive(ws, &raw); err != nil {
			return err
		}
		var message struct {
			Type string `json:"type"`
			Seq  uint64 `json:"seq"`
		}
		if json.Unmarshal([]byte(raw), &message) != nil {
			continue
		}
		switch message.Type {
		case "frame", "status", "tabs", "url":
			if err := emit(json.RawMessage(raw)); err != nil {
				return err
			}
		}
		if message.Type == "frame" && message.Seq > 0 {
			if err := websocket.JSON.Send(ws, map[string]any{"type": "ack", "seq": message.Seq}); err != nil {
				return err
			}
		}
	}
}
