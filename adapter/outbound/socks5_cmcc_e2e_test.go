package outbound

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/socks5"

	"github.com/stretchr/testify/require"
)

// These tests drive the socks5 outbound through DialContext and
// ListenPacketContext against an in-process CMCC server, so an upstream merge
// that drops or misplaces an XOR wrapper fails CI without live credentials.

// xorReader undoes the client-to-server obfuscation on the server side.
type xorReader struct{ r io.Reader }

func (x xorReader) Read(p []byte) (int, error) {
	n, err := x.r.Read(p)
	for i := range p[:n] {
		p[i] ^= 0xff
	}
	return n, err
}

// fakeCMCCServer expects XOR-obfuscated client bytes, answers in plain text
// and echoes CONNECT streams and UDP datagrams back to the client.
type fakeCMCCServer struct {
	listener      net.Listener
	udp           *net.UDPConn
	method        byte
	username      string
	password      string
	unspecifiedIP bool // answer UDP ASSOCIATE with 0.0.0.0 like some servers do
}

func startFakeCMCCServer(t *testing.T, method byte, username, password string, unspecifiedIP bool) *fakeCMCCServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close(); _ = udp.Close() })

	s := &fakeCMCCServer{
		listener:      listener,
		udp:           udp,
		method:        method,
		username:      username,
		password:      password,
		unspecifiedIP: unspecifiedIP,
	}
	go s.acceptLoop()
	go s.udpLoop()
	return s
}

func (s *fakeCMCCServer) port() int { return s.listener.Addr().(*net.TCPAddr).Port }

func (s *fakeCMCCServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

func (s *fakeCMCCServer) serve(conn net.Conn) {
	defer conn.Close()
	r := xorReader{conn}

	greeting := make([]byte, 3)
	if _, err := io.ReadFull(r, greeting); err != nil || greeting[0] != 5 || greeting[1] != 1 || greeting[2] != s.method {
		return
	}
	challenge := []byte{0x5a}
	if s.method == socks5.CMCCAuthMethod82 {
		challenge = []byte{0xde, 0xad, 0xbe, 0xef}
		_, _ = conn.Write(append([]byte{5, s.method}, challenge...))
	} else {
		_, _ = conn.Write(append([]byte{5}, challenge...))
	}

	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil || header[0] != 1 {
		return
	}
	username := make([]byte, header[1])
	if _, err := io.ReadFull(r, username); err != nil {
		return
	}
	macLen := make([]byte, 1)
	if _, err := io.ReadFull(r, macLen); err != nil {
		return
	}
	mac := make([]byte, macLen[0])
	if _, err := io.ReadFull(r, mac); err != nil {
		return
	}
	key := s.username + s.password
	if s.method == socks5.CMCCAuthMethod82 {
		if _, err := io.ReadFull(r, make([]byte, 21)); err != nil {
			return
		}
		passwordMD5 := md5.Sum([]byte(s.password))
		key = s.username + hex.EncodeToString(passwordMD5[:])
	}
	expected := hmac.New(sha256.New, []byte(key))
	_, _ = expected.Write(challenge)
	if string(username) != s.username || !hmac.Equal(mac, expected.Sum(nil)) {
		_, _ = conn.Write([]byte{1, 1})
		return
	}
	_, _ = conn.Write([]byte{1, 0})

	request := make([]byte, 3)
	if _, err := io.ReadFull(r, request); err != nil || request[0] != 5 {
		return
	}
	if _, err := socks5.ReadAddr0(r); err != nil {
		return
	}
	switch request[1] {
	case socks5.CmdConnect:
		_, _ = conn.Write(append([]byte{5, 0, 0}, socks5.ParseAddrToSocksAddr(conn.LocalAddr())...))
		_, _ = io.Copy(conn, r)
	case socks5.CmdUDPAssociate:
		bind := s.udp.LocalAddr().(*net.UDPAddr)
		if s.unspecifiedIP {
			bind = &net.UDPAddr{IP: net.IPv4zero, Port: bind.Port}
		}
		_, _ = conn.Write(append([]byte{5, 0, 0}, socks5.ParseAddrToSocksAddr(bind)...))
		_, _ = io.Copy(io.Discard, r)
	}
}

func (s *fakeCMCCServer) udpLoop() {
	buffer := make([]byte, 64*1024)
	for {
		n, from, err := s.udp.ReadFrom(buffer)
		if err != nil {
			return
		}
		packet := buffer[:n]
		for i := range packet {
			packet[i] ^= 0xff
		}
		addr, payload, err := socks5.DecodeUDPPacket(packet)
		if err != nil {
			continue // not obfuscated: drop it and let the client time out
		}
		reply, _ := socks5.EncodeUDPPacket(addr, payload)
		_, _ = s.udp.WriteTo(reply, from)
	}
}

func TestSocks5CMCCEndToEnd(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		method        byte
		unspecifiedIP bool
	}{
		{name: "0x80", method: socks5.CMCCAuthMethod80},
		{name: "0x82", method: socks5.CMCCAuthMethod82},
		{name: "unspecified UDP bind address", method: socks5.CMCCAuthMethod80, unspecifiedIP: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := startFakeCMCCServer(t, testCase.method, "1234567899876543210", "pAssWord", testCase.unspecifiedIP)
			proxy, err := NewSocks5(Socks5Option{
				Name:           "cmcc-e2e",
				Server:         "127.0.0.1",
				Port:           server.port(),
				UserName:       server.username,
				Password:       server.password,
				CMCCAuthMethod: fmt.Sprintf("0x%02x", testCase.method),
				UDP:            true,
			})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			payload := []byte{0x00, 0x01, 0x7f, 0x80, 0xff, 'h', 'i'}

			conn, err := proxy.DialContext(ctx, &C.Metadata{NetWork: C.TCP, Host: "example.com", DstPort: 443})
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
			_, err = conn.Write(payload)
			require.NoError(t, err)
			echo := make([]byte, len(payload))
			_, err = io.ReadFull(conn, echo)
			require.NoError(t, err)
			require.Equal(t, payload, echo)

			target := netip.MustParseAddrPort("192.0.2.1:53")
			packetConn, err := proxy.ListenPacketContext(ctx, &C.Metadata{NetWork: C.UDP, DstIP: target.Addr(), DstPort: target.Port()})
			require.NoError(t, err)
			defer packetConn.Close()
			require.NoError(t, packetConn.SetDeadline(time.Now().Add(5*time.Second)))
			_, err = packetConn.WriteTo(payload, net.UDPAddrFromAddrPort(target))
			require.NoError(t, err)
			datagram := make([]byte, 1500)
			n, from, err := packetConn.ReadFrom(datagram)
			require.NoError(t, err)
			require.Equal(t, payload, datagram[:n])
			require.Equal(t, target.String(), from.String())
		})
	}
}

func TestSocks5CMCCEndToEndRejectsWrongPassword(t *testing.T) {
	server := startFakeCMCCServer(t, socks5.CMCCAuthMethod80, "user", "right", false)
	proxy, err := NewSocks5(Socks5Option{
		Name:           "cmcc-e2e",
		Server:         "127.0.0.1",
		Port:           server.port(),
		UserName:       "user",
		Password:       "wrong",
		CMCCAuthMethod: "0x80",
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = proxy.DialContext(ctx, &C.Metadata{NetWork: C.TCP, Host: "example.com", DstPort: 443})
	require.ErrorContains(t, err, "CMCC authentication rejected")
}
