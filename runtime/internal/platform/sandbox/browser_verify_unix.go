//go:build (linux && (amd64 || arm64)) || darwin

package sandbox

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"time"
)

// Inspect renderer processes through the private browser endpoint and verify
// their OS sandbox, rather than treating launch arguments as proof.
func verifyBrowserSandbox(ctx context.Context, endpoint string) error {
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 3 * time.Second}).DialContext(ctx, endpoint, nil)
	if err != nil {
		return unavailable("browser_native_sandbox_unavailable")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetReadLimit(1 << 20)
	for id := 1; ; id++ {
		conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if conn.WriteJSON(map[string]any{"id": id, "method": "SystemInfo.getProcessInfo"}) != nil {
			break
		}
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var reply struct {
			ID     int `json:"id"`
			Result struct {
				Processes []struct {
					Type string `json:"type"`
					PID  int    `json:"id"`
				} `json:"processInfo"`
			} `json:"result"`
			Error json.RawMessage `json:"error"`
		}
		for {
			if err = conn.ReadJSON(&reply); err != nil || reply.ID == id {
				break
			}
		}
		if err != nil || len(reply.Error) > 0 {
			break
		}
		renderers := 0
		for _, process := range reply.Result.Processes {
			if process.Type == "renderer" {
				renderers++
				if process.PID <= 1 || !browserRendererSandboxed(ctx, process.PID) {
					return unavailable("browser_native_sandbox_unavailable")
				}
			}
		}
		if renderers > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return unavailable("browser_native_sandbox_unavailable")
		case <-time.After(50 * time.Millisecond):
		}
	}
	return unavailable("browser_native_sandbox_unavailable")
}
