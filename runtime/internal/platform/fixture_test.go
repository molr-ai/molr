package platform_test

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

var helperOnce sync.Once
var helperPath, helperDirectory string
var helperError error

const helperSource = `package main
import("fmt";"os";"os/exec";"os/signal";"strings";"syscall";"time")
func main(){switch os.Args[1]{
case "echo":fmt.Fprint(os.Stdout,"stdout-data");fmt.Fprint(os.Stderr,"stderr-data")
case "literal":fmt.Print(os.Args[len(os.Args)-1])
case "env":fmt.Print(os.Getenv("MOLR_PLATFORM_ALLOWED")+"/"+os.Getenv("MOLR_PLATFORM_PARENT_SECRET"))
case "output":fmt.Fprint(os.Stdout,strings.Repeat("x",16384));fmt.Fprint(os.Stderr,strings.Repeat("y",16384))
case "child": signals:=make(chan os.Signal,1);signal.Notify(signals,syscall.SIGTERM,os.Interrupt);<-signals
case "long":
 child:=exec.Command(os.Args[0],"child");if err:=child.Start();err!=nil{panic(err)}
 signals:=make(chan os.Signal,1);signal.Notify(signals,syscall.SIGTERM,os.Interrupt)
 if err:=os.WriteFile(os.Args[2],[]byte(fmt.Sprint(child.Process.Pid)),0600);err!=nil{panic(err)}
 <-signals
 _ = child.Process.Signal(syscall.SIGTERM)
 done:=make(chan struct{});go func(){_ = child.Wait();close(done)}()
 select{case <-done:case <-time.After(time.Second):_ = child.Process.Kill();<-done}
}}
`

func TestMain(m *testing.M) {
	code := m.Run()
	if helperDirectory != "" {
		_ = os.RemoveAll(helperDirectory)
	}
	os.Exit(code)
}
func helper(t *testing.T) string {
	t.Helper()
	helperOnce.Do(func() {
		helperDirectory, helperError = os.MkdirTemp("", "molr-platform-helper-")
		if helperError != nil {
			return
		}
		source := filepath.Join(helperDirectory, "main.go")
		helperError = os.WriteFile(source, []byte(helperSource), 0600)
		if helperError != nil {
			return
		}
		helperPath = filepath.Join(helperDirectory, "helper")
		if runtime.GOOS == "windows" {
			helperPath += ".exe"
		}
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", helperPath, source)
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
		data, err := cmd.CombinedOutput()
		if err != nil {
			helperError = fmt.Errorf("compile helper: %w: %s", err, data)
		}
	})
	if helperError != nil {
		t.Fatal(helperError)
	}
	return helperPath
}
func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		exe := helper(t)
		directory := t.TempDir()
		pidPath := filepath.Join(directory, "child.pid")
		started := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go func() {
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if b, err := os.ReadFile(pidPath); err == nil && len(b) > 0 {
						close(started)
						return
					}
				}
			}
		}()
		var mu sync.Mutex
		bindings := map[core.ID]platform.NetworkBinding{}
		execution := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "op"}
		runner := platform.NewRunner(platform.RunnerOptions{AllowedExecutables: []string{exe}, Authorize: func(ctx context.Context, e core.Execution, c platform.Command) error { return e.Validate() }})
		guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowedPrivateAddresses: []netip.Addr{netip.MustParseAddr("192.168.10.20")}, Binding: func(ctx context.Context, e core.Execution, id core.ID) (platform.NetworkBinding, error) {
			mu.Lock()
			defer mu.Unlock()
			b, ok := bindings[id]
			if !ok {
				return b, core.NewError(core.Forbidden, "resource not bound")
			}
			return b, nil
		}})
		t.Cleanup(func() {
			if b, err := os.ReadFile(pidPath); err == nil {
				if id, err := strconv.Atoi(string(b)); err == nil {
					if child, err := os.FindProcess(id); err == nil {
						_ = child.Kill()
					}
				}
			}
		})
		return contractFixture{Runner: runner, Locker: platform.NewLocker(), Network: guard, Execution: execution, DataDir: directory, DescendantStarted: started,
			Command: func(mode string) platform.Command {
				args := []string{mode}
				if mode == "long" {
					args = append(args, pidPath)
				}
				return platform.Command{Executable: exe, Args: args, Directory: directory, Timeout: 5 * time.Second, MaxOutputBytes: 32768}
			},
			BindDestination: func(id core.ID, origin string) {
				mu.Lock()
				defer mu.Unlock()
				bindings[id] = platform.NetworkBinding{Origin: origin}
			},
			DescendantsAlive: func() bool {
				b, err := os.ReadFile(pidPath)
				if err != nil {
					return false
				}
				id, err := strconv.Atoi(string(b))
				if err != nil {
					return false
				}
				if runtime.GOOS == "darwin" {
					data, err := exec.Command("/bin/ps", "-p", strconv.Itoa(id), "-o", "stat=").Output()
					state := strings.TrimSpace(string(data))
					return err == nil && state != "" && !strings.HasPrefix(state, "Z")
				}
				data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(id), "stat"))
				if err != nil {
					return false
				}
				parts := strings.Fields(string(data))
				return len(parts) > 2 && parts[2] != "Z"
			},
		}
	}
}
