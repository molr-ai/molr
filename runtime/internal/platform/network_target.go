package platform

import (
	"molr.ai/runtime/internal/core"
	"net/netip"
	"strconv"
	"strings"
)

// NetworkRule is an immutable allow-set minus exclude-set policy. Exclusions
// apply only to this resource; independently granted resources form a union.
// DNS is resolved by the caller; the same checked IP must be used for dialing.
type NetworkRule struct {
	hasPorts         bool
	protocols        map[string]bool
	addresses        addressSet
	excludeAddresses addressSet
	ports            portSet
	excludePorts     portSet
}
type addressSet struct {
	hosts    []string
	prefixes []netip.Prefix
}
type portSet [][2]uint16

func CompileNetworkTarget(target NetworkTarget) (NetworkRule, error) {
	rule := NetworkRule{protocols: map[string]bool{}}
	if len(target.Protocols) == 0 || len(target.Protocols) > 2 {
		return rule, core.Invalid("protocols", "explicit tcp and/or udp required; replace legacy network configuration")
	}
	for _, protocol := range target.Protocols {
		if (protocol != "tcp" && protocol != "udp") || rule.protocols[protocol] {
			return rule, core.Invalid("protocols", "unique tcp and/or udp required")
		}
		rule.protocols[protocol] = true
	}
	if len(target.Addresses) == 0 || len(target.Ports) == 0 {
		return rule, core.Invalid("network", "addresses and ports must not be empty")
	}
	var err error
	if rule.addresses, err = compileAddresses(target.Addresses, "addresses"); err != nil {
		return NetworkRule{}, err
	}
	if rule.excludeAddresses, err = compileAddresses(target.ExcludeAddresses, "exclude_addresses"); err != nil {
		return NetworkRule{}, err
	}
	if rule.ports, err = compilePorts(target.Ports, "ports"); err != nil {
		return NetworkRule{}, err
	}
	if rule.excludePorts, err = compilePorts(target.ExcludePorts, "exclude_ports"); err != nil {
		return NetworkRule{}, err
	}
	for _, allowed := range rule.ports {
		for port := int(allowed[0]); port <= int(allowed[1]); {
			next := port
			for _, excluded := range rule.excludePorts {
				if port >= int(excluded[0]) && port <= int(excluded[1]) {
					next = max(next, int(excluded[1])+1)
				}
			}
			if next == port {
				rule.hasPorts = true
				break
			}
			port = next
		}
		if rule.hasPorts {
			break
		}
	}
	return rule, nil
}

func compileAddresses(values []string, field string) (addressSet, error) {
	var set addressSet
	invalid := func(message string) (addressSet, error) { return addressSet{}, core.Invalid(field, message) }
	if len(values) > 32 {
		return invalid("at most 32 addresses allowed")
	}
	for _, address := range values {
		if strings.Contains(address, "/") {
			prefix, err := netip.ParsePrefix(address)
			if err != nil || prefix.Addr().Is4In6() || prefix != prefix.Masked() {
				return invalid("canonical IPv4 or IPv6 CIDR required")
			}
			set.prefixes = append(set.prefixes, prefix)
			continue
		}
		if ip, err := netip.ParseAddr(address); err == nil {
			if ip.Zone() != "" {
				return invalid("scoped IP addresses are not supported")
			}
			ip = ip.Unmap()
			set.prefixes = append(set.prefixes, netip.PrefixFrom(ip, ip.BitLen()))
			continue
		}
		address = strings.ToLower(address)
		if len(address) == 0 || len(address) > 253 {
			return invalid("invalid hostname")
		}
		for _, label := range strings.Split(address, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return invalid("invalid hostname")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return invalid("exact hostname, IP or CIDR required")
				}
			}
		}
		set.hosts = append(set.hosts, address)
	}
	return set, nil
}

func compilePorts(values []string, field string) (portSet, error) {
	var set portSet
	if len(values) > 32 {
		return nil, core.Invalid(field, "at most 32 port ranges allowed")
	}
	for _, value := range values {
		first, last, ranged := strings.Cut(value, "-")
		if !ranged {
			last = first
		}
		start, err1 := networkPort(first)
		end, err2 := networkPort(last)
		if err1 != nil || err2 != nil || start > end {
			return nil, core.Invalid(field, "ports must be 1-65535, individually or as ascending inclusive ranges")
		}
		set = append(set, [2]uint16{start, end})
	}
	return set, nil
}

func networkPort(value string) (uint16, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != value {
		return 0, core.Invalid("network", "canonical numeric port required")
	}
	return uint16(n), nil
}
func (s portSet) contains(n uint16) bool {
	for _, interval := range s {
		if n >= interval[0] && n <= interval[1] {
			return true
		}
	}
	return false
}
func (s addressSet) contains(host string, ip netip.Addr) bool {
	for _, prefix := range s.prefixes {
		if prefix.Contains(ip.Unmap()) {
			return true
		}
	}
	for _, name := range s.hosts {
		if strings.EqualFold(host, name) {
			return true
		}
	}
	return false
}
func (r NetworkRule) AllowsPort(port string) bool {
	n, err := networkPort(port)
	return err == nil && r.ports.contains(n) && !r.excludePorts.contains(n)
}
func (r NetworkRule) Allows(host, port string, ip netip.Addr) bool {
	return networkAddressAllowed(ip) && r.AllowsPort(port) && r.addresses.contains(host, ip) && !r.excludeAddresses.contains(host, ip)
}

// These protocol and metadata guards are not user network classification.
// Private and loopback access is determined entirely by explicit rule sets.
func networkAddressAllowed(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.Zone() != "" || a.IsUnspecified() || a.IsMulticast() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() {
		return false
	}
	for _, s := range []string{"100.100.100.200", "168.63.129.16", "fd00:ec2::254"} {
		if a == netip.MustParseAddr(s) {
			return false
		}
	}
	return a.IsGlobalUnicast() || a.IsLoopback()
}

func (r NetworkRule) AllowsTransport(protocol, host, port string, ip netip.Addr) bool {
	return r.protocols[protocol] && r.Allows(host, port, ip)
}
func (r NetworkRule) AllowsAddress(host string, ip netip.Addr) bool {
	return r.hasPorts && networkAddressAllowed(ip) && r.addresses.contains(host, ip) && !r.excludeAddresses.contains(host, ip)
}
func (r NetworkRule) MayResolve(host string) bool {
	if !r.hasPorts {
		return false
	}
	for _, name := range r.excludeAddresses.hosts {
		if strings.EqualFold(name, host) {
			return false
		}
	}
	if len(r.addresses.prefixes) > 0 {
		return true
	}
	for _, name := range r.addresses.hosts {
		if strings.EqualFold(name, host) {
			return true
		}
	}
	return false
}
