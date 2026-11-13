package vks6

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	N "github.com/metacubex/mihomo/common/net"
)

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type pendingDial struct{ cancel context.CancelFunc }

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
		return nil, errors.New("vks6: dialer is required")
	}
	return &Client{config: config, dialer: dialer,
		pending: make(map[*pendingDial]struct{}), conns: make(map[*Conn]struct{})}, nil
}

func (c *Client) DialContext(parent context.Context, target netip.AddrPort) (result net.Conn, err error) {
	if err = parent.Err(); err != nil {
		return nil, err
	}
	header, err := Header(c.config, target)
	if err != nil {
		return nil, err
	}
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

	port := c.config.Port + rand.Intn(c.config.PortEnd-c.config.Port+1)
	raw, err := c.dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.config.Server, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	stream := &Conn{Conn: raw, owner: c}
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
			if ctxErr := ctx.Err(); ctxErr != nil {
				err = ctxErr
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				err = context.DeadlineExceeded
			}
		}
		if err != nil {
			_ = stream.Close()
			result = nil
		}
	}()
	done := N.SetupContextForConn(ctx, raw)
	defer done(&err)
	deadline, _ := ctx.Deadline()
	if err = raw.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if _, err = writeFull(raw, header); err != nil {
		return nil, err
	}
	var reply [2]byte
	if _, err = io.ReadFull(raw, reply[:]); err != nil {
		return nil, err
	}
	if reply != [2]byte{0x06, 0x00} {
		return nil, fmt.Errorf("vks6: node rejected TCP connect: %02x %02x", reply[0], reply[1])
	}
	if err = raw.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
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
