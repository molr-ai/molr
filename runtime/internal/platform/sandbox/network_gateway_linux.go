//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/fdbased"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
	"molr.ai/runtime/internal/platform"
)

type networkGateway struct {
	exchange    func(networkMessage) networkMessage
	targets     []platform.NetworkTarget
	dnsSlots    chan struct{}
	lookup      func(context.Context, string) ([]netip.Addr, error)
	mu          sync.Mutex
	revoked     bool
	connections map[net.Conn]bool
	slots       chan struct{}
	budget      atomic.Int64
	denied      atomic.Int64
	accepted    atomic.Int64
	rules       map[string][]platform.NetworkRule
	names       map[string]string
	addresses   map[netip.Addr]string
	nextAddress uint32
	authorize   func() bool
	notice      func(string)
	closeOnce   sync.Once
	stack       *stack.Stack
	fd          int
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
}

func newNetworkGateway(ctx context.Context, cfg networkConfig, authorize func() bool, notice func(string)) (*networkGateway, error) {
	ctx, cancel := context.WithCancel(ctx)
	g := &networkGateway{targets: cfg.Targets, connections: map[net.Conn]bool{}, slots: make(chan struct{}, cfg.Connections), dnsSlots: make(chan struct{}, 8), rules: map[string][]platform.NetworkRule{}, names: map[string]string{}, addresses: map[netip.Addr]string{}, fd: -1, ctx: ctx, cancel: cancel, authorize: authorize, notice: notice}
	g.lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}
	g.budget.Store(cfg.Bytes)
	for _, target := range cfg.Targets {
		r, e := platform.CompileNetworkTarget(target)
		if e != nil {
			cancel()
			return nil, e
		}
		for _, protocol := range target.Protocols {
			g.rules[protocol] = append(g.rules[protocol], r)
		}
	}
	return g, nil
}

