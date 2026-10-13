//go:build linux && (amd64 || arm64)

package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/quota"
)

type networkSession struct {
	forwardMu               sync.Mutex
	user, network           *os.File
	group                   *cgroup
	control                 net.Conn
	lease                   *os.File
	keeperDone, gatewayDone chan error
	failure                 chan struct{}
	closed                  atomic.Bool
	once                    sync.Once
	reason                  atomic.Value
	diagnostic              atomic.Value
	err                     error
}

func socketPair() (*os.File, *os.File, error) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(fds[0]), "network-parent"), os.NewFile(uintptr(fds[1]), "network-child"), nil
}

func (n *networkSession) close() error {
	n.once.Do(func() {
		n.closed.Store(true)
		if n.control != nil {
			n.control.Close()
		}
		if n.lease != nil {
			n.lease.Close()
		}
		if n.group != nil {
			n.err = n.group.kill()
		}
		for _, done := range []chan error{n.keeperDone, n.gatewayDone} {
			if done == nil {
				continue
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				n.err = unavailable("sandbox_network_cleanup_failed")
			}
		}
		n.forwardMu.Lock()
		if n.user != nil {
			n.user.Close()
		}
		if n.network != nil {
			n.network.Close()
		}
		n.forwardMu.Unlock()
		if n.group != nil {
			if err := n.group.cleanup(); err != nil {
				n.err = err
			}
		}
	})
	return n.err
}

func (n *networkSession) failed() {
	if !n.closed.Load() {
		select {
		case n.failure <- struct{}{}:
		default:
		}
	}
}
func (n *networkSession) lastReason() string {
	if value := n.reason.Load(); value != nil {
		return value.(string)
	}
	return ""
}

