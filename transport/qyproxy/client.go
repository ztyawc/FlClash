package qyproxy

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"
)

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
	ListenPacket(context.Context, string, string, netip.AddrPort) (net.PacketConn, error)
}

// Client owns one control session shared by its data connections. Authentication
// is lazy and independent of any single waiting request's cancellation.
type Client struct {
	opts            Options
	dialer          Dialer
	resolveServer   func(context.Context, string) (netip.Addr, error)
	log             func(string)
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.Mutex
	changed         chan struct{}
	started, closed bool
	terminal        error
	current         *generation
	wg              sync.WaitGroup
	timeout         time.Duration
	retryDelays     []time.Duration
}

type generation struct {
	control          net.Conn
	ip               netip.Addr
	config           negotiated
	token            [16]byte
	mu               sync.Mutex
	closed           bool
	sockets          map[io.Closer]struct{}
	tcpNext, udpNext int
	cache            map[string]dnsCache
}

func New(o Options, d Dialer, resolve func(context.Context, string) (netip.Addr, error), log func(string)) (*Client, error) {
	if err := o.normalize(); err != nil {
		return nil, err
	}
	if d == nil || resolve == nil {
		return nil, errors.New("qyproxy: missing dialer or server resolver")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if log == nil {
		log = func(string) {}
	}
	return &Client{opts: o, dialer: d, resolveServer: resolve, log: log, ctx: ctx, cancel: cancel, changed: make(chan struct{}), timeout: 12 * time.Second, retryDelays: []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}}, nil
}
func (c *Client) notifyLocked() { close(c.changed); c.changed = make(chan struct{}) }
func (c *Client) get(ctx context.Context) (*generation, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, ErrClosed
		}
		if c.terminal != nil {
			err := c.terminal
			c.mu.Unlock()
			return nil, err
		}
		if c.current != nil {
			g := c.current
			c.mu.Unlock()
			return g, nil
		}
		if !c.started {
			c.started = true
			c.wg.Add(1)
			go c.run()
		}
		ch := c.changed
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ch:
		}
	}
}
func (c *Client) run() {
	defer c.wg.Done()
	attempt := 0
	for {
		g, err := c.login()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			if errors.Is(err, ErrAuth) || attempt >= len(c.retryDelays) {
				c.mu.Lock()
				c.terminal = err
				c.notifyLocked()
				c.mu.Unlock()
				c.log("authentication stopped; reload required")
				return
			}
			c.log(fmt.Sprintf("control connection failed; retrying: %v", err))
			if !c.wait(c.retryDelays[attempt]) {
				return
			}
			attempt++
			continue
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			g.close()
			return
		}
		c.current = g
		c.notifyLocked()
		c.mu.Unlock()
		c.log("authenticated")
		_ = c.heartbeat(g)
		c.mu.Lock()
		c.current = nil
		c.notifyLocked()
		c.mu.Unlock()
		g.close()
		if c.ctx.Err() != nil {
			return
		}
		c.log("control connection lost; closing old data sessions")
		if !c.wait(c.retryDelays[0]) {
			return
		}
		attempt = 1
	}
}
func (c *Client) wait(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-c.ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
func (c *Client) Close() error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		c.cancel()
		c.notifyLocked()
	}
	g := c.current
	c.mu.Unlock()
	if g != nil {
		g.close()
	}
	c.wg.Wait()
	return nil
}

