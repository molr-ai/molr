//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/platform"
)

type networkConfig struct {
	Quotas      bool
	Targets     []platform.NetworkTarget
	Bytes       int64
	Connections int
	Deadline    time.Time
}
type networkMessage struct {
	End     int64
	Target  int
	Bytes   int64
	Token   string
	Kind    string
	Reason  string
	Allowed bool
}

func networkHelper(mode string) int {
	if unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}) != nil || unix.Setrlimit(unix.RLIMIT_NOFILE, &unix.Rlimit{Cur: 512, Max: 512}) != nil {
		return 125
	}
	switch mode {
	case "network-keeper":
		fmt.Fprintln(os.Stdout, os.Getpid())
		_, err := io.Copy(io.Discard, os.Stdin)
		if err != nil {
			return 125
		}
		return 0
	case "network-setup":
		if err := setupNetworkDevice(); err != nil {
			fmt.Fprintln(os.Stderr, "network setup:", err)
			return 125
		}
		return 0
	case "network-gateway":
		if err := serveNetworkGateway(); err != nil {
			fmt.Fprintln(os.Stderr, "network gateway:", err)
			return 125
		}
		return 0
	}
	return 125
}

func serveNetworkGateway() error {
	debug.SetMemoryLimit(160 << 20)
	var cfg networkConfig
	config := os.NewFile(4, "network-config")
	err := json.NewDecoder(io.LimitReader(config, (1<<20)+1)).Decode(&cfg)
	config.Close()
	if err != nil {
		return err
	}
	if len(cfg.Targets) > 32 || cfg.Bytes < 1024 || cfg.Bytes > 1<<30 || cfg.Connections < 1 || cfg.Connections > 128 || (!cfg.Deadline.IsZero() && !cfg.Deadline.After(time.Now())) {
		return fmt.Errorf("invalid network limits")
	}
	f := os.NewFile(5, "network-control")
	control, err := net.FileConn(f)
	f.Close()
	if err != nil {
		return err
	}
	defer control.Close()
	ctx, cancel := context.WithCancel(context.Background())
	if !cfg.Deadline.IsZero() {
		cancel()
		ctx, cancel = context.WithDeadline(context.Background(), cfg.Deadline)
	}
	defer cancel()

	var mu sync.Mutex
	exchangeReply := func(message networkMessage) networkMessage {
		mu.Lock()
		defer mu.Unlock()
		control.SetDeadline(time.Now().Add(5 * time.Second))
		if writeNetworkMessage(control, message) != nil {
			cancel()
			return networkMessage{}
		}
		var reply networkMessage
		if readNetworkMessage(control, &reply) != nil {
			cancel()
			return networkMessage{}
		}
		return reply
	}
	exchange := func(message networkMessage) bool { return exchangeReply(message).Allowed }
	gateway, err := newNetworkGateway(ctx, cfg, func() bool { return exchange(networkMessage{Kind: "authorize"}) }, func(reason string) { exchange(networkMessage{Kind: "notice", Reason: reason}) })
	if err != nil {
		return err
	}
	if cfg.Quotas {
		gateway.exchange = exchangeReply
	}
	defer gateway.close()
	if err = restrictShell(); err != nil {
		return err
	}
	if err = gateway.start(3); err != nil {
		return err
	}
	if !exchange(networkMessage{Kind: "ready"}) {
		return fmt.Errorf("parent unavailable")
	}
	// A heartbeat also detects an idle parent's death; flow authorizations alone
	// would leave an idle network namespace alive until its execution deadline.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if !exchange(networkMessage{Kind: "authorize"}) {
				return nil
			}
		}
	}
}

func setupNetworkDevice() error {
	runtime.LockOSThread()
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open tun: %w", err)
	}
	ifr, err := unix.NewIfreq("molr0")
	if err != nil {
		return err
	}
	ifr.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI)
	if err = unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		return fmt.Errorf("create tun: %w", err)
	}
	link, err := netlink.LinkByName("molr0")
	if err != nil {
		return fmt.Errorf("find tun: %w", err)
	}
	if err = netlink.LinkSetMTU(link, 1500); err != nil {
		return err
	}
	for _, cidr := range []string{"10.0.2.100/32", "fd00:6d6f:6c72::100/128"} {
		address, e := netlink.ParseAddr(cidr)
		if e != nil {
			return e
		}
		address.Flags = unix.IFA_F_NODAD
		if e = netlink.AddrAdd(link, address); e != nil {
			return e
		}
	}
	if err = netlink.LinkSetUp(link); err != nil {
		return err
	}
	for _, cidr := range []string{"0.0.0.0/0", "::/0"} {
		_, destination, _ := net.ParseCIDR(cidr)
		if err = netlink.RouteAdd(&netlink.Route{LinkIndex: link.Attrs().Index, Dst: destination, Scope: netlink.SCOPE_LINK}); err != nil {
			return err
		}
	}
	f := os.NewFile(3, "network-setup")
	connection, err := net.FileConn(f)
	f.Close()
	if err != nil {
		return err
	}
	control := connection.(*net.UnixConn)
	if err != nil {
		return err
	}
	if _, _, err = control.WriteMsgUnix([]byte("TUN"), unix.UnixRights(fd), nil); err != nil {
		control.Close()
		return err
	}
	control.Close()
	unix.Close(fd)
	return nil
}

// Control uses SOCK_SEQPACKET: one bounded JSON message per packet. A malformed
// or oversized packet closes the execution rather than growing Runtime memory.
func readNetworkMessage(c net.Conn, message *networkMessage) error {
	var packet [512]byte
	n, err := c.Read(packet[:])
	if err != nil {
		return err
	}
	if n == len(packet) {
		return fmt.Errorf("network control message too large")
	}
	return json.Unmarshal(packet[:n], message)
}
func writeNetworkMessage(c net.Conn, message networkMessage) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if len(data) >= 512 {
		return fmt.Errorf("network control message too large")
	}
	n, err := c.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}