func (r *Runner) startNetwork(ctx context.Context, targets []platform.NetworkTarget, authorize func(context.Context) error) (*networkSession, error) {
	return r.startNetworkWithQuotas(ctx, core.Execution{}, platform.Command{NetworkTargets: targets}, authorize)
}
func (r *Runner) startNetworkWithQuotas(ctx context.Context, e core.Execution, c platform.Command, authorize func(context.Context) error) (_ *networkSession, err error) {
	targets := c.NetworkTargets
	quotaEnabled := len(c.NetworkAuthorizations) == len(targets) && len(targets) > 0
	slots := map[string][]string{}
	var serial uint64
	pending := map[string][]string{}
	exchangeQuota := func(message networkMessage) networkMessage {
		reply := networkMessage{}
		if !quotaEnabled || message.Target < 0 || message.Target >= len(c.NetworkAuthorizations) {
			return reply
		}
		spec := c.NetworkAuthorizations[message.Target]
		account := string(e.Principal.OwnerID) + "/" + string(e.AssistantID) + "/" + spec.Source
		if message.Kind == "release" {
			if key, ok := slots[message.Token]; ok {
				for _, accountKey := range key {
					r.quotas.Release(accountKey, message.Token)
				}
				delete(slots, message.Token)
				reply.Allowed = true
			}
			return reply
		}
		if message.Kind == "settle" {
			if key, ok := pending[message.Token]; ok {
				reply.Allowed = r.quotas.SettleMany(ctx, key, message.Token, message.Bytes) == nil
				if reply.Allowed {
					delete(pending, message.Token)
				}
			}
			return reply
		}
		if r.options.NetworkAuthorization != nil {
			var err error
			spec, err = r.options.NetworkAuthorization(ctx, e, spec)
			if err != nil {
				reply.Reason = "network_authorization_changed"
				return reply
			}
		}
		if spec.ExpiresAt != nil && !time.Now().Before(*spec.ExpiresAt) {
			reply.Reason = "network_authorization_changed"
			return reply
		}
		limits := spec.Constraints
		if limits == nil {
			return reply
		}
		accounts := []quota.AccountLimit{{Key: account, Limit: *limits}}
		keys := []string{account}
		if spec.GroupConstraints != nil {
			if core.ID(spec.GroupSource).Validate() != nil {
				return reply
			}
			groupKey := string(e.Principal.OwnerID) + "/group/" + spec.GroupSource
			accounts = append(accounts, quota.AccountLimit{Key: groupKey, Limit: *spec.GroupConstraints})
			keys = append(keys, groupKey)
		}
		switch message.Kind {
		case "claim":
			serial++
			token := fmt.Sprintf("%s-%d", e.OperationID, serial)
			reply.Allowed = r.quotas.ClaimMany(accounts, token)
			if reply.Allowed {
				slots[token] = keys
				reply.Token = token
			}
		case "reserve":
			serial++
			token := fmt.Sprintf("%s-%d", e.OperationID, serial)
			n, err := r.quotas.ReserveMany(ctx, accounts, token, message.Bytes)
			reply.Allowed = err == nil && n > 0
			reply.Bytes = n
			if reply.Allowed {
				pending[token] = keys
				reply.Token = token
				_, end := policy.NetworkPeriod(*limits, time.Now())
				if spec.GroupConstraints != nil {
					_, groupEnd := policy.NetworkPeriod(*spec.GroupConstraints, time.Now())
					if !groupEnd.IsZero() && (end.IsZero() || groupEnd.Before(end)) {
						end = groupEnd
					}
				}
				if spec.ExpiresAt != nil && (end.IsZero() || spec.ExpiresAt.Before(end)) {
					end = *spec.ExpiresAt
				}
				if !end.IsZero() {
					reply.End = end.UnixNano()
				}
			}
		case "check":
			reply.Allowed = true
			for _, a := range accounts {
				usage, err := r.quotas.Usage(ctx, a.Key, a.Limit)
				if err != nil || usage.UsedBytes >= a.Limit.Bytes || usage.ReservedBytes >= a.Limit.Bytes-usage.UsedBytes {
					reply.Allowed = false
					break
				}
			}
		}
		return reply
	}

	// The network stack handles hostile packets outside the guest namespace.
	// Give it its own hard limits, independently of the shell's resource budget.
	n := &networkSession{failure: make(chan struct{}, 1)}
	ok := false
	defer func() {
		if !ok {
			if cleanupErr := n.close(); cleanupErr != nil {
				// Both Prepare and Start hold the runner mutex during startup.
				r.closed = true
				err = unavailable("sandbox_network_cleanup_failed")
			}
		}
	}()
	n.group, err = newGroup(r.options.CgroupRoot, platform.Limits{MemoryBytes: 256 << 20, Processes: 64, CPUPercent: 100})
	if err != nil {
		return nil, err
	}
	attributes := func() *syscall.SysProcAttr {
		return &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(n.group.file.Fd()), Pdeathsig: syscall.SIGKILL}
	}
	input, lease, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	n.lease = lease
	launcher := r.bwrapCommand("--unshare-user", "--unshare-net", "--uid", "0", "--gid", "0", "--die-with-parent", "--clearenv", "--cap-drop", "ALL", "--ro-bind", "/", "/", "--", r.options.Helper, "sandbox-helper", "network-keeper")
	keeper := exec.Command(launcher[0], launcher[1:]...)
	keeper.Stdin = input
	keeper.Env = []string{"GOMAXPROCS=2"}
	keeper.SysProcAttr = attributes()
	out, err := keeper.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	if err = keeper.Start(); err != nil {
		input.Close()
		return nil, err
	}
	input.Close()
	n.keeperDone = make(chan error, 1)
	go func() { n.keeperDone <- keeper.Wait(); n.failed() }()
	pidResult := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(io.LimitReader(out, 32)).ReadString('\n')
		pidResult <- strings.TrimSpace(line)
	}()
	var pid string
	select {
	case pid = <-pidResult:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, unavailable("sandbox_network_setup_failed")
	}
	if id, e := strconv.Atoi(pid); e != nil || id <= 1 {
		return nil, unavailable("sandbox_network_setup_failed")
	}
	// Pin both namespace descriptors while the fixed trusted keeper is alive.
	// nsenter uses these descriptors, never a later lookup of a recycled PID.
	n.user, err = os.Open("/proc/" + pid + "/ns/user")
	if err != nil {
		return nil, err
	}
	n.network, err = os.Open("/proc/" + pid + "/ns/net")
	if err != nil {
		return nil, err
	}
	owner, err := unix.IoctlRetInt(int(n.network.Fd()), unix.NS_GET_USERNS)
	if err != nil {
		return nil, err
	}
	var want, actual unix.Stat_t
	e1 := unix.Fstat(int(n.user.Fd()), &want)
	e2 := unix.Fstat(owner, &actual)
	unix.Close(owner)
	if e1 != nil || e2 != nil || want.Ino != actual.Ino || want.Dev != actual.Dev {
		return nil, unavailable("sandbox_network_namespace_invalid")
	}
	a, b, err := socketPair()
	if err != nil {
		return nil, err
	}
	connection, err := net.FileConn(a)
	a.Close()
	if err != nil {
		b.Close()
		return nil, err
	}
	setupControl := connection.(*net.UnixConn)
	defer setupControl.Close()
	setupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	setup := exec.CommandContext(setupCtx, "/usr/bin/nsenter", "--user=/proc/self/fd/4", "--net=/proc/self/fd/5", "--preserve-credentials", "--", r.options.Helper, "sandbox-helper", "network-setup")
	setup.ExtraFiles = []*os.File{b, n.user, n.network}
	setup.Env = []string{"GOMAXPROCS=2"}
	setup.SysProcAttr = attributes()
	if err = setup.Run(); err != nil {
		b.Close()
		return nil, unavailable("sandbox_network_setup_failed")
	}
	b.Close()
	setupControl.SetReadDeadline(time.Now().Add(time.Second))
	data, rights := make([]byte, 16), make([]byte, unix.CmsgSpace(4))
	_, size, flags, _, err := setupControl.ReadMsgUnix(data, rights)
	if err != nil {
		return nil, err
	}
	messages, err := unix.ParseSocketControlMessage(rights[:size])
	if err != nil {
		return nil, err
	}
	var received []int
	for _, m := range messages {
		fds, e := unix.ParseUnixRights(&m)
		if e != nil {
			for _, fd := range received {
				unix.Close(fd)
			}
			return nil, e
		}
		received = append(received, fds...)
	}
	if len(received) != 1 || flags&(unix.MSG_CTRUNC|unix.MSG_TRUNC) != 0 {
		for _, fd := range received {
			unix.Close(fd)
		}
		return nil, fmt.Errorf("invalid network descriptor")
	}
	tun := os.NewFile(uintptr(received[0]), "network-tun")
	defer tun.Close()
	deadline, _ := ctx.Deadline()
	connections := r.options.NetworkConnections
	if quotaEnabled {
		connections = 128
	}
	payload, _ := json.Marshal(networkConfig{Quotas: quotaEnabled, Targets: targets, Bytes: r.options.NetworkBytes, Connections: connections, Deadline: deadline})
	config, err := memoryFile(payload)
	if err != nil {
		return nil, err
	}
	defer config.Close()
	a, b, err = socketPair()
	if err != nil {
		return nil, err
	}
	n.control, err = net.FileConn(a)
	a.Close()
	if err != nil {
		b.Close()
		return nil, err
	}
	gateway := exec.Command(r.options.Helper, "sandbox-helper", "network-gateway")
	gateway.Env = []string{"GOMAXPROCS=2"}
	gateway.ExtraFiles = []*os.File{tun, config, b}
	gateway.SysProcAttr = attributes()
	if err = gateway.Start(); err != nil {
		b.Close()
		return nil, err
	}
	b.Close()
	n.gatewayDone = make(chan error, 1)
	go func() { n.gatewayDone <- gateway.Wait(); n.failed() }()
	ready := make(chan bool, 1)
	go func() {
		defer func() {
			for token, key := range slots {
				for _, accountKey := range key {
					r.quotas.Release(accountKey, token)
				}
			}
		}()
		for {
			var message networkMessage
			if readNetworkMessage(n.control, &message) != nil {
				n.failed()
				return
			}
			if message.Kind == "reserve" || message.Kind == "settle" || message.Kind == "claim" || message.Kind == "release" || message.Kind == "check" {
				reply := exchangeQuota(message)
				if writeNetworkMessage(n.control, reply) != nil {
					n.failed()
					return
				}
				continue
			}
			allowed := false
			switch message.Kind {
			case "ready", "authorize":
				check, stop := context.WithTimeout(ctx, 2*time.Second)
				allowed = authorize(check) == nil
				if !allowed {
					n.reason.Store("authorization_changed")
				}
				stop()
			case "notice":
				switch message.Reason {
				case "network_authorization_changed", "network_destination_denied", "network_dns_failed", "network_connection_failed", "network_bytes_exhausted", "network_connection_limit", "network_dns_limit":
					n.reason.Store(message.Reason)
					n.diagnostic.Store(platform.NetworkDiagnostic{Reason: message.Reason, At: time.Now().UTC()})
					allowed = true
				}
			}
			if writeNetworkMessage(n.control, networkMessage{Allowed: allowed}) != nil {
				n.failed()
				return
			}
			if message.Kind == "ready" {
				select {
				case ready <- allowed:
				default:
				}
			}
			if !allowed {
				n.failed()
				return
			}
		}
	}()
	select {
	case allowed := <-ready:
		if !allowed {
			return nil, unavailable("sandbox_network_authorization_failed")
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-n.failure:
		return nil, unavailable("sandbox_network_gateway_failed")
	case <-time.After(5 * time.Second):
		return nil, unavailable("sandbox_network_setup_failed")
	}
	ok = true
	return n, nil
}

func (n *networkSession) lastDiagnostic() platform.NetworkDiagnostic {
	if v := n.diagnostic.Load(); v != nil {
		return v.(platform.NetworkDiagnostic)
	}
	return platform.NetworkDiagnostic{}
}
