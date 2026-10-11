package platform

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type NetworkBinding struct {
	Origin string
}
type NetworkOptions struct {
	AllowedPrivateAddresses []netip.Addr
	OwnerBinding            func(context.Context, core.Principal, core.ID) (NetworkBinding, error)
	Binding                 func(context.Context, core.Execution, core.ID) (NetworkBinding, error)
}
type Guard struct{ options NetworkOptions }

func NewNetworkGuard(options NetworkOptions) *Guard {
	options.AllowedPrivateAddresses = append([]netip.Addr(nil), options.AllowedPrivateAddresses...)
	return &Guard{options: options}
}
func (g *Guard) Check(ctx context.Context, execution core.Execution, destination Destination) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	if err := execution.Validate(); err != nil {
		return err
	}
	if g.options.Binding == nil {
		return core.NewError(core.Forbidden, "network access disabled")
	}
	binding, err := g.options.Binding(ctx, execution, destination.ResourceID)
	if err != nil {
		return err
	}
	return g.checkBinding(binding, destination)
}

func (g *Guard) CheckOwner(ctx context.Context, p core.Principal, destination Destination) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if g.options.OwnerBinding == nil {
		return core.NewError(core.Forbidden, "owner network access disabled")
	}
	binding, err := g.options.OwnerBinding(ctx, p, destination.ResourceID)
	if err != nil {
		return err
	}
	return g.checkBinding(binding, destination)
}

func (g *Guard) checkBinding(binding NetworkBinding, destination Destination) error {
	target, err := url.Parse(destination.URL)
	if err != nil || target.User != nil || target.Host == "" || target.Fragment != "" || (target.Scheme != "http" && target.Scheme != "https") {
		return core.NewError(core.Forbidden, "invalid network target")
	}
	configured, err := url.Parse(binding.Origin)
	if err != nil || configured.Host == "" || !strings.EqualFold(target.Scheme, configured.Scheme) || !strings.EqualFold(target.Host, configured.Host) {
		return core.NewError(core.Forbidden, "target differs from approved origin")
	}
	if len(destination.ResolvedAddresses) == 0 {
		return core.NewError(core.Forbidden, "resolved address required")
	}
	for _, original := range destination.ResolvedAddresses {
		address := original.Unmap()
		if address == netip.MustParseAddr("100.100.100.200") || address == netip.MustParseAddr("168.63.129.16") || address == netip.MustParseAddr("fd00:ec2::254") {
			return core.WithReason(core.NewError(core.Forbidden, "metadata destination prohibited"), core.NetworkDestinationProhibited)
		}
		if !address.IsValid() || address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
			return core.WithReason(core.NewError(core.Forbidden, "prohibited network destination"), core.NetworkDestinationProhibited)
		}
		explicit := false
		for _, allowed := range g.options.AllowedPrivateAddresses {
			if allowed.Unmap() == address {
				explicit = true
				break
			}
		}
		private := address.IsPrivate() || address.IsLoopback() || netip.MustParsePrefix("100.64.0.0/10").Contains(address) || !address.IsGlobalUnicast()
		if private && !explicit {
			return core.WithReason(core.NewError(core.Forbidden, "private destination not permitted by server configuration"), core.NetworkPrivateAddressDenied)
		}
		if target.Scheme == "http" && (!private || !explicit) {
			return core.WithReason(core.NewError(core.Forbidden, "public providers require HTTPS"), core.NetworkHTTPNotAllowed)
		}
	}
	return nil
}

// HTTPClient validates resolved addresses at dial time and dials those exact
// addresses, preventing a second DNS lookup from bypassing the decision. Redirects
// are disabled so authorization headers cannot migrate to another destination.
func (g *Guard) HTTPClient(execution core.Execution, resourceID core.ID, origin string) *http.Client {
	return boundHTTPClient(resourceID, origin, func(ctx context.Context, d Destination) error { return g.Check(ctx, execution, d) })
}

func (g *Guard) OwnerHTTPClient(p core.Principal, resourceID core.ID, origin string) *http.Client {
	return boundHTTPClient(resourceID, origin, func(ctx context.Context, d Destination) error { return g.CheckOwner(ctx, p, d) })
}

// PublicHTTPClient is for fixed server integrations, not user-selected tools.
// It pins the origin and allows only public HTTPS destinations at the actual dial.
func PublicHTTPClient(origin string) *http.Client {
	return ConfiguredHTTPClient(origin, nil)
}

// ConfiguredHTTPClient permits only deployment-approved private IPs and pins the origin.
func ConfiguredHTTPClient(origin string, allowed []netip.Addr) *http.Client {
	g := NewNetworkGuard(NetworkOptions{AllowedPrivateAddresses: allowed})
	return boundHTTPClient("", origin, func(ctx context.Context, d Destination) error {
		if err := persistence.CheckContext(ctx); err != nil {
			return err
		}
		return g.checkBinding(NetworkBinding{Origin: origin}, d)
	})
}

func boundHTTPClient(resourceID core.ID, origin string, check func(context.Context, Destination) error) *http.Client {
	base, _ := url.Parse(origin)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if base == nil {
			return nil, core.NewError(core.Forbidden, "invalid origin")
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		expectedPort := base.Port()
		if expectedPort == "" {
			if base.Scheme == "https" {
				expectedPort = "443"
			} else {
				expectedPort = "80"
			}
		}
		if !strings.EqualFold(host, base.Hostname()) || port != expectedPort {
			return nil, core.NewError(core.Forbidden, "unapproved connection address")
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if err = check(ctx, Destination{ResourceID: resourceID, URL: origin, ResolvedAddresses: ips}); err != nil {
			return nil, err
		}
		var last error
		dialer := net.Dialer{Timeout: 15 * time.Second}
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if last == nil {
			last = core.NewError(core.Unavailable, "no resolved addresses")
		}
		return nil, last
	}
	return &http.Client{Transport: transport, Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error {
		return core.NewError(core.Forbidden, "redirects require a new authorized request")
	}}
}