func (g *networkGateway) allowed(protocol string, ip netip.Addr, port uint16) bool {
	for _, r := range g.rules[protocol] {
		if r.AllowsTransport(protocol, "", strconv.Itoa(int(port)), ip) {
			return true
		}
	}
	return false
}
func (g *networkGateway) claim() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revoked || (g.exchange == nil && g.budget.Load() <= 0) {
		return false
	}
	select {
	case g.slots <- struct{}{}:
		g.wg.Add(1)
		return true
	default:
		return false
	}
}
func (g *networkGateway) claimDNS() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revoked || (g.exchange == nil && g.budget.Load() <= 0) {
		return false
	}
	select {
	case g.dnsSlots <- struct{}{}:
		g.wg.Add(1)
		return true
	default:
		return false
	}
}
func (g *networkGateway) releaseDNS() { <-g.dnsSlots; g.wg.Done() }
func (g *networkGateway) release()    { <-g.slots; g.wg.Done() }
func (g *networkGateway) track(c net.Conn) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revoked {
		c.Close()
		return false
	}
	g.connections[c] = true
	return true
}
func (g *networkGateway) forget(c net.Conn) {
	c.Close()
	g.mu.Lock()
	delete(g.connections, c)
	g.mu.Unlock()
}
func (g *networkGateway) revoke() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.revoked = true
	g.cancel()
	for c := range g.connections {
		c.Close()
	}
}
func (g *networkGateway) close() {
	g.closeOnce.Do(func() {
		g.revoke()
		if g.stack != nil {
			g.stack.Close()
		}
		if g.fd >= 0 {
			unix.Close(g.fd)
			g.fd = -1
		}
		g.wg.Wait()
		if g.stack != nil {
			g.stack.Wait()
		}
	})
}
func (g *networkGateway) start(fd int) error {
	g.fd = fd
	unix.SetNonblock(fd, true)
	s := stack.New(stack.Options{NetworkProtocols: []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol}, TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol}})
	g.stack = s
	link, err := fdbased.New(&fdbased.Options{FDs: []int{fd}, MTU: 1500})
	if err != nil {
		return err
	}
	if e := s.CreateNIC(1, link); e != nil {
		return fmt.Errorf("NIC: %s", e)
	}
	s.SetPromiscuousMode(1, true)
	s.SetSpoofing(1, true)
	receive := tcpip.TCPReceiveBufferSizeRangeOption{Min: 4096, Default: 65536, Max: 262144}
	send := tcpip.TCPSendBufferSizeRangeOption{Min: 4096, Default: 65536, Max: 262144}
	s.SetTransportProtocolOption(tcp.ProtocolNumber, &receive)
	s.SetTransportProtocolOption(tcp.ProtocolNumber, &send)
	s.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: 1}, {Destination: header.IPv6EmptySubnet, NIC: 1}})
	tf := tcp.NewForwarder(s, 0, cap(g.slots)+cap(g.dnsSlots), func(r *tcp.ForwarderRequest) {
		id := r.ID()
		ip, _ := netip.AddrFromSlice(id.LocalAddress.AsSlice())
		isDNS := ip == dnsAddress && id.LocalPort == 53
		claim, release := g.claim, g.release
		if isDNS {
			claim, release = g.claimDNS, g.releaseDNS
		}
		if !claim() {
			g.denied.Add(1)
			g.notice("network_connection_limit")
			r.Complete(true)
			return
		}
		go func() {
			defer release()
			if isDNS {
				var q waiter.Queue
				ep, e := r.CreateEndpoint(&q)
				if e != nil {
					r.Complete(true)
					return
				}
				r.Complete(false)
				local := gonet.NewTCPConn(&q, ep)
				if !g.track(local) {
					return
				}
				defer g.forget(local)
				g.serveDNS(local, true)
				return
			}
			remote, err := g.dial("tcp", ip, id.LocalPort)
			if err != nil {
				r.Complete(true)
				return
			}
			defer g.forget(remote)
			var q waiter.Queue
			ep, e := r.CreateEndpoint(&q)
			if e != nil {
				r.Complete(true)
				return
			}
			r.Complete(false)
			local := gonet.NewTCPConn(&q, ep)
			if !g.track(local) {
				return
			}
			defer g.forget(local)
			g.accepted.Add(1)
			bounded := &networkQuotaConn{Conn: remote, budget: &g.budget, exhausted: func() { g.notice("network_bytes_exhausted") }}
			if g.exchange != nil {
				pipe(local, remote)
			} else {
				pipe(local, bounded)
			}
		}()
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tf.HandlePacket)
	uf := udp.NewForwarder(s, func(r *udp.ForwarderRequest) bool {
		id := r.ID()
		ip, _ := netip.AddrFromSlice(id.LocalAddress.AsSlice())
		dns := ip == dnsAddress && id.LocalPort == 53
		claim, release := g.claim, g.release
		if dns {
			claim, release = g.claimDNS, g.releaseDNS
		}
		if !claim() {
			g.denied.Add(1)
			g.notice("network_connection_limit")
			return false
		}
		var q waiter.Queue
		ep, e := r.CreateEndpoint(&q)
		if e != nil {
			release()
			return true
		}
		local := gonet.NewUDPConn(&q, ep)
		if !g.track(local) {
			release()
			return true
		}
		go func() {
			defer release()
			defer g.forget(local)
			if dns {
				g.serveDNS(local, false)
				return
			}
			remote, err := g.dial("udp", ip, id.LocalPort)
			if err != nil {
				return
			}
			defer g.forget(remote)
			g.accepted.Add(1)
			done := make(chan struct{})
			go func() { g.datagrams(local, remote); local.Close(); remote.Close(); close(done) }()
			g.datagrams(remote, local)
			local.Close()
			remote.Close()
			<-done
		}()
		return true
	})
	s.SetTransportProtocolHandler(udp.ProtocolNumber, uf.HandlePacket)
	return nil
}

// Charge bytes actually forwarded, not a whole read buffer before a blocking
// read. Reserving an idle read can otherwise starve the opposite direction
// during a TLS handshake when the remaining quota is smaller than the buffer.
type networkQuotaConn struct {
	net.Conn
	budget    *atomic.Int64
	exhausted func()
}

func (c *networkQuotaConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return c.Conn.Close()
}

