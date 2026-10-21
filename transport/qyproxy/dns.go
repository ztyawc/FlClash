package qyproxy

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/miekg/dns"
)

type dnsCache struct {
	ip      netip.Addr
	expires time.Time
}

func sameQuestions(a, b []dns.Question) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i].Name, b[i].Name) || a[i].Qtype != b[i].Qtype || a[i].Qclass != b[i].Qclass {
			return false
		}
	}
	return true
}

// Resolve queries DNS through this authenticated generation, never the local
// resolver. Explicit addresses pass through without generating a DNS query.
func (c *Client) Resolve(ctx context.Context, host string) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if !ip.Is4() {
			return netip.Addr{}, ErrIPv6
		}
		return ip, nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	g, err := c.get(ctx)
	if err != nil {
		return netip.Addr{}, err
	}
	key := strings.ToLower(dns.Fqdn(host))
	g.mu.Lock()
	cached, found := g.cache[key]
	g.mu.Unlock()
	if found && time.Now().Before(cached.expires) {
		return cached.ip, nil
	}
	m := new(dns.Msg)
	m.SetQuestion(key, dns.TypeA)
	m.Id = uint16(randomByte())<<8 | uint16(randomByte())
	b, err := m.Pack()
	if err != nil {
		return netip.Addr{}, errors.New("qyproxy: invalid DNS name")
	}
	s, err := c.openPacket(ctx, g)
	if err != nil {
		return netip.Addr{}, err
	}
	defer s.Close()
	stop := watchContext(ctx, s)
	defer stop()
	deadline, _ := ctx.Deadline()
	s.SetDeadline(deadline)
	target := netip.MustParseAddrPort("1.1.1.1:53")
	if _, err = s.WriteTo(b, net.UDPAddrFromAddrPort(target)); err != nil {
		return netip.Addr{}, err
	}
	buf := make([]byte, 4096)
	n, from, err := s.ReadFrom(buf)
	if err != nil {
		return netip.Addr{}, err
	}
	if !sameAddr(from, target) {
		return netip.Addr{}, ErrProtocol
	}
	var reply dns.Msg
	if err = reply.Unpack(buf[:n]); err != nil || reply.Id != m.Id || !reply.Response || reply.Truncated || !sameQuestions(reply.Question, m.Question) {
		return netip.Addr{}, ErrProtocol
	}
	if reply.Rcode != dns.RcodeSuccess {
		return netip.Addr{}, errors.New("qyproxy: remote DNS query failed")
	}
	// Only accept A records belonging to the queried name or its CNAME chain.
	names := map[string]bool{key: true}
	ttl := uint32(30)
	for i := 0; i < len(reply.Answer); i++ {
		for _, rr := range reply.Answer {
			if v, ok := rr.(*dns.CNAME); ok && names[strings.ToLower(v.Hdr.Name)] {
				names[strings.ToLower(v.Target)] = true
				if v.Hdr.Ttl < ttl {
					ttl = v.Hdr.Ttl
				}
			}
		}
	}
	for _, rr := range reply.Answer {
		if a, ok := rr.(*dns.A); ok && names[strings.ToLower(a.Hdr.Name)] {
			ip, ok := netip.AddrFromSlice(a.A)
			if !ok || !ip.Unmap().Is4() {
				continue
			}
			ip = ip.Unmap()
			if a.Hdr.Ttl < ttl {
				ttl = a.Hdr.Ttl
			}
			g.mu.Lock()
			if g.closed {
				g.mu.Unlock()
				return netip.Addr{}, ErrClosed
			}
			if len(g.cache) >= 256 {
				g.cache = make(map[string]dnsCache)
			}
			if ttl > 0 {
				g.cache[key] = dnsCache{ip, time.Now().Add(time.Duration(ttl) * time.Second)}
			}
			g.mu.Unlock()
			return ip, nil
		}
	}
	return netip.Addr{}, errors.New("qyproxy: remote DNS returned no IPv4 address")
}
