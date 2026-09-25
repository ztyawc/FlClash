package outbound

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"

	"github.com/metacubex/mihomo/component/resolver"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
	Q "github.com/metacubex/mihomo/transport/qyproxy"
)

type QyProxyOption struct {
	BasicOption
	Name       string `proxy:"name"`
	Server     string `proxy:"server"`
	Port       int    `proxy:"port"`
	Role       string `proxy:"role,omitempty"`
	Username   string `proxy:"username"`
	Password   string `proxy:"password"`
	SN         string `proxy:"sn"`
	GameID     uint32 `proxy:"game-id"`
	ServerID   uint32 `proxy:"server-id"`
	ClientType uint32 `proxy:"client-type,omitempty"`
	GameArea   string `proxy:"game-area,omitempty"`
	Zone       string `proxy:"zone,omitempty"`
	Product    string `proxy:"product,omitempty"`
	Version    string `proxy:"version,omitempty"`
}

type QyProxy struct {
	*Base
	client      *Q.Client
	dialerProxy string
}

func NewQyProxy(o QyProxyOption) (*QyProxy, error) {
	if o.Port <= 0 || o.Port > 65535 {
		return nil, errors.New("qyproxy: invalid control port")
	}
	if o.IPVersion == C.IPv6Only {
		return nil, Q.ErrIPv6
	}
	p := &QyProxy{Base: NewBase(BaseOption{Name: o.Name, Addr: net.JoinHostPort(o.Server, strconv.Itoa(o.Port)), Type: C.QyProxy, ProviderName: o.ProviderName, UDP: true, TFO: o.TFO, MPTCP: o.MPTCP, Interface: o.Interface, RoutingMark: o.RoutingMark, Prefer: C.IPv4Only}), dialerProxy: o.DialerProxy}
	p.dialer = o.NewDialer(p.DialOptions())
	var err error
	p.client, err = Q.New(Q.Options{Server: o.Server, Port: uint16(o.Port), Role: o.Role, Username: o.Username, Password: o.Password, SN: o.SN, GameID: o.GameID, ServerID: o.ServerID, ClientType: o.ClientType, GameArea: o.GameArea, Zone: o.Zone, Product: o.Product, Version: o.Version}, p.dialer,
		func(ctx context.Context, host string) (netip.Addr, error) {
			return resolveIPWithResolver(ctx, host, C.IPv4Only, resolver.ProxyServerHostResolver)
		},
		func(event string) { log.Debugln("[QyProxy] %s: %s", o.Name, event) })
	if err != nil {
		return nil, err
	}
	return p, nil
}
func (p *QyProxy) destination(ctx context.Context, m *C.Metadata) (netip.AddrPort, error) {
	// Pure respects DNS hosts/mapping selections. Fake-IP retains Host, so it
	// is resolved inside the tunnel rather than sent to the game node.
	m = m.Pure()
	ip := m.DstIP.Unmap()
	if m.Host != "" {
		var err error
		ip, err = p.client.Resolve(ctx, m.Host)
		if err != nil {
			return netip.AddrPort{}, err
		}
	}
	if !ip.Is4() {
		return netip.AddrPort{}, Q.ErrIPv6
	}
	if m.DstPort == 0 {
		return netip.AddrPort{}, errors.New("qyproxy: zero destination port")
	}
	return netip.AddrPortFrom(ip, m.DstPort), nil
}
func (p *QyProxy) DialContext(ctx context.Context, m *C.Metadata) (C.Conn, error) {
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
func (p *QyProxy) ResolveUDP(ctx context.Context, m *C.Metadata) error {
	target, err := p.destination(ctx, m)
	if err != nil {
		return err
	}
	m.DstIP = target.Addr()
	return nil
}
func (p *QyProxy) ListenPacketContext(ctx context.Context, m *C.Metadata) (C.PacketConn, error) {
	if err := p.ResolveUDP(ctx, m); err != nil {
		return nil, err
	}
	pc, err := p.client.ListenPacket(ctx)
	if err != nil {
		return nil, err
	}
	return NewPacketConn(pc, p), nil
}
func (p *QyProxy) Close() error { return p.client.Close() }
func (p *QyProxy) ProxyInfo() C.ProxyInfo {
	info := p.Base.ProxyInfo()
	info.DialerProxy = p.dialerProxy
	return info
}

var _ ProxyAdapter = (*QyProxy)(nil)
