package qyproxy

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/sing/common/bufio"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

type testDialer struct{}

func (testDialer) DialContext(ctx context.Context, n, a string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, n, a)
}
func (testDialer) ListenPacket(ctx context.Context, n, a string, _ netip.AddrPort) (net.PacketConn, error) {
	return (&net.ListenConfig{}).ListenPacket(ctx, "udp4", "127.0.0.1:0")
}

// This fixture is a synthetic node: it verifies credentials independently,
// negotiates random ports, changes tokens at each login and runs real sockets.
type testNode struct {
	t             *testing.T
	o             Options
	control, data net.Listener
	udp           *net.UDPConn
	code          byte
	reject        atomic.Bool
	logins, acks  atomic.Int32
	mu            sync.Mutex
	connections   map[net.Conn]bool
	controls      map[net.Conn]bool
	closed        bool
	wg            sync.WaitGroup
}

func newTestNode(t *testing.T, role string, code byte) *testNode {
	a, e := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, e)
	b, e := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, e)
	u, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, e)
	o := testOptions()
	o.Role = role
	o.Port = uint16(a.Addr().(*net.TCPAddr).Port)
	require.NoError(t, o.normalize())
	n := &testNode{t: t, o: o, control: a, data: b, udp: u, code: code, connections: make(map[net.Conn]bool), controls: make(map[net.Conn]bool)}
	n.wg.Add(3)
	go n.accept(a, true)
	go n.accept(b, false)
	go n.serveUDP()
	t.Cleanup(func() {
		n.mu.Lock()
		n.closed = true
		a.Close()
		b.Close()
		u.Close()
		for c := range n.connections {
			c.Close()
		}
		n.mu.Unlock()
		n.wg.Wait()
	})
	return n
}
func (n *testNode) accept(l net.Listener, control bool) {
	defer n.wg.Done()
	for {
		s, e := l.Accept()
		if e != nil {
			return
		}
		n.mu.Lock()
		if n.closed {
			n.mu.Unlock()
			s.Close()
			return
		}
		n.connections[s] = true
		if control {
			n.controls[s] = true
		}
		n.wg.Add(1)
		n.mu.Unlock()
		go func() {
			defer n.wg.Done()
			defer s.Close()
			defer func() { n.mu.Lock(); delete(n.connections, s); delete(n.controls, s); n.mu.Unlock() }()
			s.SetDeadline(time.Now().Add(15 * time.Second))
			if control {
				n.serveControl(s)
			} else {
				n.serveTCP(s)
			}
		}()
	}
}
func (n *testNode) serveControl(s net.Conn) {
	magic := make([]byte, 4)
	if _, e := io.ReadFull(s, magic); e != nil {
		return
	}
	if !bytes.Equal(magic, controlMagic) {
		n.t.Error("wrong control magic")
		return
	}
	m, e := readMessage(s)
	if e != nil {
		return
	}
	require.Equal(n.t, configure(n.o)[4:], frame(m.kind, m.code, m.id, m.body))
	ports := append(be16(uint16(n.data.Addr().(*net.TCPAddr).Port)), be16(uint16(n.udp.LocalAddr().(*net.UDPAddr).Port))...)
	b := option(3, []byte{4, 5}, true)
	b = append(b, option(4, ports, true)...)
	b = append(b, option(6, []byte{0, 5, 0, 1}, true)...)
	b = append(b, option(9, []byte{9, '1', '9', '2', '.', '0', '.', '2', '.', '9'}, true)...)
	if n.o.Role == Primary {
		b = append(b, option(18, be16(5353), true)...)
	}
	if writeFull(s, frame(1, 2, 0, b)) != nil {
		return
	}
	challenge := []byte("synthetic-challenge")
	writeFull(s, frame(4, 1, 17, append([]byte{byte(len(challenge))}, challenge...)))
	m, e = readMessage(s)
	if e != nil {
		return
	}
	digest := md5.Sum(append(append([]byte{17}, n.o.Password...), challenge...))
	if m.kind != 4 || m.code != 2 || m.id != 17 || len(m.body) < 17 || !bytes.Equal(digest[:], m.body[1:17]) || string(m.body[17:]) != n.o.wireUsername() {
		n.t.Error("invalid client CHAP")
		return
	}
	count := n.logins.Add(1)
	if n.reject.Load() {
		writeFull(s, frame(4, 4, 17, nil))
		return
	}
	token := bytes.Repeat([]byte{byte(count)}, 16)
	body := token
	if n.code == 5 {
		body = append([]byte{0, 0, 16}, token...)
	}
	if n.code == 7 {
		body = append([]byte{0, 0, 18, 2, 16}, token...)
	}
	writeFull(s, frame(4, n.code, 17, body))
	writeFull(s, frame(1, 7, 50, nil))
	for {
		m, e = readMessage(s)
		if e != nil {
			return
		}
		if m.kind != 1 {
			n.t.Error("invalid heartbeat")
			return
		}
		if m.code == 7 {
			writeFull(s, frame(1, 8, 99, nil))
			n.acks.Add(1)
		} else if m.code == 8 {
			n.acks.Add(1)
		}
	}
}
func (n *testNode) session(b []byte) ([8]byte, error) {
	var mask [8]byte
	if len(b) < 6 || !bytes.Equal(b[:4], dataMagic) {
		return mask, ErrProtocol
	}
	length := int(binary.BigEndian.Uint16(b[4:]) & 0x7fff)
	if len(b) != length+6 {
		return mask, ErrProtocol
	}
	b = b[6:]
	var token []byte
	var area string
	for len(b) > 0 {
		if len(b) < 2 || len(b) < 2+int(b[1]) {
			return mask, ErrProtocol
		}
		v := b[2 : 2+int(b[1])]
		switch b[0] {
		case 1:
			token = v
		case 10:
			area = string(v)
		case 8, 17:
			if len(v) != 8 {
				return mask, ErrProtocol
			}
			copy(mask[:], v)
		}
		b = b[2+int(b[1]):]
	}
	if !bytes.Equal(token, bytes.Repeat([]byte{byte(n.logins.Load())}, 16)) || area != n.o.dataArea() {
		return mask, ErrAuth
	}
	return mask, nil
}
func (n *testNode) serveTCP(s net.Conn) {
	h := make([]byte, 6)
	if _, e := io.ReadFull(s, h); e != nil {
		return
	}
	b := make([]byte, int(binary.BigEndian.Uint16(h[4:])&0x7fff))
	if len(b) > 2048 {
		return
	}
	if _, e := io.ReadFull(s, b); e != nil {
		return
	}
	mask, e := n.session(append(h, b...))
	if e != nil {
		return
	}
	ack := append([]byte{5, 0}, make([]byte, 12)...)
	banner := []byte("server-first")
	xor(banner, mask, 0)
	writeFull(s, append(ack, banner...))
	var pos uint64
	buf := make([]byte, 123)
	var plain []byte
	for {
		nr, e := s.Read(buf)
		if nr > 0 {
			xor(buf[:nr], mask, pos)
			pos += uint64(nr)
			plain = append(plain, buf[:nr]...)
		}
		if e != nil {
			if e != io.EOF {
				return
			}
			break
		}
	}
	// After client's FIN, the server can still send a long reply.
	xor(plain, mask, uint64(len(banner)))
	writeFull(s, plain)
}
func (n *testNode) serveUDP() {
	defer n.wg.Done()
	sessions := map[netip.AddrPort][8]byte{}
	buf := make([]byte, 65535)
	for {
		nr, from, e := n.udp.ReadFromUDPAddrPort(buf)
		if e != nil {
			return
		}
		b := append([]byte{}, buf[:nr]...)
		if bytes.HasPrefix(b, dataMagic) {
			mask, e := n.session(b)
			if e != nil {
				continue
			}
			sessions[from] = mask
			n.udp.WriteToUDPAddrPort(append([]byte{5, 0}, make([]byte, 12)...), from)
			continue
		}
		mask, ok := sessions[from]
		if !ok {
			continue
		}
		target, p, e := parseUDP(b, mask)
		if e != nil {
			continue
		}
		if target.Port() == 53 || target.Port() == 5353 {
			if n.o.Role == Primary {
				prefix := append([]byte{1, byte(len(n.o.GameArea))}, n.o.GameArea...)
				if !bytes.HasPrefix(p, prefix) {
					n.t.Error("missing primary DNS prefix")
					continue
				}
				p = p[len(prefix):]
			}
			var m dns.Msg
			if m.Unpack(p) != nil {
				continue
			}
			reply := new(dns.Msg)
			reply.SetReply(&m)
			reply.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: m.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 30}, A: net.IPv4(192, 0, 2, 1)}}
			p, _ = reply.Pack()
		}
		n.udp.WriteToUDPAddrPort(udpFrame(target, p, mask), from)
	}
}
func (n *testNode) dropControl() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for s := range n.controls {
		s.Close()
	}
}
func (n *testNode) client(t *testing.T) *Client {
	c, e := New(n.o, testDialer{}, func(context.Context, string) (netip.Addr, error) { return netip.MustParseAddr("127.0.0.1"), nil }, nil)
	require.NoError(t, e)
	c.retryDelays = []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestClientRolesAndData(t *testing.T) {
	for _, test := range []struct {
		role string
		code byte
	}{{Primary, 7}, {CN2, 5}, {CN2Download, 5}, {Primary, 3}} {
		t.Run(fmt.Sprintf("%s_%d", test.role, test.code), func(t *testing.T) {
			n := newTestNode(t, test.role, test.code)
			c := n.client(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ip, e := c.Resolve(ctx, "example.com")
			require.NoError(t, e)
			require.Equal(t, netip.MustParseAddr("192.0.2.1"), ip)
			s, e := c.DialContext(ctx, netip.AddrPortFrom(ip, 80))
			require.NoError(t, e)
			defer s.Close()
			s.SetDeadline(time.Now().Add(5 * time.Second))
			banner := make([]byte, 12)
			_, e = io.ReadFull(s, banner)
			require.NoError(t, e)
			require.Equal(t, "server-first", string(banner))
			original := bytes.Repeat([]byte("data-over-copy-path"), 10000)
			payload := append([]byte{}, original...)
			_, e = bufio.Copy(s, bytes.NewReader(payload))
			require.NoError(t, e)
			require.Equal(t, original, payload, "must not modify caller buffers")
			require.NoError(t, s.(interface{ CloseWrite() error }).CloseWrite())
			reply, e := io.ReadAll(s)
			require.NoError(t, e)
			require.Equal(t, payload, reply)
			pc, e := c.ListenPacket(ctx)
			require.NoError(t, e)
			defer pc.Close()
			pc.SetDeadline(time.Now().Add(5 * time.Second))
			target := net.UDPAddrFromAddrPort(netip.MustParseAddrPort("192.0.2.5:3478"))
			for _, p := range [][]byte{[]byte("ordinary UDP"), []byte("second packet")} {
				_, e = pc.WriteTo(p, target)
				require.NoError(t, e)
				b := make([]byte, 100)
				size, from, e := pc.ReadFrom(b)
				require.NoError(t, e)
				require.Equal(t, target.String(), from.String())
				require.Equal(t, p, b[:size])
			}
			_, e = pc.WriteTo(make([]byte, 1401), target)
			require.ErrorIs(t, e, ErrOversize)
			require.Equal(t, int32(1), n.logins.Load())
		})
	}
}
func TestConcurrentLoginAndRecovery(t *testing.T) {
	n := newTestNode(t, CN2, 5)
	c := n.client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := c.ListenPacket(ctx)
			if e != nil {
				t.Error(e)
				return
			}
			s.Close()
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), n.logins.Load())
	s, e := c.DialContext(ctx, netip.MustParseAddrPort("192.0.2.1:80"))
	require.NoError(t, e)
	defer s.Close()
	banner := make([]byte, 12)
	_, e = io.ReadFull(s, banner)
	require.NoError(t, e)
	n.dropControl()
	require.Eventually(t, func() bool { return n.logins.Load() == 2 }, time.Second, 5*time.Millisecond)
	s.SetReadDeadline(time.Now().Add(time.Second))
	_, e = s.Read(make([]byte, 1))
	require.Error(t, e)
	ip, e := c.Resolve(ctx, "example.com")
	require.NoError(t, e)
	require.True(t, ip.Is4())
	require.NoError(t, c.Close())
	_, e = c.ListenPacket(ctx)
	require.ErrorIs(t, e, ErrClosed)
}
func TestRejectionAndCancellation(t *testing.T) {
	n := newTestNode(t, Primary, 7)
	n.reject.Store(true)
	c := n.client(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, e := c.ListenPacket(ctx)
	require.ErrorIs(t, e, ErrAuth)
	_, e = c.ListenPacket(ctx)
	require.ErrorIs(t, e, ErrAuth)
	require.Equal(t, int32(1), n.logins.Load())
	dead, stop := context.WithCancel(context.Background())
	stop()
	_, e = c.ListenPacket(dead)
	require.ErrorIs(t, e, context.Canceled)
}
func TestDNSRepeatedIDs(t *testing.T) {
	n := newTestNode(t, Primary, 7)
	c := n.client(t)
	pc, e := c.ListenPacket(context.Background())
	require.NoError(t, e)
	defer pc.Close()
	pc.SetDeadline(time.Now().Add(5 * time.Second))
	for i := 0; i < 20; i++ {
		m := new(dns.Msg)
		m.SetQuestion(fmt.Sprintf("n%d.example.", i), dns.TypeA)
		m.Id = 42
		b, e := m.Pack()
		require.NoError(t, e)
		_, e = pc.WriteTo(b, net.UDPAddrFromAddrPort(netip.AddrPortFrom(netip.MustParseAddr("8.8.8.8"), 53)))
		require.NoError(t, e)
	}
	names := map[string]bool{}
	for i := 0; i < 20; i++ {
		b := make([]byte, 1500)
		size, from, e := pc.ReadFrom(b)
		require.NoError(t, e)
		require.Equal(t, "8.8.8.8:53", from.String())
		var m dns.Msg
		require.NoError(t, m.Unpack(b[:size]))
		require.Equal(t, uint16(42), m.Id)
		names[m.Question[0].Name] = true
	}
	require.Len(t, names, 20)
}

func TestHeartbeatTimeoutAndPartialFrame(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := &Client{ctx: ctx, timeout: 50 * time.Millisecond}
			g := &generation{control: left, config: negotiated{interval: 20 * time.Millisecond, retries: 1}}
			done := make(chan error, 1)
			go func() { done <- c.heartbeat(g) }()
			if partial {
				// A truncated header must fail instead of desynchronizing the stream.
				_, e := right.Write([]byte{1, 8})
				require.NoError(t, e)
			} else {
				go io.Copy(io.Discard, right) // consume heartbeat requests; send no ACK
			}
			select {
			case e := <-done:
				require.Error(t, e)
			case <-time.After(time.Second):
				t.Fatal("heartbeat did not expire")
			}
		})
	}
}

type stalledDialer struct {
	testDialer
	peer chan net.Conn
}

func (d stalledDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	a, b := net.Pipe()
	d.peer <- b
	return a, nil
}
func TestCloseCancelsPendingLogin(t *testing.T) {
	d := stalledDialer{peer: make(chan net.Conn, 1)}
	o := testOptions()
	c, e := New(o, d, func(context.Context, string) (netip.Addr, error) { return netip.MustParseAddr("127.0.0.1"), nil }, nil)
	require.NoError(t, e)
	result := make(chan error, 1)
	go func() { _, e := c.ListenPacket(context.Background()); result <- e }()
	peer := <-d.peer
	defer peer.Close()
	done := make(chan struct{})
	go func() { c.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close blocked during handshake")
	}
	require.ErrorIs(t, <-result, ErrClosed)
}
