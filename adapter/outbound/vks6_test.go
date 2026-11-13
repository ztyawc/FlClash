package outbound

import (
	"context"
	"net/netip"
	"testing"

	C "github.com/metacubex/mihomo/constant"
)

func TestVKS6DestinationMetadata(t *testing.T) {
	p, err := NewVKS6(VKS6Option{Server: "203.0.113.10", Port: 8050,
		Username: "vk-test", Password: "synthetic-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	chosen := netip.MustParseAddr("203.0.113.33")
	m := &C.Metadata{NetWork: C.TCP, DNSMode: C.DNSMapping, Host: "mapped.example",
		DstIP: chosen, DstPort: 443}
	got, err := p.destination(context.Background(), m)
	if err != nil || got != netip.AddrPortFrom(chosen, 443) || m.Host != "mapped.example" {
		t.Fatalf("mapped target = %s, metadata host = %q, err = %v", got, m.Host, err)
	}
	for _, invalid := range []*C.Metadata{
		{NetWork: C.TCP, DNSMode: C.DNSFakeIP, DstIP: netip.MustParseAddr("198.18.0.2"), DstPort: 443},
		{NetWork: C.TCP, DstIP: netip.MustParseAddr("2001:db8::1"), DstPort: 443},
		{NetWork: C.UDP, DstIP: chosen, DstPort: 443},
		{NetWork: C.TCP, DstIP: chosen, DstPort: 0},
	} {
		if _, err := p.destination(context.Background(), invalid); err == nil {
			t.Fatalf("unsupported target accepted: %+v", invalid)
		}
	}
}
