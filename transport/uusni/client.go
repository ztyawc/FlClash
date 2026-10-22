package uusni

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	N "github.com/metacubex/mihomo/common/net"
)

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type pendingDial struct{ cancel context.CancelFunc }

// Client has no control session, heartbeat or fallback. Each flow owns one
// connection obtained from mihomo's dialer; Close also interrupts handshakes.
type Client struct {
	config  Config
	dialer  Dialer
	mu      sync.Mutex
	closed  bool
	pending map[*pendingDial]struct{}
	conns   map[*Conn]struct{}
	workers sync.WaitGroup
}

func NewClient(config Config, dialer Dialer) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if dialer == nil {
		return nil, errors.New("uusni: dialer is required")
	}
	return &Client{config: config, dialer: dialer, pending: make(map[*pendingDial]struct{}), conns: make(map[*Conn]struct{})}, nil
}

func (c *Client) DialContext(parent context.Context, host string, port uint16) (result net.Conn, err error) {
	if err = parent.Err(); err != nil {
		return nil, err
	}
	header, err := Header(c.config, host, port)
	if err != nil {
		return nil, err
	}
	// Bound both dial and header writes, including callers without a deadline.
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	pending := &pendingDial{cancel: cancel}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		cancel()
		return nil, net.ErrClosed
	}
	c.pending[pending] = struct{}{}
	c.workers.Add(1)
	c.mu.Unlock()
	defer func() {
		cancel()
		c.mu.Lock()
		delete(c.pending, pending)
		c.mu.Unlock()
		c.workers.Done()
	}()

	raw, err := c.dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.config.Server, strconv.Itoa(c.config.Port)))
	if err != nil {
		return nil, err
	}
	stream := &Conn{Conn: raw, key: byte(c.config.ObfsKey), owner: c}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = raw.Close()
		return nil, net.ErrClosed
	}
	c.conns[stream] = struct{}{}
	c.mu.Unlock()
	defer func() {
		if err != nil {
			_ = stream.Close()
			result = nil
		}
	}()
	done := N.SetupContextForConn(ctx, stream)
	defer done(&err)
	deadline, _ := ctx.Deadline()
	if err = raw.SetWriteDeadline(deadline); err != nil {
		return nil, err
	}
	if _, err = writeFull(raw, header); err != nil {
		return nil, err
	}
	if err = raw.SetWriteDeadline(time.Time{}); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	// There is no success ACK. The next received byte belongs to the target.
	return stream, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	for pending := range c.pending {
		pending.cancel()
	}
	conns := make([]*Conn, 0, len(c.conns))
	for conn := range c.conns {
		conns = append(conns, conn)
	}
	c.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
	c.workers.Wait()
	return nil
}
