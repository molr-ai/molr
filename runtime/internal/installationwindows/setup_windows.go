// Package installationwindows owns per-user Windows desktop Runtime tasks.
package installationwindows

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/platform"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type build struct{ Version, Commit, OS, Arch string }
type record struct {
	Manager   string `json:"manager"`
	System    bool   `json:"system"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	DataDir   string `json:"data_dir"`
	Version   string `json:"version"`
	Binary    string `json:"binary"`
	URL       string `json:"url"`
	Paths     struct {
		Unit string `json:"unit"`
	} `json:"paths"`
}
type result struct {
	Type      string   `json:"type"`
	Installed bool     `json:"installed"`
	State     string   `json:"state"`
	Health    string   `json:"health,omitempty"`
	Version   string   `json:"version,omitempty"`
	URL       string   `json:"url,omitempty"`
	Record    string   `json:"record,omitempty"`
	LoginURL  string   `json:"loginURL,omitempty"`
	Missing   []string `json:"missing,omitempty"`
}

func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	tmp := path + ".tmp"
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
func command(ctx context.Context, exe string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, exe, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	b, e := c.CombinedOutput()
	if e != nil {
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(exe), e, strings.TrimSpace(string(b)))
	}
	return b, nil
}
func escaped(s string) string {
	var out bytes.Buffer
	xml.EscapeText(&out, []byte(s))
	return out.String()
}
func taskXML(r record, configPath, sid string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Triggers><LogonTrigger><Enabled>true</Enabled><UserId>` + escaped(sid) + `</UserId></LogonTrigger></Triggers><Principals><Principal id="Owner"><UserId>` + escaped(sid) + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><StartWhenAvailable>true</StartWhenAvailable></Settings><Actions Context="Owner"><Exec><Command>` + escaped(r.Binary) + `</Command><Arguments>` + escaped("serve --config "+syscall.EscapeArg(configPath)+" --log-file "+syscall.EscapeArg(filepath.Join(filepath.Dir(configPath), "runtime.log"))) + `</Arguments><WorkingDirectory>` + escaped(filepath.Dir(r.Binary)) + `</WorkingDirectory></Exec></Actions></Task>`
}
func currentSID() (string, error) {
	t, e := windows.OpenCurrentProcessToken()
	if e != nil {
		return "", e
	}
	defer t.Close()
	u, e := t.GetTokenUser()
	if e != nil {
		return "", e
	}
	return u.User.Sid.String(), nil
}
func stop(ctx context.Context, r record) error {
	if !platform.ServiceRunning(r.DataDir) {
		return nil
	}
	if e := platform.StopService(r.DataDir); e != nil {
		return e
	}
	deadline := time.Now().Add(40 * time.Second)
	for platform.ServiceRunning(r.DataDir) {
		if time.Now().After(deadline) {
			return fmt.Errorf("Runtime did not finish graceful shutdown; installation unchanged")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}
func waitReady(ctx context.Context, r record) error {
	client := http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, "GET", r.URL+"/", nil)
		response, e := client.Do(req)
		if e == nil {
			response.Body.Close()
			if response.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("Runtime HTTP startup deadline exceeded; inspect the task and retained data")
}
func copyFile(from, to string) error {
	in, e := os.Open(from)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e == nil {
		e = ce
	}
	return e
}

func Run(ctx context.Context, args []string, out, diagnostics io.Writer) (resultErr error) {
	if len(args) < 2 || args[0] != "desktop" {
		return fmt.Errorf("Windows installer supports desktop <status|plan|install|start|stop|restart|upgrade|uninstall>")
	}
	action := args[1]
	switch action {
	case "status", "plan", "install", "start", "stop", "restart", "upgrade", "uninstall":
	default:
		return fmt.Errorf("unknown desktop action")
	}
	flags := flag.NewFlagSet("desktop", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	name := flags.String("name", "", "instance name")
	rootArg := flags.String("program-root", "", "private program root")
	preset := flags.String("resource-preset", "least-privilege", "initial resource preset")
	yes := flags.Bool("yes", false, "confirm changes")
	flags.Bool("json", false, "")
	flags.Bool("non-interactive", false, "")
	flags.Bool("no-browser", false, "")
	if e := flags.Parse(args[2:]); e != nil {
		return e
	}
	if flags.NArg() != 0 || !regexp.MustCompile(`^molr-desktop-[a-f0-9]{16}$`).MatchString(*name) || !filepath.IsAbs(*rootArg) {
		return fmt.Errorf("valid desktop name and absolute program root required")
	}
	if action != "status" && action != "plan" && !*yes {
		return fmt.Errorf("--yes required")
	}
	if *preset != "least-privilege" && *preset != "permissive" {
		return fmt.Errorf("invalid resource preset")
	}
	root := filepath.Clean(*rootArg)
	recordPath := filepath.Join(root, "installation.json")
	configPath := filepath.Join(root, "server.json")
	r := record{Manager: "desktop", Name: *name, DataDir: filepath.Join(root, "data")}
	r.Paths.Unit = filepath.Join(root, "task.xml")
	b, e := os.ReadFile(recordPath)
	initialRecord := append([]byte(nil), b...)
	if e == nil {
		if e = json.Unmarshal(b, &r); e != nil {
			return e
		}
		if r.Manager != "desktop" || r.Name != *name || r.DataDir != filepath.Join(root, "data") || r.Paths.Unit != filepath.Join(root, "task.xml") || !strings.HasPrefix(strings.ToLower(r.Binary), strings.ToLower(root+string(filepath.Separator)+"versions"+string(filepath.Separator))) {
			return fmt.Errorf("installation ownership mismatch")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	snapshot := func() result {
		v := result{Type: "desktop", Installed: r.Installed, State: "missing", Version: r.Version, URL: r.URL, Record: recordPath}
		if r.Installed {
			v.State = "inactive"
			if platform.ServiceRunning(r.DataDir) {
				v.State = "running"
				v.Health = "verification_pending"
			}
		}
		return v
	}
	if action == "status" {
		return json.NewEncoder(out).Encode(snapshot())
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	packageRoot := filepath.Dir(filepath.Dir(exe))
	var release build
	if action == "plan" || action == "install" || action == "upgrade" {
		b, e = os.ReadFile(filepath.Join(packageRoot, "release.json"))
		if e != nil {
			return e
		}
		if e = json.Unmarshal(b, &release); e != nil {
			return e
		}
		if release.OS != "windows" || release.Arch != runtime.GOARCH || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`).MatchString(release.Version) || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(release.Commit) {
			return fmt.Errorf("bundled Runtime identity mismatch")
		}
	}
	if action == "plan" {
		return json.NewEncoder(out).Encode(result{Type: "desktop", State: "planned"})
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		return e
	}
	if e = platform.PrivateDirectory(root); e != nil {
		return e
	}
	lock, e := platform.NewLocker().Acquire(ctx, root)
	if e != nil {
		return e
	}
	defer lock.Close()
	currentRecord, readErr := os.ReadFile(recordPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if !bytes.Equal(initialRecord, currentRecord) {
		return fmt.Errorf("installation changed since inspection; retry")
	}
	if action == "install" && r.Installed {
		return fmt.Errorf("Runtime already installed")
	}
	if action != "install" && !r.Installed {
		return fmt.Errorf("installed Runtime required")
	}
	// Validate the registered task before arming upgrade rollback. A rejected
	// ownership check must never stop or overwrite a different scheduled task.
	if r.Installed {
		if e = verifyTask(ctx, r, configPath); e != nil {
			return e
		}
	}
	first := false
	if action == "upgrade" {
		previous := r
		previousTask, err := os.ReadFile(r.Paths.Unit)
		if err != nil {
			return err
		}
		wasRunning := platform.ServiceRunning(r.DataDir)
		defer func() {
			if resultErr == nil {
				return
			}
			rollbackCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			rollback := func() error {
				if err := stop(rollbackCtx, r); err != nil {
					return err
				}
				if err := os.WriteFile(previous.Paths.Unit, previousTask, 0600); err != nil {
					return err
				}
				if _, err := scheduler(rollbackCtx, "/Create", "/TN", previous.Name, "/XML", previous.Paths.Unit, "/F"); err != nil {
					return err
				}
				if err := writeJSON(recordPath, previous); err != nil {
					return err
				}
				if wasRunning {
					if _, err := scheduler(rollbackCtx, "/Run", "/TN", previous.Name); err != nil {
						return err
					}
					return waitReady(rollbackCtx, previous)
				}
				return nil
			}
			if err := rollback(); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("previous Runtime recovery failed: %w", err))
			}
		}()
	}
	if action == "stop" || action == "restart" || action == "upgrade" || action == "uninstall" {
		if e = stop(ctx, r); e != nil {
			return e
		}
	}
	if action == "uninstall" {
		if _, e = scheduler(ctx, "/Delete", "/TN", *name, "/F"); e != nil {
			return e
		}
		if e = os.Remove(r.Paths.Unit); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		r.Installed = false
		if e = writeJSON(recordPath, r); e != nil {
			return e
		}
		if e = os.RemoveAll(filepath.Join(root, "versions")); e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(snapshot())
	}
	if action == "install" || action == "upgrade" {
		versionDir := filepath.Join(root, "versions", release.Version+"-"+release.Commit[:12])
		if e = os.MkdirAll(versionDir, 0700); e != nil {
			return e
		}
		binary := filepath.Join(versionDir, "molr.exe")
		if _, e = os.Stat(binary); errors.Is(e, os.ErrNotExist) {
			if e = copyFile(filepath.Join(packageRoot, "bin", "molr.exe"), binary); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		identity, e := command(ctx, binary, "version", "--json")
		if e != nil {
			return e
		}
		var actual build
		if e = json.Unmarshal(identity, &actual); e != nil {
			return e
		}
		if actual.Version != release.Version || actual.Commit != release.Commit || actual.OS != release.OS || actual.Arch != release.Arch {
			return fmt.Errorf("installed binary does not match release")
		}
		r.Binary = binary
		r.Version = release.Version
		if _, e = os.Stat(configPath); errors.Is(e, os.ErrNotExist) {
			listener, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				return e
			}
			address := listener.Addr().String()
			listener.Close()
			cfg := config.Defaults(r.DataDir)
			cfg.HTTP.Listen = address
			cfg.HTTP.PublicURL = "http://" + address
			cfg.HTTP.TLSMode = config.LocalHTTP
			cfg.PortForward.ListenHost = "127.0.0.1"
			cfg.Updates.AutoCheck = false
			cfg.Sandbox, e = config.SandboxPreset(*preset)
			if e != nil {
				return e
			}
			if e = cfg.Validate(); e != nil {
				return e
			}
			if e = writeJSON(configPath, cfg); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		b, e = os.ReadFile(configPath)
		if e != nil {
			return e
		}
		cfg, e := config.Load(bytes.NewReader(b))
		if e != nil {
			return e
		}
		if cfg.DataDir != r.DataDir || cfg.HTTP.TLSMode != config.LocalHTTP {
			return fmt.Errorf("desktop Runtime requires owned data and loopback HTTP")
		}
		r.URL = cfg.HTTP.PublicURL
		bootstrap, e := command(ctx, r.Binary, "bootstrap", "--config", configPath)
		if e != nil {
			return e
		}
		first = needsInitialLogin(action, initialRecord, bootstrap)
		fmt.Fprint(diagnostics, string(bootstrap))
		sid, e := currentSID()
		if e != nil {
			return e
		}
		if e = os.WriteFile(r.Paths.Unit, taskXMLFile(taskXML(r, configPath, sid)), 0600); e != nil {
			return e
		}
		if _, e = scheduler(ctx, "/Create", "/TN", *name, "/XML", r.Paths.Unit, "/F"); e != nil {
			return e
		}
		r.Installed = true
		if e = writeJSON(recordPath, r); e != nil {
			return e
		}
	}
	if action != "stop" {
		if !platform.ServiceRunning(r.DataDir) {
			if _, e = scheduler(ctx, "/Run", "/TN", *name); e != nil {
				return e
			}
		}
		if e = waitReady(ctx, r); e != nil {
			return e
		}
	}
	result := snapshot()
	if first {
		password, e := os.ReadFile(filepath.Join(r.DataDir, "initial-password"))
		if e != nil {
			return e
		}
		defer clear(password)
		call := func(action string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, r.Binary, action, "--config", configPath, "--password-stdin")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			cmd.Stdin = bytes.NewReader(password)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			b, e := cmd.Output()
			if e != nil {
				return nil, fmt.Errorf("initial %s verification failed: %s", action, stderr.String())
			}
			return b, nil
		}
		if _, e = call("doctor"); e != nil {
			return e
		}
		result.Health = "verified"
		link, e := call("access")
		if e != nil {
			return e
		}
		result.LoginURL = strings.TrimSpace(string(link))
	}
	return json.NewEncoder(out).Encode(result)
}

func verifyTask(ctx context.Context, r record, configPath string) error {
	raw, err := scheduler(ctx, "/Query", "/TN", r.Name, "/XML")
	if err != nil {
		return err
	}
	sid, err := currentSID()
	if err != nil {
		return err
	}
	return compareTaskIdentity(raw, []byte(taskXML(r, configPath, sid)))
}