// Cancels a handshake by closing its socket; stop waits out the watcher so it
// cannot close an established connection after the handshake has returned.
func watchContext(ctx context.Context, s io.Closer) func() {
	done, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case <-ctx.Done():
			s.Close()
		case <-done:
		}
	}()
	return func() { close(done); <-finished }
}
func (c *Client) login() (_ *generation, err error) {
	phase := "control dial"
	defer func() {
		if err != nil {
			err = fmt.Errorf("qyproxy %s: %w", phase, err)
		}
	}()
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()
	ip, err := c.resolveServer(ctx, c.opts.Server)
	if err != nil {
		return nil, err
	}
	ip = ip.Unmap()
	if !ip.Is4() {
		return nil, ErrIPv6
	}
	s, err := c.dialer.DialContext(ctx, "tcp", netip.AddrPortFrom(ip, c.opts.Port).String())
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	stop := watchContext(ctx, s)
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = s.SetDeadline(deadline); err != nil {
		return nil, err
	}
	phase = "configure write"
	if err = writeFull(s, configure(c.opts)); err != nil {
		return nil, err
	}
	g := &generation{control: s, ip: ip, sockets: make(map[io.Closer]struct{}), cache: make(map[string]dnsCache)}
	configured, challenged := false, false
	var challengeID byte
	for count := 0; count < 12; count++ {
		phase = "control response"
		m, e := readMessage(s)
		if e != nil {
			return nil, e
		}
		switch {
		case m.kind == 1 && m.code == 2:
			phase = "configure ACK"
			if configured || m.id != 0 {
				return nil, ErrProtocol
			}
			g.config, err = parseConfigure(m.body)
			if err != nil {
				return nil, err
			}
			configured = true
		case m.kind == 4 && m.code == 1:
			phase = "CHAP challenge"
			if !configured {
				return nil, ErrProtocol
			}
			b, e := challengeResponse(c.opts, m)
			if e != nil {
				return nil, e
			}
			if e = writeFull(s, b); e != nil {
				return nil, e
			}
			challengeID = m.id
			challenged = true
		case m.kind == 4 && (m.code == 3 || m.code == 5 || m.code == 7):
			phase = "CHAP success"
			if !configured || !challenged || m.id != challengeID {
				return nil, ErrProtocol
			}
			g.token, err = parseSuccess(m)
			if err != nil {
				return nil, err
			}
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			if err = s.SetDeadline(time.Time{}); err != nil {
				return nil, err
			}
			return g, nil
		case m.kind == 4 && (m.code == 4 || m.code == 6):
			return nil, ErrAuth
		default:
			return nil, ErrProtocol
		}
	}
	return nil, ErrProtocol
}
func (c *Client) heartbeat(g *generation) error {
	interval := g.config.interval
	next := time.Now().Add(interval)
	last := time.Now()
	grace := interval * time.Duration(uint64(g.config.retries)+1)
	for {
		if c.ctx.Err() != nil {
			return c.ctx.Err()
		}
		now := time.Now()
		if now.Sub(last) >= grace {
			return errors.New("qyproxy: heartbeat timeout")
		}
		if !now.Before(next) {
			g.control.SetWriteDeadline(now.Add(c.timeout))
			if err := writeFull(g.control, frame(1, 7, randomByte(), nil)); err != nil {
				return err
			}
			next = now.Add(interval)
		}
		deadline := next
		if last.Add(grace).Before(deadline) {
			deadline = last.Add(grace)
		}
		g.control.SetReadDeadline(deadline)
		// ReadFull may consume part of a frame before a timeout. A partial frame
		// must terminate this generation instead of being parsed as a new header.
		var first [1]byte
		_, err := io.ReadFull(g.control, first[:])
		if err != nil {
			if e, ok := err.(net.Error); ok && e.Timeout() {
				continue
			}
			return err
		}
		g.control.SetReadDeadline(time.Now().Add(c.timeout))
		m, err := readMessage(io.MultiReader(&byteReader{b: first[:]}, g.control))
		if err != nil {
			return err
		}
		if m.kind != 1 || (m.code != 7 && m.code != 8) || len(m.body) != 0 {
			return ErrProtocol
		}
		if m.code == 8 {
			last = time.Now()
		} else {
			g.control.SetWriteDeadline(time.Now().Add(c.timeout))
			if err = writeFull(g.control, frame(1, 8, randomByte(), nil)); err != nil {
				return err
			}
		}
	}
}

type byteReader struct{ b []byte }

func (r *byteReader) Read(b []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(b, r.b)
	r.b = r.b[n:]
	return n, nil
}
func randomByte() byte { var b [1]byte; _, _ = rand.Read(b[:]); return b[0] }
func (g *generation) track(s io.Closer) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		s.Close()
		return ErrClosed
	}
	g.sockets[s] = struct{}{}
	return nil
}
func (g *generation) untrack(s io.Closer) { g.mu.Lock(); delete(g.sockets, s); g.mu.Unlock() }
func (g *generation) close() {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	sockets := make([]io.Closer, 0, len(g.sockets))
	for s := range g.sockets {
		sockets = append(sockets, s)
	}
	g.sockets = make(map[io.Closer]struct{})
	g.cache = make(map[string]dnsCache)
	g.mu.Unlock()
	g.control.Close()
	for _, s := range sockets {
		s.Close()
	}
}
func (g *generation) ports(udp bool) ([]uint16, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, ErrClosed
	}
	p, n := g.config.tcp, &g.tcpNext
	if udp {
		p, n = g.config.udp, &g.udpNext
	}
	start := *n % len(p)
	*n = (*n + 1) % len(p)
	out := append([]uint16{}, p[start:]...)
	return append(out, p[:start]...), nil
}
func localSource(a net.Addr, g *generation) (netip.AddrPort, error) {
	ap, err := netip.ParseAddrPort(a.String())
	if err != nil {
		return ap, errors.New("qyproxy: unsupported local socket address")
	}
	ip := ap.Addr().Unmap()
	if ip.IsUnspecified() {
		p, e := netip.ParseAddrPort(g.control.LocalAddr().String())
		if e == nil {
			ip = p.Addr().Unmap()
		}
		if !ip.Is4() {
			ip = netip.IPv4Unspecified()
		}
	}
	if !ip.Is4() {
		return ap, ErrIPv6
	}
	return netip.AddrPortFrom(ip, ap.Port()), nil
}

