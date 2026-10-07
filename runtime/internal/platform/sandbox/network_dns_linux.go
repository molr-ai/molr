//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"molr.ai/runtime/internal/platform"
)

var dnsAddress = netip.MustParseAddr("198.18.0.1")
var virtualPrefix = netip.MustParsePrefix("198.18.0.0/15")
var virtualPrefix6 = netip.MustParsePrefix("fd00:6d6f:6c72:ffff::/64")

const resolverFile = "nameserver 198.18.0.1\noptions timeout:2 attempts:2\n"

// Real addresses are never returned to the guest for domain grants. Each
// execution owns a bounded synthetic mapping; every connection re-resolves the
// name and checks ALL answers, then dials the checked IP without a second lookup.
func (g *networkGateway) resolve(host string) ([]netip.Addr, error) {
	if host == "host6.molr.internal" {
		return []netip.Addr{netip.MustParseAddr("::1")}, nil
	}
	if host == "host.molr.internal" {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	ctx, cancel := context.WithTimeout(g.ctx, 5*time.Second)
	defer cancel()
	ips, err := g.lookup(ctx, host)
	if err != nil || len(ips) == 0 || len(ips) > 64 {
		return nil, fmt.Errorf("DNS resolution unavailable")
	}
	return ips, nil
}

func (g *networkGateway) dnsReply(packet []byte) []byte {
	var query dnsmessage.Message
	if query.Unpack(packet) != nil || query.Response || query.OpCode != 0 || len(query.Questions) != 1 {
		return nil
	}
	q := query.Questions[0]
	response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, RecursionDesired: query.RecursionDesired, RecursionAvailable: true}, Questions: query.Questions}
	pack := func() []byte { b, _ := response.Pack(); return b }
	host := strings.TrimSuffix(strings.ToLower(q.Name.String()), ".")
	if q.Class != dnsmessage.ClassINET || (q.Type != dnsmessage.TypeA && q.Type != dnsmessage.TypeAAAA) || len(host) == 0 || len(host) > 253 {
		response.RCode = dnsmessage.RCodeRefused
		return pack()
	}
	if !g.authorize() {
		response.RCode = dnsmessage.RCodeRefused
		return pack()
	}
	mayResolve := false
	for _, rules := range g.rules {
		for _, rule := range rules {
			mayResolve = mayResolve || rule.MayResolve(host)
		}
	}
	if !mayResolve {
		g.notice("network_destination_denied")
		response.RCode = dnsmessage.RCodeRefused
		return pack()
	}
	ips, err := g.resolve(host)
	if err != nil {
		g.notice("network_dns_failed")
		response.RCode = dnsmessage.RCodeServerFailure
		return pack()
	}
	hasFamily := false
	for _, ip := range ips {
		allowed := false
		for _, rules := range g.rules {
			for _, rule := range rules {
				allowed = allowed || rule.AllowsAddress(host, ip)
			}
		}
		if !allowed {
			g.notice("network_destination_denied")
			response.RCode = dnsmessage.RCodeRefused
			return pack()
		}
		hasFamily = hasFamily || (q.Type == dnsmessage.TypeA && ip.Unmap().Is4()) || (q.Type == dnsmessage.TypeAAAA && ip.Is6() && !ip.Is4In6())
	}
	if !hasFamily {
		return pack()
	}
	key := fmt.Sprintf("%s/%d", host, q.Type)
	g.mu.Lock()
	address, exists := g.names[key]
	if !exists {
		if len(g.names) >= 1024 {
			g.mu.Unlock()
			g.notice("network_dns_limit")
			response.RCode = dnsmessage.RCodeServerFailure
			return pack()
		}
		g.nextAddress++
		ip := netip.AddrFrom4([4]byte{198, 19, byte(g.nextAddress >> 8), byte(g.nextAddress)})
		if q.Type == dnsmessage.TypeAAAA {
			b := virtualPrefix6.Addr().As16()
			binary.BigEndian.PutUint32(b[12:], g.nextAddress)
			ip = netip.AddrFrom16(b)
		}
		address = ip.String()
		g.names[key] = address
		g.addresses[ip] = host
	}
	g.mu.Unlock()
	ip := netip.MustParseAddr(address)
	h := dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 30}
	var body dnsmessage.ResourceBody
	if ip.Is4() {
		body = &dnsmessage.AResource{A: ip.As4()}
	} else {
		body = &dnsmessage.AAAAResource{AAAA: ip.As16()}
	}
	response.Answers = []dnsmessage.Resource{{Header: h, Body: body}}
	return pack()
}

