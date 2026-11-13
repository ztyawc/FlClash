package outbound

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"

	"github.com/metacubex/mihomo/component/resolver"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/vks6"
)

type VKS6Option struct {
	BasicOption
	Name     string `proxy:"name"`
	Server   string `proxy:"server"`
	Port     any    `proxy:"port"`
	PortEnd  any    `proxy:"port-end,omitempty"`
	Username string `proxy:"username"`
	Password string `proxy:"password"`
	UDP      bool   `proxy:"udp,omitempty"`
	TLS      bool   `proxy:"tls,omitempty"`
	Network  string `proxy:"network,omitempty"`
}

type VKS6 struct {
	*Base
	client      *vks6.Client
	dialerProxy string
}

func NewVKS6(o VKS6Option) (*VKS6, error) {
	port, err := strictUnsignedInteger(o.Port, 65535)
	if err != nil || port == 0 {
		return nil, errors.New("vks6: port must be an integer between 1 and 65535")
	}
	portEnd := port
	if o.PortEnd != nil {
		portEnd, err = strictUnsignedInteger(o.PortEnd, 65535)
		if err != nil {
			return nil, errors.New("vks6: port-end must be an integer between port and 65535")
		}
	}
	if portEnd < port {
		return nil, errors.New("vks6: port-end is below port")
	}
	if o.UDP || o.TLS || o.Network != "" && o.Network != "tcp" {
		return nil, errors.New("vks6: only plain TCP is supported")
	}
	if o.IPVersion == C.IPv6Only {
		return nil, errors.New("vks6: IPv6 is not supported")
	}
	p := &VKS6{Base: NewBase(BaseOption{
		Name: o.Name, Addr: net.JoinHostPort(o.Server, strconv.Itoa(port)),
		Type: C.VKS6, ProviderName: o.ProviderName, TFO: o.TFO,
		MPTCP: o.MPTCP, Interface: o.Interface, RoutingMark: o.RoutingMark,
		Prefer: C.IPv4Only,
	}), dialerProxy: o.DialerProxy}
	p.dialer = o.NewDialer(p.DialOptions())
	p.client, err = vks6.NewClient(vks6.Config{
		Server: o.Server, Port: port, PortEnd: portEnd,
		Username: o.Username, Password: o.Password,
	}, p.dialer)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *VKS6) destination(ctx context.Context, m *C.Metadata) (netip.AddrPort, error) {
	if m == nil || m.NetWork != C.TCP || m.DstPort == 0 {
		return netip.AddrPort{}, errors.New("vks6: a TCP target with a nonzero port is required")
	}
	m = m.Pure()
	var ip netip.Addr
	if m.Host != "" {
		var err error
		ip, err = resolver.ResolveIPv4WithResolver(ctx, m.Host, resolver.DefaultResolver)
		if err != nil {
			return netip.AddrPort{}, err
		}
	} else {
		if m.DNSMode == C.DNSFakeIP {
			return netip.AddrPort{}, errors.New("vks6: fake-IP target has no original hostname")
		}
		ip = m.DstIP.Unmap()
	}
	if !ip.Is4() || resolver.IsFakeIP(ip) {
		return netip.AddrPort{}, errors.New("vks6: target has no real IPv4 address")
	}
	return netip.AddrPortFrom(ip, m.DstPort), nil
}

func (p *VKS6) DialContext(ctx context.Context, m *C.Metadata) (C.Conn, error) {
	target, err := p.destination(ctx, m)
	if err != nil {
		return nil, err
	}
	c, err := p.client.DialContext(ctx, target)
	if err != nil {
		return nil, err
	}
	return NewConn(c, p), nil
}

func (p *VKS6) ProxyInfo() C.ProxyInfo {
	info := p.Base.ProxyInfo()
	info.DialerProxy = p.dialerProxy
	return info
}

func (p *VKS6) Close() error { return p.client.Close() }

var _ ProxyAdapter = (*VKS6)(nil)
