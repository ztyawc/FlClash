package uusni

import (
	"errors"
	"io"
	"net"
	"sync"

	N "github.com/metacubex/mihomo/common/net"
)

type Conn struct {
	net.Conn
	key      byte
	owner    *Client
	rmu, wmu sync.Mutex
	once     sync.Once
	closeErr error
}

// Expose the underlying connection for half-close traversal, while preventing
// sing/bufio and kernel splice from replacing the transforming reader/writer.
func (c *Conn) Upstream() any           { return c.Conn }
func (c *Conn) ReaderReplaceable() bool { return false }
func (c *Conn) WriterReplaceable() bool { return false }

func (c *Conn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	n, err := c.Conn.Read(p)
	xor(p[:n], c.key)
	return n, err
}

func (c *Conn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	size := len(p)
	if size > 32*1024 {
		size = 32 * 1024
	}
	scratch := make([]byte, size)
	total := 0
	for len(p) != 0 {
		n := copy(scratch, p)
		xor(scratch[:n], c.key)
		written, err := writeFull(c.Conn, scratch[:n])
		total += written
		if err != nil {
			return total, err
		}
		p = p[n:]
	}
	return total, nil
}

func writeFull(w io.Writer, b []byte) (int, error) {
	total := 0
	for len(b) != 0 {
		n, err := w.Write(b)
		if n < 0 || n > len(b) {
			return total, io.ErrShortWrite
		}
		total += n
		b = b[n:]
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
	return errors.New("uusni: upstream does not support write half-close")
}

func (c *Conn) CloseRead() error {
	if half, ok := N.FindUpstream[interface{ CloseRead() error }](c.Conn, nil); ok {
		return half.CloseRead()
	}
	return errors.New("uusni: upstream does not support read half-close")
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
