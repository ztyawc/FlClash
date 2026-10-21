package outbound

import (
	"context"
	"net/netip"
	"testing"

	C "github.com/metacubex/mihomo/constant"
	"github.com/stretchr/testify/require"
)

func TestQyProxyExplicitHosts(t *testing.T) {
	p, e := NewQyProxy(QyProxyOption{Name: "test", Server: "192.0.2.1", Port: 7025, Username: "test", Password: "test", SN: "test", GameID: 1, ServerID: 2})
	require.NoError(t, e)
	defer p.Close()
	m := &C.Metadata{Host: "must-not-resolve.invalid", DstIP: netip.MustParseAddr("192.0.2.2"), DstPort: 443, DNSMode: C.DNSHosts}
	for i := 0; i < 2; i++ {
		ap, e := p.destination(context.Background(), m)
		require.NoError(t, e)
		require.Equal(t, "192.0.2.2:443", ap.String())
		require.NotEmpty(t, m.Host, "don't mutate caller metadata")
		m.DNSMode = C.DNSMapping
	}
	_, e = p.destination(context.Background(), &C.Metadata{DstIP: netip.MustParseAddr("2001:db8::1"), DstPort: 443})
	require.Error(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = p.destination(ctx, &C.Metadata{Host: "example.com", DstIP: netip.MustParseAddr("198.18.0.1"), DstPort: 443})
	require.ErrorIs(t, e, context.Canceled)
}