func (c *Client) DialContext(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
	if !target.IsValid() || !target.Addr().Is4() || target.Port() == 0 {
		return nil, ErrIPv6
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	g, err := c.get(ctx)
	if err != nil {
		return nil, err
	}
	ports, err := g.ports(false)
	if err != nil {
		return nil, err
	}
	for _, p := range ports {
		attempt, stop := context.WithTimeout(ctx, 4*time.Second)
		s, e := c.openTCP(attempt, g, p, target)
		stop()
		if e == nil {
			return s, nil
		}
		err = e
		if errors.Is(e, ErrProtocol) || errors.Is(e, ErrSession) || errors.Is(e, ErrClosed) || ctx.Err() != nil {
			break
		}
	}
	return nil, err
}
func (c *Client) openTCP(ctx context.Context, g *generation, port uint16, target netip.AddrPort) (_ net.Conn, err error) {
	s, err := c.dialer.DialContext(ctx, "tcp", netip.AddrPortFrom(g.ip, port).String())
	if err != nil {
		return nil, err
	}
	if err = g.track(s); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			g.untrack(s)
			s.Close()
		}
	}()
	stop := watchContext(ctx, s)
	defer stop()
	deadline, _ := ctx.Deadline()
	s.SetDeadline(deadline)
	src, err := localSource(s.LocalAddr(), g)
	if err != nil {
		return nil, err
	}
	var mask [8]byte
	if _, err = rand.Read(mask[:]); err != nil {
		return nil, err
	}
	if err = writeFull(s, sessionInit(c.opts, g.token, mask, src, target)); err != nil {
		return nil, err
	}
	var ack [14]byte
	if _, err = io.ReadFull(s, ack[:]); err != nil {
		return nil, err
	}
	if err = checkACK(ack[:]); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	s.SetDeadline(time.Time{})
	return &streamConn{Conn: s, mask: mask, g: g}, nil
}
func (c *Client) ListenPacket(ctx context.Context) (net.PacketConn, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	g, err := c.get(ctx)
	if err != nil {
		return nil, err
	}
	return c.openPacket(ctx, g)
}
func (c *Client) openPacket(ctx context.Context, g *generation) (net.PacketConn, error) {
	ports, err := g.ports(true)
	if err != nil {
		return nil, err
	}
	for _, p := range ports {
		attempt, stop := context.WithTimeout(ctx, 4*time.Second)
		s, e := c.openUDP(attempt, g, p)
		stop()
		if e == nil {
			return s, nil
		}
		err = e
		if errors.Is(e, ErrProtocol) || errors.Is(e, ErrSession) || errors.Is(e, ErrClosed) || ctx.Err() != nil {
			break
		}
	}
	return nil, err
}
func (c *Client) openUDP(ctx context.Context, g *generation, port uint16) (_ net.PacketConn, err error) {
	remote := netip.AddrPortFrom(g.ip, port)
	s, err := c.dialer.ListenPacket(ctx, "udp", "", remote)
	if err != nil {
		return nil, err
	}
	if err = g.track(s); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			g.untrack(s)
			s.Close()
		}
	}()
	stop := watchContext(ctx, s)
	defer stop()
	deadline, _ := ctx.Deadline()
	s.SetDeadline(deadline)
	src, err := localSource(s.LocalAddr(), g)
	if err != nil {
		return nil, err
	}
	var mask [8]byte
	if _, err = rand.Read(mask[:]); err != nil {
		return nil, err
	}
	b := sessionInit(c.opts, g.token, mask, src, netip.AddrPort{})
	raddr := net.UDPAddrFromAddrPort(remote)
	if _, err = s.WriteTo(b, raddr); err != nil {
		return nil, err
	}
	buf := make([]byte, 2048)
	for {
		n, from, e := s.ReadFrom(buf)
		if e != nil {
			return nil, e
		}
		if !sameAddr(from, remote) {
			continue
		}
		if err = checkACK(buf[:n]); err != nil {
			return nil, err
		}
		break
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	s.SetDeadline(time.Time{})
	return &packetConn{PacketConn: s, g: g, opts: c.opts, remote: remote, mask: mask, pending: make(map[uint16]dnsPending)}, nil
}
func sameAddr(a net.Addr, b netip.AddrPort) bool {
	p, e := netip.ParseAddrPort(a.String())
	return e == nil && netip.AddrPortFrom(p.Addr().Unmap(), p.Port()) == b
}