func (g *networkGateway) serveDNS(c net.Conn, tcp bool) {
	buffer := make([]byte, 4096)
	for requests := 0; requests < 256; requests++ {
		billingTarget := -1
		c.SetDeadline(time.Now().Add(10 * time.Second))
		var n int
		var err error
		if tcp {
			var length [2]byte
			if _, err = io.ReadFull(c, length[:]); err != nil {
				return
			}
			n = int(binary.BigEndian.Uint16(length[:]))
			if n > len(buffer) || n < 12 {
				return
			}
			_, err = io.ReadFull(c, buffer[:n])
		} else {
			n, err = c.Read(buffer)
		}
		if err != nil {
			return
		}
		if g.exchange == nil && networkTake(&g.budget, n) != n {
			g.notice("network_bytes_exhausted")
			return
		}
		if g.exchange != nil {
			var query dnsmessage.Message
			if query.Unpack(buffer[:n]) != nil || len(query.Questions) != 1 {
				return
			}
			host := strings.TrimSuffix(strings.ToLower(query.Questions[0].Name.String()), ".")
			target := -1
			for i, t := range g.targets {
				rule, _ := platform.CompileNetworkTarget(t)
				if rule.MayResolve(host) && g.exchange(networkMessage{Kind: "check", Target: i}).Allowed {
					target = i
					break
				}
			}
			if target < 0 {
				g.notice("network_bytes_exhausted")
				return
			}
			billingTarget = target
			amount := n
			if tcp {
				amount += 2
			}
			credit := g.exchange(networkMessage{Kind: "reserve", Target: target, Bytes: int64(amount)})
			if !credit.Allowed {
				return
			}
			if credit.Bytes != int64(amount) {
				g.exchange(networkMessage{Kind: "settle", Target: target, Token: credit.Token})
				return
			}
			if credit.End != 0 && time.Now().UnixNano() >= credit.End {
				g.exchange(networkMessage{Kind: "settle", Target: target, Token: credit.Token})
				return
			}
			if !g.exchange(networkMessage{Kind: "settle", Target: target, Token: credit.Token, Bytes: credit.Bytes}).Allowed {
				return
			}
		}
		response := g.dnsReply(buffer[:n])
		if len(response) == 0 {
			return
		}
		if tcp {
			response = append(binary.BigEndian.AppendUint16(nil, uint16(len(response))), response...)
		}
		if g.exchange != nil {
			target := billingTarget
			if target < 0 {
				return
			}
			amount := len(response)
			credit := g.exchange(networkMessage{Kind: "reserve", Target: target, Bytes: int64(amount)})
			if !credit.Allowed || credit.Bytes != int64(amount) {
				if credit.Allowed {
					g.exchange(networkMessage{Kind: "settle", Target: target, Token: credit.Token})
				}
				g.notice("network_bytes_exhausted")
				return
			}
			if !g.exchange(networkMessage{Kind: "settle", Target: target, Token: credit.Token, Bytes: int64(amount)}).Allowed {
				return
			}
		}
		if g.exchange == nil && networkTake(&g.budget, len(response)) != len(response) {
			g.notice("network_bytes_exhausted")
			return
		}
		if _, err = c.Write(response); err != nil {
			return
		}
		if !tcp {
			return
		}
	}
}
