package socks5

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func cmccLiveTestConfig(t *testing.T) (string, *User, byte) {
	t.Helper()
	server := os.Getenv("MIHOMO_CMCC_TEST_ADDR")
	username := os.Getenv("MIHOMO_CMCC_TEST_USERNAME")
	password := os.Getenv("MIHOMO_CMCC_TEST_PASSWORD")
	methodValue := strings.ToLower(strings.TrimSpace(os.Getenv("MIHOMO_CMCC_TEST_METHOD")))
	if server == "" || username == "" || password == "" || methodValue == "" {
		t.Skip("set MIHOMO_CMCC_TEST_ADDR, _USERNAME, _PASSWORD, and _METHOD to run the live test")
	}

	var method byte
	switch methodValue {
	case "0x80", "80", "128":
		method = CMCCAuthMethod80
	case "0x82", "82", "130":
		method = CMCCAuthMethod82
	default:
		t.Fatalf("unsupported MIHOMO_CMCC_TEST_METHOD %q", methodValue)
	}
	return server, &User{Username: username, Password: password}, method
}

func dialLiveCMCC(t *testing.T) (net.Conn, string, *User, byte) {
	t.Helper()
	server, user, method := cmccLiveTestConfig(t)
	rawConn, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(context.Background(), "tcp", server)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawConn.Close() })
	require.NoError(t, rawConn.SetDeadline(time.Now().Add(15*time.Second)))
	return NewCMCCConn(rawConn), server, user, method
}

func TestCMCCLiveTCP(t *testing.T) {
	conn, _, user, method := dialLiveCMCC(t)
	_, err := ClientHandshakeCMCC(conn, ParseAddr("cp.cloudflare.com:80"), CmdConnect, user, method)
	require.NoError(t, err)

	request := []byte("GET /generate_204 HTTP/1.1\r\nHost: cp.cloudflare.com\r\nConnection: close\r\n\r\n")
	require.NoError(t, writeFull(conn, request))
	statusLine, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(statusLine, "HTTP/1.1 "), statusLine)
}

func TestCMCCLiveUDP(t *testing.T) {
	controlConn, server, user, method := dialLiveCMCC(t)
	associateAddr := ParseAddrToSocksAddr(&net.UDPAddr{IP: net.IPv4zero, Port: 0})
	bindAddr, err := ClientHandshakeCMCC(controlConn, associateAddr, CmdUDPAssociate, user, method)
	require.NoError(t, err)
	relayAddr := bindAddr.UDPAddr()
	require.NotNil(t, relayAddr)
	if relayAddr.IP.IsUnspecified() {
		host, _, splitErr := net.SplitHostPort(server)
		require.NoError(t, splitErr)
		ips, lookupErr := net.LookupIP(host)
		require.NoError(t, lookupErr)
		require.NotEmpty(t, ips)
		relayAddr.IP = ips[0]
	}

	rawUDPConn, err := net.ListenUDP("udp", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawUDPConn.Close() })
	require.NoError(t, rawUDPConn.SetDeadline(time.Now().Add(10*time.Second)))
	udpConn := NewCMCCPacketConn(rawUDPConn)

	query := []byte{
		0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'c', 'o', 'm', 0x00,
		0x00, 0x01, 0x00, 0x01,
	}
	packet, err := EncodeUDPPacket(ParseAddr("8.8.8.8:53"), query)
	require.NoError(t, err)
	_, err = udpConn.WriteTo(packet, relayAddr)
	require.NoError(t, err)

	response := make([]byte, 64*1024)
	n, _, err := udpConn.ReadFrom(response)
	require.NoError(t, err)
	_, payload, err := DecodeUDPPacket(response[:n])
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(payload), 12)
	require.Equal(t, uint16(0x1234), binary.BigEndian.Uint16(payload[:2]))
	require.NotZero(t, payload[2]&0x80, "DNS response bit was not set")
}
