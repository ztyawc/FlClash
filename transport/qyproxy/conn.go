package qyproxy

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type streamConn struct {
	net.Conn
	g        *generation
	mask     [8]byte
	rmu, wmu sync.Mutex
	rx, tx   uint64
	once     sync.Once
}

// Expose half-close traversal, but never permit copy/splice to bypass the codec.
func (c *streamConn) Upstream() any           { return c.Conn }
func (c *streamConn) ReaderReplaceable() bool { return false }
func (c *streamConn) WriterReplaceable() bool { return false }
func (c *streamConn) Read(b []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	n, e := c.Conn.Read(b)
	xor(b[:n], c.mask, c.rx)
	c.rx += uint64(n)
	return n, e
}
func (c *streamConn) Write(b []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	total := 0
	size := len(b)
	if size > 32768 {
		size = 32768
	}
	scratch := make([]byte, size)
	for len(b) > 0 {
		next := len(b)
		if next > len(scratch) {
			next = len(scratch)
		}
		copy(scratch, b[:next])
		xor(scratch[:next], c.mask, c.tx)
		written := 0
		for written < next {
			n, err := c.Conn.Write(scratch[written:next])
			if n < 0 || n > next-written {
				return total, io.ErrShortWrite
			}
			written += n
			total += n
			c.tx += uint64(n)
			if err != nil {
				return total, err
			}
			if n == 0 {
				return total, io.ErrShortWrite
			}
		}
		b = b[next:]
	}
	return total, nil
}
func (c *streamConn) CloseWrite() error {
	if s, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return s.CloseWrite()
	}
	return c.Close()
}
func (c *streamConn) CloseRead() error {
	if s, ok := c.Conn.(interface{ CloseRead() error }); ok {
		return s.CloseRead()
	}
	return c.Close()
}
func (c *streamConn) Close() error {
	var err error
	c.once.Do(func() { c.g.untrack(c.Conn); err = c.Conn.Close() })
	return err
}

type dnsPending struct {
	target   netip.AddrPort
	id       uint16
	expires  time.Time
	question []dns.Question
}
type packetConn struct {
	net.PacketConn
	g            *generation
	opts         Options
	remote       netip.AddrPort
	mask         [8]byte
	rmu, wmu, mu sync.Mutex
	pending      map[uint16]dnsPending
	nextID       uint16
	once         sync.Once
}

func (c *packetConn) Upstream() any           { return c.PacketConn }
func (c *packetConn) ReaderReplaceable() bool { return false }
func (c *packetConn) WriterReplaceable() bool { return false }
func (c *packetConn) dnsTarget() (netip.AddrPort, error) {
	if c.opts.Role != Primary {
		return netip.MustParseAddrPort("1.1.1.1:53"), nil
	}
	if !c.g.config.dns.IsValid() {
		return netip.AddrPort{}, errors.New("qyproxy: node did not supply DNS")
	}
	return c.g.config.dns, nil
}
func (c *packetConn) WriteTo(b []byte, a net.Addr) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	target, err := netip.ParseAddrPort(a.String())
	if err != nil || !target.Addr().Unmap().Is4() || target.Port() == 0 {
		return 0, ErrIPv6
	}
	target = netip.AddrPortFrom(target.Addr().Unmap(), target.Port())
	payload := b
	var id uint16
	isDNS := target.Port() == 53
	if isDNS {
		var m dns.Msg
		if e := m.Unpack(b); e != nil || m.Response || len(m.Question) == 0 {
			return 0, ErrProtocol
		}
		dest, e := c.dnsTarget()
		if e != nil {
			return 0, e
		}
		c.mu.Lock()
		now := time.Now()
		for key, v := range c.pending {
			if now.After(v.expires) {
				delete(c.pending, key)
			}
		}
		if len(c.pending) >= 1024 {
			c.mu.Unlock()
			return 0, errors.New("qyproxy: too many outstanding DNS requests")
		}
		for {
			c.nextID++
			if _, used := c.pending[c.nextID]; !used {
				break
			}
		}
		id = c.nextID
		c.pending[id] = dnsPending{target, m.Id, now.Add(15 * time.Second), m.Question}
		c.mu.Unlock()
		defer func() {
			if err != nil {
				c.mu.Lock()
				delete(c.pending, id)
				c.mu.Unlock()
			}
		}()
		prefix := []byte{}
		if c.opts.Role == Primary {
			prefix = append([]byte{1, byte(len(c.opts.GameArea))}, c.opts.GameArea...)
		}
		payload = make([]byte, len(prefix)+len(b))
		copy(payload, prefix)
		copy(payload[len(prefix):], b)
		binary.BigEndian.PutUint16(payload[len(prefix):], id)
		target = dest
	}
	if len(payload)+10 > c.opts.mtu() {
		err = ErrOversize
		return 0, err
	}
	wire := udpFrame(target, payload, c.mask)
	n, e := c.PacketConn.WriteTo(wire, net.UDPAddrFromAddrPort(c.remote))
	err = e
	if err == nil && n != len(wire) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return 0, err
	}
	return len(b), nil
}
func (c *packetConn) ReadFrom(b []byte) (int, net.Addr, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	wire := make([]byte, 65535)
	for {
		n, from, err := c.PacketConn.ReadFrom(wire)
		if err != nil {
			return 0, nil, err
		}
		if !sameAddr(from, c.remote) {
			continue
		}
		if n == 14 && wire[0] == 5 && wire[1] == 3 {
			c.Close()
			return 0, nil, errors.New("qyproxy: UDP session released")
		}
		source, payload, e := parseUDP(wire[:n], c.mask)
		if e != nil {
			continue
		}
		dest, e := c.dnsTarget()
		if e == nil && source == dest {
			var m dns.Msg
			if e = m.Unpack(payload); e != nil || !m.Response {
				continue
			}
			c.mu.Lock()
			p, ok := c.pending[m.Id]
			valid := ok && time.Now().Before(p.expires) && sameQuestions(m.Question, p.question)
			if valid {
				delete(c.pending, m.Id)
			}
			c.mu.Unlock()
			if !valid {
				continue
			}
			binary.BigEndian.PutUint16(payload, p.id)
			source = p.target
		}
		copied := copy(b, payload)
		if copied < len(payload) {
			return copied, net.UDPAddrFromAddrPort(source), io.ErrShortBuffer
		}
		return copied, net.UDPAddrFromAddrPort(source), nil
	}
}
func (c *packetConn) Close() error {
	var err error
	c.once.Do(func() {
		c.g.untrack(c.PacketConn)
		err = c.PacketConn.Close()
		c.mu.Lock()
		clearPending := make(map[uint16]dnsPending)
		c.pending = clearPending
		c.mu.Unlock()
	})
	return err
}
