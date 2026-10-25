package shellsession

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestBrowserStreamClosesDuringHandshake(t *testing.T) {
	for _, cancelRequest := range []bool{true, false} {
		name := "timeout"
		if cancelRequest {
			name = "canceled"
		}
		t.Run(name, func(t *testing.T) {
			client, peer := net.Pipe()
			defer peer.Close()
			accepted := make(chan struct{})
			peerClosed := make(chan struct{})
			go func() {
				defer close(peerClosed)
				_, _ = http.ReadRequest(bufio.NewReader(peer))
				close(accepted)
				_, _ = io.Copy(io.Discard, peer)
			}()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- relayBrowserFrames(ctx, client, func(json.RawMessage) error { t.Error("unexpected frame"); return nil })
			}()
			<-accepted
			if cancelRequest {
				cancel()
			}
			limit := time.Second
			if !cancelRequest {
				limit = 6 * time.Second
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("handshake unexpectedly succeeded")
				}
			case <-time.After(limit):
				client.Close()
				t.Fatal("handshake did not release connection")
			}
			select {
			case <-peerClosed:
			case <-time.After(time.Second):
				t.Fatal("peer still connected")
			}
		})
	}
}

func TestBrowserStreamACKAndCancellation(t *testing.T) {
	ack := make(chan uint64, 1)
	closed := make(chan struct{})
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer close(closed)
		_ = websocket.Message.Send(ws, `{"type":"frame","seq":7,"data":"fixture"}`)
		var message struct {
			Seq uint64 `json:"seq"`
		}
		if websocket.JSON.Receive(ws, &message) == nil {
			ack <- message.Seq
		}
		var extra string
		_ = websocket.Message.Receive(ws, &extra)
	}))
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	need(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- relayBrowserFrames(ctx, conn, func(raw json.RawMessage) error {
			if !bytes.Contains(raw, []byte(`"seq":7`)) {
				return errors.New("wrong frame")
			}
			return nil
		})
	}()
	select {
	case seq := <-ack:
		if seq != 7 {
			t.Fatal(seq)
		}
	case <-time.After(time.Second):
		t.Fatal("frame not acknowledged")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream still running")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("upstream still connected")
	}
}

func TestBrowserHTTPReusesConnectionsAndClosesOnTaskExit(t *testing.T) {
	var dials atomic.Int32
	closed := make(chan struct{}, 8)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closed <- struct{}{}
		}
	}
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := newBrowserHTTPClient(ctx, func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	})
	for i := 0; i < 32; i++ {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, bytes.NewBufferString(`{"action":"pointer"}`))
		response, err := client.Do(req)
		need(t, err)
		_, err = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		need(t, err)
	}
	if dials.Load() != 1 {
		t.Fatalf("32 inputs opened %d connections", dials.Load())
	}
	cancel()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("idle task connection leaked")
	}
}

func TestBrowserHTTPNeverReplaysAcceptedInput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := newBrowserHTTPClient(ctx, (&net.Dialer{}).DialContext)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, bytes.NewBufferString(`{"action":"insert_text","text":"once"}`))
		response, err := client.Do(req)
		if i == 1 {
			if err == nil {
				response.Body.Close()
				t.Fatal("lost response reported success")
			}
			break
		}
		need(t, err)
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
	if calls.Load() != 2 {
		t.Fatalf("accepted input replayed: %d requests", calls.Load())
	}
}