func networkTake(budget *atomic.Int64, wanted int) int {
	for {
		left := budget.Load()
		if left <= 0 {
			return 0
		}
		n := min(int64(wanted), left)
		if budget.CompareAndSwap(left, left-n) {
			return int(n)
		}
	}
}

func (c *networkQuotaConn) Read(b []byte) (int, error) {
	left := c.budget.Load()
	if left <= 0 {
		c.exhausted()
		return 0, io.ErrUnexpectedEOF
	}
	n, err := c.Conn.Read(b[:min(int64(len(b)), left)])
	allowed := networkTake(c.budget, n)
	if allowed != n {
		c.exhausted()
		return allowed, io.ErrUnexpectedEOF
	}
	return n, err
}

func (c *networkQuotaConn) Write(b []byte) (int, error) {
	allowed := networkTake(c.budget, len(b))
	if allowed == 0 {
		c.exhausted()
		return 0, io.ErrShortWrite
	}
	n, err := c.Conn.Write(b[:allowed])
	if err == nil && allowed != len(b) {
		err = io.ErrShortWrite
	}
	return n, err
}
func (g *networkGateway) dial(protocol string, ip netip.Addr, port uint16) (net.Conn, error) {
	if !g.authorize() {
		return nil, fmt.Errorf("authorization changed")
	}
	host := ip.String()
	ips := []netip.Addr{ip}
	if virtualPrefix.Contains(ip) || virtualPrefix6.Contains(ip) {
		g.mu.Lock()
		host = g.addresses[ip]
		g.mu.Unlock()
		if host == "" {
			g.notice("network_destination_denied")
			return nil, fmt.Errorf("unknown virtual address")
		}
		var err error
		ips, err = g.resolve(host)
		if err != nil {
			g.notice("network_dns_failed")
			return nil, err
		}
	}
	for _, address := range ips {
		allowed := false
		for _, rule := range g.rules[protocol] {
			allowed = allowed || rule.AllowsTransport(protocol, host, strconv.Itoa(int(port)), address)
		}
		if !allowed {
			g.notice("network_destination_denied")
			return nil, fmt.Errorf("network destination denied")
		}
	}
	for _, address := range ips {
		if virtualPrefix.Contains(ip) && !address.Unmap().Is4() {
			continue
		}
		if virtualPrefix6.Contains(ip) && (!address.Is6() || address.Is4In6()) {
			continue
		}
		target := -1
		if g.exchange != nil {
			for i, t := range g.targets {
				rule, _ := platform.CompileNetworkTarget(t)
				if rule.AllowsTransport(protocol, host, strconv.Itoa(int(port)), address) {
					target = i
					break
				}
			}
			if target < 0 {
				return nil, fmt.Errorf("network destination denied")
			}
		}
		var claim networkMessage
		if target >= 0 {
			claim = g.exchange(networkMessage{Kind: "claim", Target: target})
			if !claim.Allowed {
				g.notice("network_connection_limit")
				return nil, fmt.Errorf("network connection quota exhausted")
			}
		}
		c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(g.ctx, protocol, net.JoinHostPort(address.String(), strconv.Itoa(int(port))))
		if err != nil && target >= 0 {
			g.exchange(networkMessage{Kind: "release", Target: target, Token: claim.Token})
		}
		if err == nil {
			if target >= 0 {
				c = &accountedConn{Conn: c, g: g, target: target, slot: claim.Token, udp: protocol == "udp"}
			}
			if !g.track(c) {
				return nil, fmt.Errorf("revoked")
			}
			return c, nil
		}
	}
	g.notice("network_connection_failed")
	return nil, fmt.Errorf("destination unavailable")
}
func (g *networkGateway) datagrams(dst, src net.Conn) {
	buffer := make([]byte, 65535)
	for {
		src.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, e := src.Read(buffer)
		if e != nil {
			return
		}
		if g.exchange == nil && networkTake(&g.budget, n) != n {
			g.notice("network_bytes_exhausted")
			return
		}
		dst.SetReadDeadline(time.Now().Add(60 * time.Second))
		dst.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, e = dst.Write(buffer[:n]); e != nil {
			return
		}
	}
}
