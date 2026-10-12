package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

// HelperMain runs inside the existing isolated execution. It never executes a
// user-supplied program or shell string and never exports the daemon socket.
func HelperMain() int {
	const binary = "/resources/browser-agent/agent-browser"
	if err := os.MkdirAll("/tmp/molr-browser", 0700); err != nil {
		return 125
	}
	env := []string{"PATH=/usr/bin:/bin", "HOME=/work", "TMPDIR=/tmp", "LANG=C.UTF-8", "AGENT_BROWSER_SOCKET_DIR=/tmp/molr-browser", "AGENT_BROWSER_EXECUTABLE_PATH=/resources/browser-chromium/chrome", "AGENT_BROWSER_PROFILE=/work/.local/state/molr/browser/profile", "AGENT_BROWSER_STREAM_PORT=9223", "AGENT_BROWSER_ARGS=--disable-dev-shm-usage", "AGENT_BROWSER_STREAM_QUALITY=60", "AGENT_BROWSER_STREAM_MAX_WIDTH=1280", "AGENT_BROWSER_STREAM_MAX_HEIGHT=800", "TOKIO_WORKER_THREADS=2", "RAYON_NUM_THREADS=2"}
	if err := os.MkdirAll("/work/.local/state/molr/browser/profile", 0700); err != nil {
		return 125
	}
	run := func(ctx context.Context, args ...string) Result {
		cmd := exec.CommandContext(ctx, binary, append([]string{"--json"}, args...)...)
		cmd.Env = env
		out := &boundedOutput{remaining: 1 << 20}
		cmd.Stdout = out
		cmd.Stderr = out
		err := cmd.Run()
		if ctx.Err() != nil {
			os.Exit(125)
		}
		if err != nil {
			return Result{Error: fmt.Sprintf("browser command failed: %v: %s", err, out.data)}
		}
		if !json.Valid(out.data) {
			return Result{OK: true, Data: mustJSON(string(out.data))}
		}
		var response struct {
			Success bool            `json:"success"`
			Data    json.RawMessage `json:"data"`
			Error   string          `json:"error"`
		}
		if err := json.Unmarshal(out.data, &response); err != nil {
			return Result{Error: "invalid browser response"}
		}
		return Result{OK: response.Success, Data: response.Data, Error: response.Error}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	result := run(ctx, "open", "about:blank")
	cancel()
	if !result.OK {
		fmt.Fprintln(os.Stderr, result.Error)
		return 125
	}
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	result = run(ctx, "set", "viewport", "1280", "800")
	cancel()
	if !result.OK {
		fmt.Fprintln(os.Stderr, result.Error)
		return 125
	}
	// Inspect native isolation before handing the browser to a task. This is a
	// trusted internal navigation, not a public action accepting chrome:// URLs.
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	sandboxStatus := run(ctx, "open", "chrome://sandbox")
	if sandboxStatus.OK {
		sandboxStatus = run(ctx, "get", "text", "body")
	}
	run(ctx, "open", "about:blank")
	cancel()
	var mu sync.Mutex
	var pointerX, pointerY float64
	held := map[string]bool{}
	dispatch := func(ctx context.Context, command map[string]any) Result {
		result, err := daemonCommand(ctx, command)
		// A transport failure leaves the outcome unknown. Terminate the execution;
		// never acknowledge handoff while a daemon operation might still be running.
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(125)
		}
		return result
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ready": true, "sandbox": sandboxStatus})
	})
	mux.HandleFunc("POST /action", func(w http.ResponseWriter, r *http.Request) {
		var a Action
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		d.DisallowUnknownFields()
		if d.Decode(&a) != nil {
			http.Error(w, "invalid action", 400)
			return
		}
		args, err := a.Args()
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		// Disconnect does not cancel an accepted action: the caller must know it has
		// actually settled before handing control to a human. Every action is bounded.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var result Result
		switch a.Action {
		case "pointer":
			event := map[string]string{"move": "mouseMoved", "down": "mousePressed", "up": "mouseReleased"}[a.Event]
			command := map[string]any{"action": "input_mouse", "type": event, "x": a.X, "y": a.Y}
			if a.Event != "move" {
				command["button"] = a.Button
				command["clickCount"] = 1
			}
			result = dispatch(ctx, command)
			if result.OK {
				pointerX, pointerY = a.X, a.Y
				if a.Event == "down" {
					held[a.Button] = true
				}
				if a.Event == "up" {
					delete(held, a.Button)
				}
			}
		case "fill":
			result = dispatch(ctx, map[string]any{"action": "fill", "selector": a.Selector, "value": a.Text})
		case "insert_text":
			result = dispatch(ctx, map[string]any{"action": "inserttext", "text": a.Text})
		default:
			result = run(ctx, args...)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	// This endpoint is only reached by Runtime's trusted control handoff.
	mux.HandleFunc("POST /reset-input", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result := Result{OK: true}
		for button := range held {
			result = dispatch(ctx, map[string]any{"action": "input_mouse", "type": "mouseReleased", "x": pointerX, "y": pointerY, "button": button, "clickCount": 0})
			if !result.OK {
				break
			}
			delete(held, button)
		}
		json.NewEncoder(w).Encode(result)
	})
	server := http.Server{Addr: "127.0.0.1:9224", Handler: mux, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 8 << 10}
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	return 0
}
func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

type boundedOutput struct {
	data      []byte
	remaining int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if n > b.remaining {
		return 0, fmt.Errorf("browser output limit")
	}
	b.remaining -= n
	b.data = append(b.data, p...)
	return n, nil
}

// The pinned native daemon uses newline-delimited JSON on its private socket.
// Typed input bypasses CLI flag parsing and preserves coordinates and Unicode.
func daemonCommand(ctx context.Context, command map[string]any) (Result, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", "/tmp/molr-browser/default.sock")
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	command["id"] = "molr"
	if err = json.NewEncoder(conn).Encode(command); err != nil {
		return Result{}, err
	}
	var response struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
	}
	if err = json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&response); err != nil {
		return Result{}, err
	}
	return Result{OK: response.Success, Data: response.Data, Error: response.Error}, nil
}
