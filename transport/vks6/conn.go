package vks6

import (
	"errors"
	"io"
	"net"
	"sync"

	N "github.com/metacubex/mihomo/common/net"
)

const dataKey = byte(0x5a)

type Conn struct {
	net.Conn
	owner    *Client
	rmu, wmu sync.Mutex
	once     sync.Once
	closeErr error
}

// Keep mihomo's fast-copy paths from bypassing the XOR layer.
func (c *Conn) Upstream() any           { return c.Conn }
func (c *Conn) ReaderReplaceable() bool { return false }
func (c *Conn) WriterReplaceable() bool { return false }

func xor(data []byte) {
	for i := range data {
		data[i] ^= dataKey
	}
}

func (c *Conn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	n, err := c.Conn.Read(p)
	xor(p[:n])
	return n, err
}

func (c *Conn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	const chunkSize = 32 * 1024
	buf := make([]byte, chunkSize)
	total := 0
	for len(p) > 0 {
		n := copy(buf, p)
		xor(buf[:n])
		written, err := writeFull(c.Conn, buf[:n])
		total += written
		if err != nil {
			return total, err
		}
		p = p[n:]
	}
	return total, nil
}

func writeFull(w io.Writer, p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n, err := w.Write(p)
		if n < 0 || n > len(p) {
			return total, io.ErrShortWrite
		}
		total += n
		p = p[n:]
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func (c *Conn) CloseWrite() error {
	if half, ok := N.FindUpstream[interface{ CloseWrite() error }](c.Conn, nil); ok {
		return half.CloseWrite()
	}
	return errors.New("vks6: upstream does not support write half-close")
}

func (c *Conn) CloseRead() error {
	if half, ok := N.FindUpstream[interface{ CloseRead() error }](c.Conn, nil); ok {
		return half.CloseRead()
	}
	return errors.New("vks6: upstream does not support read half-close")
}

func (c *Conn) Close() error {
	c.once.Do(func() {
		c.closeErr = c.Conn.Close()
		if c.owner != nil {
			c.owner.mu.Lock()
			delete(c.owner.conns, c)
			c.owner.mu.Unlock()
		}
	})
	return c.closeErr
}
