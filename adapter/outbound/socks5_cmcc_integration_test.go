package outbound

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"

	"github.com/stretchr/testify/require"
)

func newLiveCMCCOutbound(t *testing.T) *Socks5 {
	t.Helper()
	serverAddr := os.Getenv("MIHOMO_CMCC_TEST_ADDR")
	username := os.Getenv("MIHOMO_CMCC_TEST_USERNAME")
	password := os.Getenv("MIHOMO_CMCC_TEST_PASSWORD")
	method := os.Getenv("MIHOMO_CMCC_TEST_METHOD")
	if serverAddr == "" || username == "" || password == "" || method == "" {
		t.Skip("set MIHOMO_CMCC_TEST_ADDR, _USERNAME, _PASSWORD, and _METHOD to run the live test")
	}

	server, portValue, err := net.SplitHostPort(serverAddr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portValue)
	require.NoError(t, err)
	proxy, err := NewSocks5(Socks5Option{
		Name:           "cmcc-live-test",
		Server:         server,
		Port:           port,
		UserName:       username,
		Password:       password,
		CMCCAuthMethod: method,
		UDP:            true,
	})
	require.NoError(t, err)
	return proxy
}

func TestSocks5CMCCLiveOutboundTCP(t *testing.T) {
	proxy := newLiveCMCCOutbound(t)
	conn, err := proxy.DialContext(context.Background(), &C.Metadata{
		NetWork: C.TCP,
		Host:    "cp.cloudflare.com",
		DstPort: 80,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.SetDeadline(time.Now().Add(15*time.Second)))

	request := []byte("GET /generate_204 HTTP/1.1\r\nHost: cp.cloudflare.com\r\nConnection: close\r\n\r\n")
	_, err = conn.Write(request)
	require.NoError(t, err)
	statusLine, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(statusLine, "HTTP/1.1 "), statusLine)
}

func TestSocks5CMCCLiveOutboundUDP(t *testing.T) {
	proxy := newLiveCMCCOutbound(t)
	target := netip.MustParseAddr("8.8.8.8")
	conn, err := proxy.ListenPacketContext(context.Background(), &C.Metadata{
		NetWork: C.UDP,
		DstIP:   target,
		DstPort: 53,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.SetDeadline(time.Now().Add(10*time.Second)))

	query := []byte{
		0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'c', 'o', 'm', 0x00,
		0x00, 0x01, 0x00, 0x01,
	}
	_, err = conn.WriteTo(query, net.UDPAddrFromAddrPort(netip.AddrPortFrom(target, 53)))
	require.NoError(t, err)

	response := make([]byte, 64*1024)
	n, _, err := conn.ReadFrom(response)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, 12)
	require.Equal(t, uint16(0x1234), binary.BigEndian.Uint16(response[:2]))
	require.NotZero(t, response[2]&0x80, "DNS response bit was not set")
}
