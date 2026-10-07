package app

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/portforward"
)

type shellForward struct {
	Protocol string
	Port     int
	Address  string
	// URL is a convenience for HTTP servers, not a protocol/readiness claim.
	URL   string
	State string
}
type taskForward struct {
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	once     sync.Once
	slots    chan struct{}
}

func (f *taskForward) close() { f.once.Do(func() { f.cancel(); f.listener.Close() }) }

func (s *shellTasks) forwardHost() (string, error) {
	if h := s.forwardConfig.PublicHost; h != "" {
		return h, nil
	}
	ip := net.ParseIP(s.forwardConfig.ListenHost)
	if ip != nil && !ip.IsUnspecified() {
		return ip.String(), nil
	}
	// For a single-machine LAN installation, prefer an active private IPv4
	// interface. Multi-interface/NAT deployments should set public_host.
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	var fallback string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.To4() == nil || !ip.IsGlobalUnicast() {
				continue
			}
			if ip.IsPrivate() {
				return ip.String(), nil
			}
			if fallback == "" {
				fallback = ip.String()
			}
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", core.NewError(core.FailedPrecondition, "configure port_forward.public_host for a reachable forward address")
}

// Called before recording/starting the command so allocation failures do not
// leave a service running without its requested forwarding addresses.
func (s *shellTasks) prepareForwards(ctx context.Context, r *runningShell, ports []int) error {
	if len(ports) == 0 {
		return nil
	}
	if !s.forwardConfig.Enabled {
		return core.NewError(core.FailedPrecondition, "TCP port forwarding disabled")
	}
	host, err := s.forwardHost()
	if err != nil {
		return err
	}
	for _, port := range ports {
		listener, err := net.Listen("tcp", net.JoinHostPort(s.forwardConfig.ListenHost, "0"))
		if err != nil {
			for _, f := range r.forwards {
				f.close()
			}
			return err
		}
		run, cancel := context.WithCancel(ctx)
		f := &taskForward{listener: listener, ctx: run, cancel: cancel, slots: make(chan struct{}, 32)}
		r.forwards = append(r.forwards, f)
		address := net.JoinHostPort(host, strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
		r.task.Forwards = append(r.task.Forwards, shellForward{Protocol: "tcp", Port: port, Address: address, URL: "http://" + address, State: "waiting"})
	}
	return nil
}
func (s *shellTasks) forwardState(r *runningShell, index int, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.task.State != "running" || r.task.Forwards[index].State == state {
		return
	}
	r.task.Forwards[index].State = state
	_ = s.save(context.Background(), r.task)
}
func (s *shellTasks) serveForward(ctx context.Context, e core.Execution, r *runningShell, index int, f *taskForward) {
	defer s.wg.Done()
	defer f.close()
	stop := context.AfterFunc(f.ctx, func() { f.listener.Close() })
	defer stop()
	select {
	case <-r.ready:
	case <-f.ctx.Done():
		return
	}
	s.mu.Lock()
	id, port := r.task.SandboxID, r.task.Forwards[index].Port
	s.mu.Unlock()
	dial := func(ctx context.Context) (portforward.Stream, error) { return s.router.dialForward(ctx, e, id, port) }
	var wg sync.WaitGroup
	defer func() { f.close(); wg.Wait() }()
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			probe, cancel := context.WithTimeout(f.ctx, 4*time.Second)
			conn, err := dial(probe)
			if err == nil {
				conn.Close()
				s.forwardState(r, index, "ready")
			} else {
				s.forwardState(r, index, "waiting")
			}
			cancel()
			select {
			case <-f.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	for {
		client, err := f.listener.Accept()
		if err != nil {
			return
		}
		select {
		case f.slots <- struct{}{}:
		default:
			client.Close()
			continue
		}
		select {
		case s.forwardSlots <- struct{}{}:
		default:
			<-f.slots
			client.Close()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-s.forwardSlots; <-f.slots }()
			defer client.Close()
			stopClient := context.AfterFunc(f.ctx, func() { client.Close() })
			defer stopClient()
			upstream, err := dial(f.ctx)
			if err != nil {
				return
			}
			defer upstream.Close()
			stopUpstream := context.AfterFunc(f.ctx, func() { upstream.Close() })
			defer stopUpstream()
			portforward.Bridge(client.(*net.TCPConn), upstream)
		}()
	}
}
func (s *sandboxRouter) dialForward(ctx context.Context, e core.Execution, id core.ID, port int) (portforward.Stream, error) {
	if id == "local" {
		if s.shell == nil {
			return nil, core.NewError(core.Unavailable, "local sandbox unavailable")
		}
		conn, err := s.shell.runner.DialForward(ctx, e, port)
		if err != nil {
			return nil, err
		}
		stream, ok := conn.(portforward.Stream)
		if !ok {
			conn.Close()
			return nil, fmt.Errorf("TCP half-close unavailable")
		}
		return stream, nil
	}
	connection, err := s.connection(ctx, e.Principal, id)
	if err != nil {
		return nil, err
	}
	client, err := s.client(ctx, e.Principal, connection)
	if err != nil {
		return nil, err
	}
	return client.DialForward(ctx, identity(e), port)
}
